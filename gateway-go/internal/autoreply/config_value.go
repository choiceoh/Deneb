// config_value.go — Parses raw config values from set commands.
// Mirrors src/auto-reply/reply/config-value.ts (48 LOC).
package autoreply

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ConfigValueResult holds the parsed config value or an error.
type ConfigValueResult struct {
	Value any
	Error string
}

var numericRe = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

// ParseConfigValue parses a raw string into a typed value.
// Supports JSON objects/arrays, booleans, null, numbers, quoted strings, and bare strings.
func ParseConfigValue(raw string) ConfigValueResult {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ConfigValueResult{Error: "Missing value."}
	}

	// JSON object or array.
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var v any
		if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
			return ConfigValueResult{Error: fmt.Sprintf("Invalid JSON: %s", err)}
		}
		return ConfigValueResult{Value: v}
	}

	// Boolean literals.
	if trimmed == "true" {
		return ConfigValueResult{Value: true}
	}
	if trimmed == "false" {
		return ConfigValueResult{Value: false}
	}

	// Null.
	if trimmed == "null" {
		return ConfigValueResult{Value: nil}
	}

	// Numeric.
	if numericRe.MatchString(trimmed) {
		if n, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return ConfigValueResult{Value: n}
		}
	}

	// Quoted string — try JSON parse first, then strip quotes.
	if (strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`)) ||
		(strings.HasPrefix(trimmed, "'") && strings.HasSuffix(trimmed, "'")) {
		var v any
		if err := json.Unmarshal([]byte(trimmed), &v); err == nil {
			return ConfigValueResult{Value: v}
		}
		// Strip quotes manually.
		unquoted := trimmed[1 : len(trimmed)-1]
		return ConfigValueResult{Value: unquoted}
	}

	// Bare string.
	return ConfigValueResult{Value: trimmed}
}
