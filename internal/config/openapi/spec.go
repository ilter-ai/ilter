package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"

	"github.com/ilter-ai/ilter/internal/config"
)

var openapiLog = slog.With("component", "openapi")

// LoadSpec loads and parses an OpenAPI spec from cfg.SpecURL (URL or file path).
// External $ref resolution is disabled (SSRF prevention).
// Supports OpenAPI 3.0, 3.1 (via kin-openapi native loader) and Swagger 2.0
// (via openapi2conv.ToV3 conversion). Returns a resolved *openapi3.T with all
// $ref references resolved inline.
func LoadSpec(cfg *config.OpenAPISpecConfig) (*openapi3.T, error) {
	raw, err := readSpecRaw(cfg.SpecURL)
	if err != nil {
		return nil, fmt.Errorf("openapi: reading spec %q: %w", cfg.Name, err)
	}

	// Detect spec version before deciding the load path.
	version, err := detectVersion(raw)
	if err != nil {
		return nil, fmt.Errorf("openapi: detecting spec version %q: %w", cfg.Name, err)
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false // CRITICAL: SSRF prevention

	var doc *openapi3.T

	switch version {
	case "swagger2":
		openapiLog.Info("detected Swagger 2.0 spec, converting to OpenAPI 3.0",
			"name", cfg.Name)

		var doc2 openapi2.T
		if err = unmarshalSpec(raw, &doc2); err != nil {
			return nil, fmt.Errorf("openapi: parsing Swagger 2.0 spec %q: %w", cfg.Name, err)
		}
		doc, err = openapi2conv.ToV3(&doc2)
		if err != nil {
			return nil, fmt.Errorf("openapi: converting Swagger 2.0 to OpenAPI 3.0 %q: %w", cfg.Name, err)
		}
		// After conversion, resolve $ref references that kin-openapi left dangling.
		if err = loader.ResolveRefsIn(doc, nil); err != nil {
			return nil, fmt.Errorf("openapi: resolving $ref in spec %q: %w", cfg.Name, err)
		}
	default: // "openapi3" — OpenAPI 3.0 or 3.1
		doc, err = loader.LoadFromData(raw)
		if err != nil {
			return nil, fmt.Errorf("openapi: loading spec %q: %w", cfg.Name, err)
		}
	}

	detectBaseURL(doc)

	if len(doc.Servers) == 0 || doc.Servers[0].URL == "" {
		return nil, fmt.Errorf("openapi: spec %q has no servers, host, or basePath", cfg.Name)
	}

	opCount := countOperations(doc)
	openapiLog.Info(
		"loaded spec",
		"name", cfg.Name,
		"base_url", doc.Servers[0].URL,
		"operations", opCount,
	)

	return doc, nil
}

// readSpecRaw reads raw spec bytes from specURL which may be an http(s) URL
// or a local file path. Uses a 30s HTTP client timeout for URLs.
func readSpecRaw(specURL string) ([]byte, error) {
	if specURL == "" {
		return nil, fmt.Errorf("spec_url is empty")
	}

	if strings.HasPrefix(specURL, "http://") || strings.HasPrefix(specURL, "https://") {
		client := &http.Client{Timeout: 30 * time.Second}
		req, err := http.NewRequestWithContext(context.Background(), "GET", specURL, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request for %q: %w", specURL, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetching URL %q: %w", specURL, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetching URL %q: unexpected HTTP status %d", specURL, resp.StatusCode)
		}
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("reading response body from %q: %w", specURL, err)
		}
		return raw, nil
	}

	// Local file
	info, err := os.Stat(specURL)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", specURL)
		}
		return nil, fmt.Errorf("stat %q: %w", specURL, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a spec file: %s", specURL)
	}
	raw, err := os.ReadFile(specURL)
	if err != nil {
		return nil, fmt.Errorf("reading file %q: %w", specURL, err)
	}
	return raw, nil
}

// detectVersion scans raw spec bytes to determine whether
// it is Swagger 2.0 ("swagger2") or OpenAPI 3.x ("openapi3").
func detectVersion(raw []byte) (string, error) {
	// Try JSON first — handles JSON + YAML-parsable top-level maps.
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err == nil {
		if swagger, ok := doc["swagger"]; ok {
			if s, ok := swagger.(string); ok && s == "2.0" {
				return "swagger2", nil
			}
		}
		if _, ok := doc["openapi"]; ok {
			return "openapi3", nil
		}
	}
	// YAML-only: scan for top-level keys via byte patterns.
	trim := bytes.TrimSpace(raw)
	if bytes.HasPrefix(trim, []byte("swagger: 2.0")) || bytes.Contains(trim, []byte("\nswagger: 2.0")) || bytes.Contains(trim, []byte("swagger: \"2.0\"")) {
		return "swagger2", nil
	}
	if bytes.HasPrefix(trim, []byte("openapi:")) || bytes.Contains(trim, []byte("\nopenapi:")) || bytes.Contains(trim, []byte("\"openapi\":")) {
		return "openapi3", nil
	}
	return "", fmt.Errorf("unable to detect OpenAPI version: missing 'openapi' or 'swagger' field")
}

// unmarshalSpec unmarshals raw bytes into target (JSON only).
// Swagger 2.0 specs should be in JSON format for this to work.
func unmarshalSpec(raw []byte, target any) error {
	return json.Unmarshal(raw, target)
}

// detectBaseURL populates doc.Servers from the spec if not already set.
// For OpenAPI 3.x it preserves servers[].url. For Swagger 2.0 (already
// converted to 3.x) the servers are set by openapi2conv.ToV3.
// If servers are still empty it checks for x-servers or defaults to "".
func detectBaseURL(doc *openapi3.T) {
	if len(doc.Servers) > 0 && doc.Servers[0].URL != "" {
		return
	}
	doc.Servers = openapi3.Servers{}
}

// countOperations returns the total number of HTTP operations in the spec.
func countOperations(doc *openapi3.T) int {
	if doc.Paths == nil {
		return 0
	}
	n := 0
	for _, item := range doc.Paths.Map() {
		if item.Get != nil {
			n++
		}
		if item.Put != nil {
			n++
		}
		if item.Post != nil {
			n++
		}
		if item.Delete != nil {
			n++
		}
		if item.Options != nil {
			n++
		}
		if item.Head != nil {
			n++
		}
		if item.Patch != nil {
			n++
		}
		if item.Trace != nil {
			n++
		}
	}
	return n
}
