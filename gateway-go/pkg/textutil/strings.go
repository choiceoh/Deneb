// Package textutil provides shared text formatting helpers
// used across multiple internal packages.
package textutil

import "strings"

// FirstNonBlank returns the first value whose trimmed form is non-empty,
// returning that trimmed form. If every value is empty or whitespace-only it
// returns "". This is the variadic "pick the first meaningful string" helper
// used by lifecycle/skill code paths that surface the first available reason
// or label.
func FirstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// FirstNonEmpty returns the first value that is non-empty after trimming,
// returning the ORIGINAL (un-trimmed) value. The trim is only a presence test;
// callers that need the trimmed form should use FirstNonBlank. Useful when the
// surrounding whitespace is significant (e.g. a raw field value to preserve).
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// TruncateRunes caps s to at most maxRunes runes (on a rune boundary, so
// multibyte Korean text is never split mid-character) and appends suffix when
// it actually trims. Pass "" to truncate without any marker. Callers keep their
// own suffix (e.g. "…", "...", or a localized marker) so the LLM-visible output
// is byte-for-byte unchanged after consolidation.
func TruncateRunes(s string, maxRunes int, suffix string) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + suffix
}

// TruncateRunesWithin caps the complete result, including suffix, to maxRunes.
// If suffix itself exhausts the budget, the leading suffix runes are returned.
func TruncateRunesWithin(s string, maxRunes int, suffix string) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	suffixRunes := []rune(suffix)
	if len(suffixRunes) >= maxRunes {
		return string(suffixRunes[:maxRunes])
	}
	return string(r[:maxRunes-len(suffixRunes)]) + suffix
}

// DedupeStrings trims each entry, drops blanks, and removes duplicates while
// preserving first-seen order. Returns a freshly allocated slice (never shares
// the input backing array) so callers can mutate it freely.
func DedupeStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, s := range values {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
