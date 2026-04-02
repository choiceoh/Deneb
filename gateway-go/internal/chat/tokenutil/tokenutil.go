// Package tokenutil provides shared token estimation utilities for the chat subsystem.
package tokenutil

import "unicode/utf8"

// RunesPerToken is the rune-based token estimate divisor.
// Korean BPE averages ~2 runes/token; English averages ~4 runes/token.
// Divisor 2 is calibrated for Korean (the primary language of this app)
// and accepts a 2x overestimate for ASCII-only content.
const RunesPerToken = 2

// EstimateTokens returns a rough token count for a string.
// Uses Unicode rune count so Korean text (3 bytes/rune in UTF-8) is not
// triple-counted as it would be with a raw len(s) / 4 byte estimate.
func EstimateTokens(s string) int {
	n := utf8.RuneCountInString(s) / RunesPerToken
	if n < 1 {
		return 1
	}
	return n
}
