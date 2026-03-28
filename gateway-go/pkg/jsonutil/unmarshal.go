package jsonutil

import (
	"encoding/json"
	"fmt"
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

// UnmarshalLLM extracts a JSON object from noisy LLM output and unmarshals
// into T. Pipeline: StripThinkingTags -> ExtractObject -> json.Unmarshal,
// with RecoverTruncated as fallback.
//
// Does NOT include retry or transport logic — callers handle their own retry.
func UnmarshalLLM[T any](raw string) (T, error) {
	var zero T

	cleaned := ExtractObject(raw)

	var result T
	if json.Unmarshal([]byte(cleaned), &result) == nil {
		return result, nil
	}

	// Try truncated JSON recovery.
	if recovered := RecoverTruncated(cleaned); recovered != "" {
		if json.Unmarshal([]byte(recovered), &result) == nil {
			return result, nil
		}
	}

	return zero, fmt.Errorf("unmarshal LLM output: invalid JSON: %s", Truncate(raw, 300))
}
