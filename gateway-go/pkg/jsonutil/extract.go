// Package jsonutil provides JSON extraction and unmarshaling utilities.
//
// Two layers:
//   - Core: Unmarshal/UnmarshalInto — thin json.Unmarshal wrappers with
//     consistent error formatting. Zero overhead beyond encoding/json.
//   - LLM: ExtractObject/ExtractArray/RecoverTruncated/UnmarshalLLM —
//     handles noisy model output (thinking tags, code fences, prose, truncation).
//     Only imported by LLM-adjacent code (memory, chat/pilot).
package jsonutil

import (
	"encoding/json"
	"strings"
)

// ---------- Thinking tag removal ----------

// StripThinkingTags removes <think>...</think> and <thinking>...</thinking>
// blocks from LLM output. Uses a fast string scanner (no regex) that short-
// circuits when no '<' is present.
func StripThinkingTags(s string) string {
	// Fast path: no angle brackets means no tags.
	if !strings.Contains(s, "<") {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		// Look for opening tag.
		tagStart := strings.Index(s[i:], "<think")
		if tagStart == -1 {
			b.WriteString(s[i:])
			break
		}
		tagStart += i

		// Verify it's <think> or <thinking>.
		rest := s[tagStart:]
		var closeTag string
		if strings.HasPrefix(rest, "<thinking>") {
			closeTag = "</thinking>"
		} else if strings.HasPrefix(rest, "<think>") {
			closeTag = "</think>"
		} else {
			// Not a thinking tag, copy up to and including '<'.
			b.WriteString(s[i : tagStart+1])
			i = tagStart + 1
			continue
		}

		// Write everything before the tag.
		b.WriteString(s[i:tagStart])

		// Find closing tag.
		closeIdx := strings.Index(rest, closeTag)
		if closeIdx == -1 {
			// Unclosed tag — skip to end (defensive: model output was truncated).
			break
		}

		// Skip past closing tag and any trailing whitespace.
		i = tagStart + closeIdx + len(closeTag)
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
			i++
		}
	}

	return b.String()
}

// thinkingPreamblePrefixes are common meta-prefixes that reasoning models
// (DeepSeek-R1, QwQ, etc.) emit at the start of their output. These carry no
// semantic value for downstream consumers like thread naming or summarization.
var thinkingPreamblePrefixes = []string{
	"Thinking Process:",
	"Analyze the Request:",
	"Analysis:",
	"Let me think",
	"Let me analyze",
	"I need to",
	"Okay, let me",
	"Okay, I need",
}

// StripThinkingPreamble removes common boilerplate prefixes that reasoning
// models prepend to their output. Strips up to 5 consecutive prefix lines
// from the top of the string.
func StripThinkingPreamble(s string) string {
	s = strings.TrimSpace(s)
	for range 5 {
		trimmed := false
		for _, prefix := range thinkingPreamblePrefixes {
			if strings.HasPrefix(s, prefix) {
				s = strings.TrimSpace(s[len(prefix):])
				trimmed = true
				break
			}
		}
		if !trimmed {
			break
		}
		s = strings.TrimLeft(s, "\r\n")
	}
	return strings.TrimSpace(s)
}

// ---------- Object extraction ----------

// ExtractObject removes thinking tags, markdown code fences, and surrounding
// prose, returning the outermost JSON object substring {...}.
// Uses brace-depth tracking with string-literal awareness so nested objects,
// strings containing braces, and trailing prose are handled correctly.
func ExtractObject(s string) string {
	s = StripThinkingTags(s)
	s = strings.TrimSpace(s)
	s = stripCodeFences(s)

	return findOutermostBracketed(s, '{', '}')
}

// ---------- Array extraction ----------

// ExtractArray removes thinking tags and code fences, then extracts the
// outermost JSON array [...] using bracket-depth tracking with string-literal
// awareness. Returns ("", false) if no complete array is found.
func ExtractArray(s string) (string, bool) {
	s = StripThinkingTags(s)
	s = strings.TrimSpace(s)
	s = stripCodeFences(s)

	result := findOutermostBracketed(s, '[', ']')
	// findOutermostBracketed returns s unchanged when no complete pair is found.
	// A valid extraction must start with '[' and end with ']'.
	if len(result) >= 2 && result[0] == '[' && result[len(result)-1] == ']' {
		return result, true
	}
	return "", false
}

// ---------- Shared bracket-depth tracker ----------

// findOutermostBracketed finds the first complete matched pair of open/close
// brackets in s using depth tracking with JSON string-literal awareness.
// Works for both {} (objects) and [] (arrays). Returns s unchanged if no
// complete pair is found.
func findOutermostBracketed(s string, open, close byte) string {
	start := -1
	depth := 0
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if c == open {
			if depth == 0 {
				start = i
			}
			depth++
		} else if c == close {
			depth--
			if depth == 0 && start >= 0 {
				return s[start : i+1]
			}
		}
	}
	return s
}

// ---------- Code fence removal ----------

// stripCodeFences removes markdown code fences (```json, ```JSON, ```jsonc, etc.)
// surrounding JSON content.
func stripCodeFences(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Strip ``` and optional language tag on same line.
	idx := strings.IndexByte(s[3:], '\n')
	if idx >= 0 {
		s = s[3+idx+1:]
	} else {
		s = s[3:]
	}
	if strings.HasSuffix(s, "```") {
		s = s[:len(s)-3]
	}
	return strings.TrimSpace(s)
}

// ---------- Truncated JSON recovery ----------

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
	lastBrace := lastUnquotedBrace(sub)
	if lastBrace == -1 {
		return ""
	}

	// Close the array.
	candidate := sub[:lastBrace+1] + "]"

	// If there was an outer object, close it too.
	if strings.HasPrefix(prefix, "{") {
		candidate = prefix + candidate + "}"
	}

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

// lastUnquotedBrace returns the index of the last '}' in s that is outside
// a JSON string literal. Returns -1 if no such '}' exists.
// Unlike strings.LastIndex(s, "}"), this correctly skips '}' inside quoted
// strings, preventing false matches on Korean/CJK text containing braces.
func lastUnquotedBrace(s string) int {
	inString := false
	escaped := false
	last := -1

	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if !inString && c == '}' {
			last = i
		}
	}

	return last
}

// ---------- Double-encoded JSON ----------

// UnescapeDoubleEncoded detects and unescapes JSON where all quotes have been
// backslash-escaped by the LLM. This is a common failure mode with local models
// under guided_json / xgrammar constrained decoding:
//
//	{\"facts\": [{\"content\": \"hello\"}]}  →  {"facts": [{"content": "hello"}]}
//
// Returns the original string unchanged if no escaped quotes are found.
func UnescapeDoubleEncoded(s string) string {
	if !strings.Contains(s, `\"`) {
		return s
	}
	return strings.ReplaceAll(s, `\"`, `"`)
}

// ---------- Utilities ----------

// Truncate returns the first maxRunes runes of s, appending "..." if truncated.
// Rune-safe for Korean/CJK multi-byte UTF-8.
func Truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
