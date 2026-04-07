package jsonutil

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Unmarshal decodes JSON data into T with consistent error wrapping.
// The context string describes the operation for diagnostics:
//
//	p, err := jsonutil.Unmarshal[MyParams]("cron params", input)
//	// error: "parse cron params: unexpected end of JSON input"
func Unmarshal[T any](context string, data []byte) (T, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return v, fmt.Errorf("parse %s: %w", context, err)
	}
	return v, nil
}

// UnmarshalInto decodes JSON data into v (pointer) with consistent error wrapping.
// Use this for anonymous structs where Unmarshal[T] cannot be used:
//
//	var p struct { Name string `json:"name"` }
//	if err := jsonutil.UnmarshalInto("user params", input, &p); err != nil {
//	    return "", err
//	}
func UnmarshalInto(context string, data []byte, v any) error {
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parse %s: %w", context, err)
	}
	return nil
}

// UnmarshalLLM extracts a JSON object from noisy LLM output and unmarshals
// into T. Pipeline:
//  1. StripThinkingTags + ExtractObject (strip noise, find {...})
//  2. json.Unmarshal (try clean parse)
//  3. UnescapeDoubleEncoded + retry (fix {\"key\": ...} from guided_json)
//  4. StripTrailingCommas + retry (fix common LLM mistake: {"a":1,})
//  5. RecoverTruncated (fix token-limit truncation)
//
// Does NOT include retry or transport logic — callers handle their own retry.
func UnmarshalLLM[T any](raw string) (T, error) {
	var zero T

	cleaned := ExtractObject(raw)

	// Step 1: try direct parse.
	var result T
	if json.Unmarshal([]byte(cleaned), &result) == nil {
		return result, nil
	}

	// Step 2: unescape double-encoded JSON. Local models under guided_json
	// sometimes backslash-escape all quotes: {\"key\": \"val\"} → {"key": "val"}.
	// Must come before StripTrailingCommas since trailing comma detection gets
	// confused by escaped quotes' string-literal tracking.
	if unescaped := UnescapeDoubleEncoded(raw); unescaped != raw {
		extracted := ExtractObject(unescaped)
		if json.Unmarshal([]byte(extracted), &result) == nil {
			return result, nil
		}
		// Update cleaned for subsequent steps.
		cleaned = extracted
	}

	// Step 3: strip trailing commas (most common LLM JSON mistake).
	sanitized := StripTrailingCommas(cleaned)
	if sanitized != cleaned {
		if json.Unmarshal([]byte(sanitized), &result) == nil {
			return result, nil
		}
	}

	// Step 4: truncated JSON recovery.
	if recovered := RecoverTruncated(cleaned); recovered != "" {
		if json.Unmarshal([]byte(recovered), &result) == nil {
			return result, nil
		}
	}

	return zero, fmt.Errorf("unmarshal LLM output: invalid JSON: %s", Truncate(raw, 300))
}

// StripTrailingCommas removes trailing commas before } and ] in JSON.
// This fixes the most common LLM JSON generation mistake:
//
//	{"items": [1, 2, 3,]}  →  {"items": [1, 2, 3]}
//	{"a": 1, "b": 2,}      →  {"a": 1, "b": 2}
//
// Correctly handles commas inside strings (does not modify those).
func StripTrailingCommas(s string) string {
	// Fast path: no trailing comma patterns possible.
	if !strings.ContainsAny(s, ",") {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	inString := false
	escaped := false

	for i := range len(s) {
		c := s[i]

		if escaped {
			escaped = false
			b.WriteByte(c)
			continue
		}
		if c == '\\' && inString {
			escaped = true
			b.WriteByte(c)
			continue
		}
		if c == '"' {
			inString = !inString
			b.WriteByte(c)
			continue
		}
		if inString {
			b.WriteByte(c)
			continue
		}

		// Outside string: check for trailing comma.
		if c == ',' {
			// Look ahead past whitespace for } or ].
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				// Skip the comma (trailing comma before closing bracket).
				continue
			}
		}

		b.WriteByte(c)
	}

	return b.String()
}
