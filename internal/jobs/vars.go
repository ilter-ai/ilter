package jobs

import (
	"fmt"
	"strings"
)

// DefaultMaxVarLength is the maximum length of a single variable value.
const DefaultMaxVarLength = 65536

// SanitizeVarValue validates and sanitizes a single variable value.
func SanitizeVarValue(key, value string, maxLen int) (string, error) {
	if strings.ContainsRune(value, 0) {
		return "", fmt.Errorf("variable %q contains null byte", key)
	}
	if len(value) > maxLen {
		return "", fmt.Errorf("variable %q exceeds max length (%d > %d)", key, len(value), maxLen)
	}
	return value, nil
}

// ResolveVariables resolves a VariablesConfig map and returns the
// resolved variable map. Each value is sanitized before being returned.
func ResolveVariables(varsConfig VariablesConfig, maxVarLength int) (map[string]any, error) {
	if maxVarLength <= 0 {
		maxVarLength = DefaultMaxVarLength
	}
	if len(varsConfig) == 0 {
		return map[string]any{}, nil
	}

	result := make(map[string]any, len(varsConfig))

	for name, val := range varsConfig {
		if m, ok := val.(map[string]any); ok {
			if typ, _ := m["type"].(string); typ != "" {
				switch typ {
				case "static":
					vStr, _ := m["value"].(string)
					sanitized, err := SanitizeVarValue(name, vStr, maxVarLength)
					if err != nil {
						return nil, err
					}
					result[name] = sanitized
				default:
					return nil, fmt.Errorf("unknown variable source type %q for variable %q", typ, name)
				}
				continue
			}
		}
		vStr, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("variable %q: expected string, got %T", name, val)
		}
		sanitized, err := SanitizeVarValue(name, vStr, maxVarLength)
		if err != nil {
			return nil, err
		}
		result[name] = sanitized
	}

	return result, nil
}
