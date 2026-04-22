package jsonutil

import (
	"encoding/json"
)

// TruncateStringLeaves walks a JSON document and shrinks every string value
// whose length exceeds headChars, keeping the first headChars runes and
// appending "...[truncated]". Non-string leaves (numbers, bools, null) pass
// through unchanged. If the input is not valid JSON it is returned as-is,
// because the caller's downstream handling expects the original bytes.
//
// This matters for tool-call arguments and structured tool results where
// the LLM provider strictly validates JSON: a naive byte-slice truncation
// produces an unterminated string / missing brace, provider returns a
// non-retryable 400, and the session gets stuck re-sending the same broken
// history on every turn. Structural truncation preserves validity.
//
// headChars is a rune count, so multibyte UTF-8 (CJK, emoji) is not cut
// mid-codepoint. Output is marshaled with ensure_ascii disabled (Go's
// default) so CJK stays compact.
//
// Inspired by NousResearch/hermes-agent
// (agent/context_compressor.py:_truncate_tool_call_args_json).
func TruncateStringLeaves(raw string, headChars int) string {
	if headChars <= 0 {
		headChars = 200
	}
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return raw
	}
	shrunk := shrinkLeaves(parsed, headChars)
	out, err := json.Marshal(shrunk)
	if err != nil {
		return raw
	}
	return string(out)
}

// shrinkLeaves recursively walks a decoded JSON structure and truncates
// oversized string leaves. Arrays and objects pass through with their
// contents shrunk; non-string scalars (numbers, booleans, null) are
// returned unchanged.
func shrinkLeaves(v any, headChars int) any {
	switch val := v.(type) {
	case string:
		return truncateRunes(val, headChars)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = shrinkLeaves(item, headChars)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = shrinkLeaves(item, headChars)
		}
		return out
	default:
		return val
	}
}

// truncateRunes trims s to at most headChars runes, appending a truncated
// marker when the original string was longer. Rune-safe: never cuts a
// multibyte character in half.
func truncateRunes(s string, headChars int) string {
	if headChars < 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= headChars {
		return s
	}
	return string(runes[:headChars]) + "...[truncated]"
}
