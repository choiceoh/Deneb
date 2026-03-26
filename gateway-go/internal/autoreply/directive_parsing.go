// directive_parsing.go — Low-level token utilities for directive argument parsing.
// Mirrors src/auto-reply/reply/directive-parsing.ts (40 LOC).
package autoreply

import "unicode"

// SkipDirectiveArgPrefix skips leading whitespace and an optional colon prefix
// to find the start of directive arguments. Returns the index to start from.
func SkipDirectiveArgPrefix(raw string) int {
	i := 0
	runes := []rune(raw)
	n := len(runes)

	// Skip leading whitespace.
	for i < n && unicode.IsSpace(runes[i]) {
		i++
	}
	// Skip optional colon.
	if i < n && runes[i] == ':' {
		i++
		// Skip whitespace after colon.
		for i < n && unicode.IsSpace(runes[i]) {
			i++
		}
	}
	return i
}

// TakeDirectiveToken extracts the next whitespace-delimited token starting
// from startIndex. Returns the token (or empty string if none) and the next
// index to continue from.
func TakeDirectiveToken(raw string, startIndex int) (token string, nextIndex int) {
	runes := []rune(raw)
	n := len(runes)
	i := startIndex

	// Skip whitespace.
	for i < n && unicode.IsSpace(runes[i]) {
		i++
	}
	if i >= n {
		return "", i
	}

	start := i
	for i < n && !unicode.IsSpace(runes[i]) {
		i++
	}
	if start == i {
		return "", i
	}

	token = string(runes[start:i])

	// Skip trailing whitespace.
	for i < n && unicode.IsSpace(runes[i]) {
		i++
	}

	return token, i
}
