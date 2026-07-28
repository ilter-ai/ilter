package openapi

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/ilter-ai/ilter/internal/config"
)

// Operation holds an indexed OpenAPI operation with resolved schemas.
type Operation struct {
	ID          string // "petstore_listPets" (namespaced, sanitized)
	API         string // spec name
	Method      string // HTTP method (GET, POST, ...)
	Path        string // original path template (e.g. /pets/{petId})
	ServerURL   string // resolved base URL from spec servers[]
	Summary     string // truncated to 200 chars
	Description string
	Tags        []string
	ParamSchema *openapi3.SchemaRef // combined path+query params
	BodySchema  *openapi3.SchemaRef // request body (nil for GET/DELETE)
	ParamStyles map[string]string   // param name → serialization style ("form", "deepObject", …)
}

// SearchResult is a scored search result for tool discovery.
type SearchResult struct {
	OperationID string `json:"operation_id"`
	API         string `json:"api"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Summary     string `json:"summary"`
	Score       int    `json:"score"`
}

const maxSummaryLen = 200

var (
	reInvalidChars    = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	reMultiUnderscore = regexp.MustCompile(`_+`)
)

// BuildIndex walks a resolved spec and builds an index of every operation
// it declares — every registered spec exposes its full operation set as
// callable tools, with no allowlist filtering. It detects name collisions.
func BuildIndex(spec *openapi3.T, cfg *config.OpenAPISpecConfig) ([]Operation, map[string]*Operation, error) {
	specName := cfg.Name

	var ops []Operation
	nameCount := make(map[string]int)         // tracks collision count for dedup
	pathOrder := spec.Paths.InMatchingOrder() // deterministic iteration

	for _, path := range pathOrder {
		pathItem := spec.Paths.Value(path)
		if pathItem == nil {
			continue
		}
		operations := pathItem.Operations()
		for method, op := range operations {
			if op == nil {
				continue
			}

			opID := op.OperationID

			// Build the raw display name.

			var rawName string
			if opID != "" {
				rawName = specName + "_" + opID
			} else {
				rawName = specName + "_" + strings.ToLower(method) + "_" + pathToSlug(path)
			}

			name := SanitizeToolName(rawName)

			// Deduplicate: append _N suffix on collision.
			if _, exists := nameCount[name]; exists {
				for i := 1; ; i++ {
					candidate := SanitizeToolName(fmt.Sprintf("%s_%d", rawName, i))
					if _, exists := nameCount[candidate]; !exists {
						name = candidate
						break
					}
				}
			}
			nameCount[name] = 1

			// Build the combined parameter schema (path + query params only).
			paramSchema, paramStyles := buildParamSchema(pathItem, op)

			// Extract request body schema if present.
			var bodySchema *openapi3.SchemaRef
			if op.RequestBody != nil && op.RequestBody.Value != nil {
				for _, mt := range op.RequestBody.Value.Content {
					if mt.Schema != nil {
						bodySchema = mt.Schema
						break
					}
				}
			}

			summary := op.Summary
			if len(summary) > maxSummaryLen {
				summary = summary[:maxSummaryLen]
			}

			var tagsCopy []string
			if len(op.Tags) > 0 {
				tagsCopy = make([]string, len(op.Tags))
				copy(tagsCopy, op.Tags)
			}

			serverURL := ""
			if len(spec.Servers) > 0 {
				serverURL = spec.Servers[0].URL
			}

			ops = append(ops, Operation{
				ID:          name,
				API:         specName,
				Method:      method,
				Path:        path,
				ServerURL:   serverURL,
				Summary:     summary,
				Description: op.Description,
				Tags:        tagsCopy,
				ParamSchema: paramSchema,
				ParamStyles: paramStyles,
				BodySchema:  bodySchema,
			})
		}
	}

	// Build the lookup map and check for duplicate final names.
	opMap := make(map[string]*Operation, len(ops))
	for i := range ops {
		if _, dup := opMap[ops[i].ID]; dup {
			return nil, nil, fmt.Errorf("openapi: duplicate operation name %q in spec %q (collision after dedup)", ops[i].ID, specName)
		}
		opMap[ops[i].ID] = &ops[i]
	}

	return ops, opMap, nil
}

// Search performs weighted keyword search over operations.
// Returns top-N results sorted by score descending.
func Search(ops []Operation, query string, limit int) []SearchResult {
	if limit <= 0 {
		limit = 10
	}

	trimmedQuery := strings.TrimSpace(strings.ToLower(query))
	tokens := strings.Fields(trimmedQuery)
	if len(tokens) == 0 {
		return nil
	}

	type scored struct {
		op    Operation
		score int
	}

	results := make([]scored, 0, len(ops))

	for _, op := range ops {
		score := 0
		opID := strings.ToLower(op.ID)
		summary := strings.ToLower(op.Summary)
		path := strings.ToLower(op.Path)
		desc := strings.ToLower(op.Description)

		for _, token := range tokens {
			if strings.Contains(opID, token) {
				score += 6
			}
			if strings.Contains(summary, token) {
				score += 4
			}
			if len(op.Tags) > 0 {
				for _, tag := range op.Tags {
					if strings.Contains(strings.ToLower(tag), token) {
						score += 4
						break
					}
				}
			}
			if strings.Contains(path, token) {
				score += 3
			}
			if strings.Contains(desc, token) {
				score += 2
			}
		}

		if score > 0 {
			results = append(results, scored{op: op, score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].op.ID < results[j].op.ID
	})

	// Fallback: if no keyword matched, but query is broad (e.g. "list all apis", "all", "*"), return top operations
	if len(results) == 0 && len(ops) > 0 {
		isBroadQuery := trimmedQuery == "*" || trimmedQuery == "all" ||
			strings.Contains(trimmedQuery, "list") || strings.Contains(trimmedQuery, "api") ||
			strings.Contains(trimmedQuery, "show") || strings.Contains(trimmedQuery, "available")
		if isBroadQuery {
			for _, op := range ops {
				results = append(results, scored{op: op, score: 1})
			}
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}

	out := make([]SearchResult, len(results))
	for i, r := range results {
		out[i] = SearchResult{
			OperationID: r.op.ID,
			API:         r.op.API,
			Method:      r.op.Method,
			Path:        r.op.Path,
			Summary:     r.op.Summary,
			Score:       r.score,
		}
	}

	return out
}

// Describe returns full ParamSchema + BodySchema JSON for the requested
// operation IDs. Unknown IDs produce a structured error entry.
func Describe(ops map[string]*Operation, ids []string) ([]json.RawMessage, error) {
	results := make([]json.RawMessage, 0, len(ids))

	for _, id := range ids {
		op, ok := ops[id]
		if !ok {
			errEntry, _ := json.Marshal(map[string]string{
				"operation_id": id,
				"error":        "operation not found",
			})
			results = append(results, errEntry)
			continue
		}

		item := make(map[string]any, 8)
		item["operation_id"] = op.ID
		item["api"] = op.API
		item["method"] = op.Method
		item["path"] = op.Path
		item["summary"] = op.Summary

		if len(op.Tags) > 0 {
			item["tags"] = op.Tags
		}
		if op.Description != "" {
			item["description"] = op.Description
		}
		if op.ParamSchema != nil {
			item["param_schema"] = op.ParamSchema
		}
		if op.BodySchema != nil {
			item["body_schema"] = op.BodySchema
		}

		data, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("describe: marshal operation %q: %w", id, err)
		}
		results = append(results, data)
	}

	return results, nil
}

// SanitizeToolName ensures a name matches ^[a-zA-Z0-9_-]{1,64}$.
// Non-compliant characters are replaced with '_'. Names over 64 chars
// keep a prefix and suffix separated by '_'.
func SanitizeToolName(name string) string {
	if name == "" {
		return "tool"
	}

	// Replace anything not in [a-zA-Z0-9_-] with '_'
	sanitized := reInvalidChars.ReplaceAllString(name, "_")

	// Collapse multiple '_' into one
	sanitized = reMultiUnderscore.ReplaceAllString(sanitized, "_")

	// Trim leading/trailing '_'
	sanitized = strings.Trim(sanitized, "_")

	if sanitized == "" {
		return "tool"
	}

	// Truncate if longer than 64 chars, keeping prefix and suffix.
	if len(sanitized) > 64 {
		keep := 30
		suffixLen := 64 - keep - 1 // 1 for the middle '_'
		if suffixLen < 1 {
			suffixLen = 1
		}
		first := sanitized[:keep]
		last := sanitized[len(sanitized)-suffixLen:]
		sanitized = first + "_" + last
	}

	return sanitized
}

// buildParamSchema combines path and query parameters from both the path item
// and operation into a single object schema, returning the schema and a map of
// param name to serialization style. Header and cookie params are excluded.
func buildParamSchema(pathItem *openapi3.PathItem, operation *openapi3.Operation) (*openapi3.SchemaRef, map[string]string) {
	// Merge path+query params. Path-level params are inherited by operations;
	// operation-level params override path-level ones on matching (in:name).
	merged := make(map[string]*openapi3.Parameter) // key = "in:name"

	for _, pref := range pathItem.Parameters {
		if pref == nil || pref.Value == nil {
			continue
		}
		p := pref.Value
		if p.In == openapi3.ParameterInPath || p.In == openapi3.ParameterInQuery {
			merged[p.In+":"+p.Name] = p
		}
	}
	for _, pref := range operation.Parameters {
		if pref == nil || pref.Value == nil {
			continue
		}
		p := pref.Value
		if p.In == openapi3.ParameterInPath || p.In == openapi3.ParameterInQuery {
			merged[p.In+":"+p.Name] = p
		}
	}

	if len(merged) == 0 {
		return nil, nil
	}

	props := make(map[string]*openapi3.SchemaRef, len(merged))
	paramStyles := make(map[string]string, len(merged))
	var required []string

	for _, p := range merged {
		if p.Schema != nil {
			props[p.Name] = p.Schema
		}
		// Determine serialization style with correct default per location.
		if p.Style != "" {
			paramStyles[p.Name] = p.Style
		} else if p.In == openapi3.ParameterInQuery {
			paramStyles[p.Name] = openapi3.SerializationForm
		} else {
			paramStyles[p.Name] = openapi3.SerializationSimple
		}
		if p.Required || p.In == openapi3.ParameterInPath {
			required = append(required, p.Name)
		}
	}

	if len(props) == 0 {
		return nil, paramStyles
	}
	if len(required) == 0 {
		return &openapi3.SchemaRef{
			Value: &openapi3.Schema{
				Type:       &openapi3.Types{"object"},
				Properties: props,
			},
		}, paramStyles
	}

	// Sort required for deterministic output.
	sort.Strings(required)
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:       &openapi3.Types{"object"},
			Properties: props,
			Required:   required,
		},
	}, paramStyles
}

// pathToSlug converts a path template to a URL-safe slug.
// /pets/{petId} → pets_petId
func pathToSlug(path string) string {
	slug := strings.TrimPrefix(path, "/")
	slug = strings.ReplaceAll(slug, "{", "")
	slug = strings.ReplaceAll(slug, "}", "")
	slug = strings.ReplaceAll(slug, "/", "_")
	slug = strings.ReplaceAll(slug, "-", "_")

	// Collapse multiple underscores.
	slug = reMultiUnderscore.ReplaceAllString(slug, "_")
	slug = strings.Trim(slug, "_")

	if slug == "" {
		slug = "root"
	}
	return slug
}
