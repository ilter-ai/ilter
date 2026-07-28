// Package openapi provides OpenAPI spec loading, indexing, and HTTP execution
// for the ilter MCP tool system.
package openapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
)

// ExecuteToolCall validates LLM-provided arguments, builds a URL from the
// operation spec + config (never from the LLM), injects gateway-side auth,
// performs the HTTP request, and returns a tool-role Message for every outcome.
//
// Security invariants:
//   - LLM never controls: base URL, host, headers, auth values, HTTP method, path template.
//   - Unknown params are silently dropped (never forwarded to the upstream API).
//   - Path params are url.PathEscaped (path-traversal prevention).
//   - String params with CRLF are rejected (header-injection prevention).
//   - Auth token NEVER appears in the returned tool result content.
//   - All outcomes return a tool-role message — never an HTTP 500.
func ExecuteToolCall(
	ctx context.Context,
	toolCallID string,
	op *Operation,
	cfg *config.OpenAPISpecConfig,
	args map[string]any,
	adminKey string,
) *model.Message {
	opName := "openapi_call"
	if op != nil && op.ID != "" {
		opName = op.ID
	}

	if op == nil {
		return toolMsg(toolCallID, opName, "Error: nil operation")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	// 1. Index known params from the operation's ParamSchema (a JSON Schema
	// object with per-property schemas). Header and cookie params are excluded
	// by the indexer — they won't appear here and will be dropped as unknown.
	props, required := schemaProperties(op.ParamSchema)

	// 2. Validate required params.
	for _, name := range required {
		if _, ok := args[name]; !ok {
			return toolMsg(toolCallID, opName, fmt.Sprintf("Error: missing required parameter %q", name))
		}
	}

	// 3. Classify args — route to path/query, drop unknown, reject CRLF.
	pathVals := make(map[string]string)
	queryVals := url.Values{}

	for name, raw := range args {
		prop, ok := props[name]
		if !ok {
			// Cookie and header params are excluded from the schema by the
			// indexer (BuildIndex → buildParamSchema). They become "unknown"
			// here and are silently dropped. This is the correct security
			// boundary: the LLM can never inject header/cookie values.
			openapiLog.Debug("dropping unknown param (not in spec schema)",
				"param", name, "op", op.ID)
			continue
		}

		str := fmt.Sprintf("%v", raw)
		if containsCRLF(str) {
			return toolMsg(toolCallID, opName, fmt.Sprintf("Error: CRLF characters rejected in parameter %q", name))
		}

		// Path params match {name} in the path template; everything else is query.
		if isPathParam(name, op.Path) {
			pathVals[name] = url.PathEscape(str)
		} else {
			propSchema := propSchema(prop)
			style := op.ParamStyles[name]

			if style == openapi3.SerializationDeepObject && isObjectish(raw, propSchema) {
				serializeDeepObject(name, raw, queryVals)
			} else if isArrayish(raw, propSchema) {
				for _, v := range serializeForm(raw) {
					queryVals.Add(name, v)
				}
			} else {
				queryVals.Set(name, str)
			}
		}
	}

	// 4. Build URL: server URL + path template with escaped path params.
	base := strings.TrimRight(op.ServerURL, "/")
	path := op.Path
	for name, escaped := range pathVals {
		path = strings.ReplaceAll(path, "{"+name+"}", escaped)
	}
	u, err := url.ParseRequestURI(base + path)
	if err != nil {
		return toolMsg(toolCallID, opName, fmt.Sprintf("Error: invalid URL: %v", err))
	}
	if len(queryVals) > 0 {
		u.RawQuery = queryVals.Encode()
	}

	// 5. Build body from leftover args not consumed by params.
	var body []byte
	if op.BodySchema != nil {
		leftover := make(map[string]any)
		for k, v := range args {
			if _, isParam := props[k]; !isParam {
				leftover[k] = v
			}
		}
		if len(leftover) > 0 {
			body, err = json.Marshal(leftover)
			if err != nil {
				return toolMsg(toolCallID, opName, fmt.Sprintf("Error: serializing request body: %v", err))
			}
		}
	}

	// 6. Build HTTP request.
	method := strings.ToUpper(strings.TrimSpace(op.Method))
	if method == "" {
		return toolMsg(toolCallID, opName, "Error: empty HTTP method")
	}

	var bodyRd io.Reader
	if len(body) > 0 {
		bodyRd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyRd)
	if err != nil {
		return toolMsg(toolCallID, opName, fmt.Sprintf("Error: creating request: %v", err))
	}

	// Auth injection — gateway-side, LLM never sees these values.
	if strings.TrimSpace(cfg.Auth.Value) != "" {
		switch cfg.Auth.Type {
		case "bearer":
			req.Header.Set("Authorization", "Bearer "+cfg.Auth.Value)
		case "basic":
			encoded := base64.StdEncoding.EncodeToString([]byte(cfg.Auth.Value))
			req.Header.Set("Authorization", "Basic "+encoded)
		case "api_key":
			hdr := cfg.Auth.Key
			if hdr == "" {
				hdr = "X-API-Key"
			}
			req.Header.Set(hdr, cfg.Auth.Value)
		}
	}

	if req.Header.Get("Authorization") == "" && req.Header.Get("X-API-Key") == "" {
		if strings.Contains(u.Host, "127.0.0.1") || strings.Contains(u.Host, "localhost") {
			if adminKey != "" {
				req.Header.Set("Authorization", "Bearer "+adminKey)
				req.Header.Set("X-Admin-Key", adminKey)
			}
		}
	}

	// Content-Type for body-bearing requests.
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	// 7. Execute HTTP call.
	client := &http.Client{Timeout: timeout}
	if !isSafeMethod(method) {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return toolMsg(toolCallID, opName, fmt.Sprintf("Error: %v", err))
	}
	defer resp.Body.Close()

	// 8. Read response — limit to 1MB, then truncate to 8KB for the LLM.
	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, 1_048_576))
	if err != nil {
		return toolMsg(toolCallID, opName, fmt.Sprintf("Error: reading response: %v", err))
	}

	contentType := resp.Header.Get("Content-Type")
	var result string

	if isTextContent(contentType) {
		result = string(rawBody)
		if len(result) > 8192 {
			result = utf8SafeTruncate(result, 8192)
			result += "...[truncated]"
		}
	} else {
		result = fmt.Sprintf("[non-text response: %s, %d bytes]", contentType, len(rawBody))
	}

	if resp.StatusCode >= 400 {
		result = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, result)
	}

	return toolMsg(toolCallID, opName, result)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// toolMsg builds a tool-role Message with the given ID, name, and content.
func toolMsg(id, name, content string) *model.Message {
	return &model.Message{
		Role:       "tool",
		ToolCallID: id,
		Name:       name,
		Content:    content,
	}
}

// containsCRLF checks for CR or LF characters (header-injection prevention).
func containsCRLF(s string) bool {
	return strings.ContainsAny(s, "\r\n")
}

// isSafeMethod returns true for GET and HEAD, which are allowed to follow
// redirects automatically. All other methods use no-redirect.
func isSafeMethod(m string) bool {
	return m == "GET" || m == "HEAD"
}

// isTextContent returns true if the Content-Type indicates human-readable text.
// Empty Content-Type is treated as text (common for simple APIs).
func isTextContent(ct string) bool {
	ct = strings.ToLower(ct)
	return ct == "" ||
		strings.HasPrefix(ct, "text/") ||
		strings.HasPrefix(ct, "application/json") ||
		strings.HasPrefix(ct, "application/xml") ||
		strings.HasPrefix(ct, "application/javascript") ||
		strings.HasPrefix(ct, "application/ld+json")
}

// utf8SafeTruncate truncates s to at most max bytes while preserving UTF-8
// validity: it chops the last incomplete rune(s).
func utf8SafeTruncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	s = s[:maxLen]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// isPathParam returns true if {name} appears in the path template verbatim.
func isPathParam(name, path string) bool {
	return strings.Contains(path, "{"+name+"}")
}

// schemaProperties extracts the Properties map and Required slice from a
// SchemaRef that represents an object schema. Returns empty map if nil.
func schemaProperties(sr *openapi3.SchemaRef) (map[string]*openapi3.SchemaRef, []string) {
	if sr == nil || sr.Value == nil {
		return nil, nil
	}
	return sr.Value.Properties, sr.Value.Required
}

// propSchema dereferences a SchemaRef property to *openapi3.Schema, or nil.
func propSchema(sr *openapi3.SchemaRef) *openapi3.Schema {
	if sr == nil {
		return nil
	}
	return sr.Value
}

// isArrayish returns true if the value (or its schema) is an array or object,
// meaning it needs serialization rather than being passed as a scalar.
func isArrayish(v any, s *openapi3.Schema) bool {
	if s != nil && s.Type != nil {
		if s.Type.Includes("array") || s.Type.Includes("object") {
			return true
		}
	}
	switch v.(type) {
	case []any, map[string]any:
		return true
	}
	return false
}

// serializeForm serializes a value using "form" style:
//   - array  → foo=a&foo=b&foo=c
//   - object → foo=a,1&foo=b,2
func serializeForm(v any) []string {
	switch val := v.(type) {
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out
	case map[string]any:
		out := make([]string, 0, len(val))
		for k, vv := range val {
			out = append(out, fmt.Sprintf("%s,%v", k, vv))
		}
		return out
	}
	return []string{fmt.Sprintf("%v", v)}
}

// isObjectish returns true if the value (or its schema) is an object,
// meaning it is suitable for deepObject serialization.
func isObjectish(v any, s *openapi3.Schema) bool {
	if s != nil && s.Type != nil {
		if !s.Type.Includes("object") {
			return false
		}
	}
	_, ok := v.(map[string]any)
	return ok
}

// serializeDeepObject serializes an object value using "deepObject" style:
//   - {a:1, b:2} with param name "filter" → filter[a]=1&filter[b]=2
func serializeDeepObject(name string, v any, vals url.Values) {
	m, ok := v.(map[string]any)
	if !ok {
		// Fallback: treat as scalar.
		vals.Set(name, fmt.Sprintf("%v", v))
		return
	}
	// Keys are sorted for deterministic output.
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		vals.Add(name+"["+k+"]", fmt.Sprintf("%v", m[k]))
	}
}
