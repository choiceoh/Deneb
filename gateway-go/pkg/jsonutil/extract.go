// Package jsonutil provides JSON extraction and unmarshaling utilities for
// noisy LLM output. It handles thinking tags, markdown code fences,
// surrounding prose, and truncated JSON recovery.
package jsonutil

import (
	"encoding/json"
	"regexp"
	"strings"
)

// thinkingTagRe matches <think>...</think> and <thinking>...</thinking> blocks
// that reasoning models (Qwen3.5, DeepSeek-R1, etc.) emit before output.
var thinkingTagRe = regexp.MustCompile(`(?s)<think(?:ing)?>.*?</think(?:ing)?>\s*`)

// StripThinkingTags removes <think>...</think> and <thinking>...</thinking>
// blocks from LLM output.
func StripThinkingTags(s string) string {
	return thinkingTagRe.ReplaceAllString(s, "")
}

// ExtractObject removes thinking tags, markdown code fences, and surrounding
// prose, returning the outermost JSON object substring {...}.
// Uses brace-depth tracking with string-literal awareness so nested objects
// and strings containing braces are handled correctly.
func ExtractObject(s string) string {
	s = StripThinkingTags(s)
	s = strings.TrimSpace(s)

	// Strip markdown code fences.
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
	}
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
	}
	s = strings.TrimSpace(s)

	// If it already starts with '{', it's likely clean JSON.
	if strings.HasPrefix(s, "{") {
		return s
	}

	// Find the outermost JSON object using brace-depth tracking.
	// Correctly handles prose like: 결과: {"a": {"b": 1}} 이상입니다.
	start := -1
	depth := 0
	inString := false
	escaped := false
	for i, r := range s {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && inString {
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if r == '{' {
			if depth == 0 {
				start = i
			}
			depth++
		} else if r == '}' {
			depth--
			if depth == 0 && start >= 0 {
				return s[start : i+1]
			}
		}
	}

	return s
}

// ExtractArray finds the first '[' and last ']' in s and returns the substring.
// Returns ("", false) if no valid bracket pair is found.
func ExtractArray(s string) (string, bool) {
	start := strings.Index(s, "[")
	if start == -1 {
		return "", false
	}
	end := strings.LastIndex(s, "]")
	if end == -1 || end <= start {
		return "", false
	}
	return s[start : end+1], true
}

// RecoverTruncated attempts to recover parseable JSON from truncated output
// (e.g. token limit hit mid-stream). It finds the last complete object in an
// array, closes the array and outer object.
// Returns empty string if recovery fails.
func RecoverTruncated(s string) string {
	arrStart := strings.Index(s, "[")
	if arrStart == -1 {
		return ""
	}

	// Prefix before the array (e.g. `{"results": `).
	prefix := strings.TrimSpace(s[:arrStart])

	sub := s[arrStart:]
	lastBrace := strings.LastIndex(sub, "}")
	if lastBrace == -1 {
		return ""
	}

	// Close the array.
	candidate := sub[:lastBrace+1] + "]"

	// If there was an outer object, close it too.
	if strings.HasPrefix(prefix, "{") {
		candidate = prefix + candidate + "}"
	}

	// Verify it's valid JSON before returning.
	if json.Valid([]byte(candidate)) {
		return candidate
	}

	// Fallback: try just the array portion.
	arrayOnly := sub[:lastBrace+1] + "]"
	if json.Valid([]byte(arrayOnly)) {
		return arrayOnly
	}

	return ""
}

// Truncate returns the first maxRunes runes of s, appending "..." if truncated.
// Rune-safe for Korean/CJK multi-byte UTF-8.
func Truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
