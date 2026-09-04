package textutil

import (
	"strings"
	"unicode/utf8"
)

// LastSentenceEnd returns the offset just past the last sentence terminator in
// s — a Latin terminator followed by whitespace, or a CJK one anywhere — and 0
// when the text holds no sentence end.
//
// Callers use it to cut a growing or oversized block where a reader (or a
// translator) would: at a finished thought rather than mid-clause.
func LastSentenceEnd(s string) int {
	end := 0
	prevTerm := false
	for i, r := range s {
		switch r {
		case '。', '！', '？', '…':
			end = i + utf8.RuneLen(r)
			prevTerm = false
			continue
		}
		if prevTerm && (r == ' ' || r == '\n' || r == '\t') {
			end = i + utf8.RuneLen(r)
		}
		prevTerm = r == '.' || r == '!' || r == '?'
	}
	return end
}

// CutAtBoundary returns the largest cut of s at or under maxBytes, preferring a
// line break, then a sentence end, then any space. The result is always
// positive and rune-aligned when s is longer than maxBytes, so a caller
// splitting a long text cannot stall on a run that offers no boundary at all.
// A shorter s is returned whole.
func CutAtBoundary(s string, maxBytes int) int {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return len(s)
	}
	window := s[:maxBytes]
	if i := strings.LastIndexByte(window, '\n'); i >= 0 {
		return i + 1
	}
	if j := LastSentenceEnd(window); j > 0 {
		return j
	}
	if i := strings.LastIndexByte(window, ' '); i >= 0 {
		return i + 1
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	if cut == 0 {
		return maxBytes
	}
	return cut
}
