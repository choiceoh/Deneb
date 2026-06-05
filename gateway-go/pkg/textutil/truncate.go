package textutil

import "unicode/utf8"

// TruncateBytes returns s shortened to at most maxBytes bytes, backing off to
// the nearest rune boundary so it never splits a multi-byte character (which
// would emit a U+FFFD replacement char — visible mojibake in Korean text).
//
// It preserves the BYTE budget: the result is always <= maxBytes bytes. This is
// the right tool for context/length caps, unlike slicing []rune at a rune count,
// which for CJK text would let the byte size grow up to 3x past the intended
// limit. Returns s unchanged when it already fits.
func TruncateBytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// TailBytes returns the final at most maxBytes bytes of s, advancing the start
// to a rune boundary so the result never begins mid-character. It is the
// byte-budget tail counterpart to TruncateBytes, for head+tail trimmers that
// keep the start and end of an oversized string. Returns s unchanged when it
// already fits.
func TailBytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	start := len(s) - maxBytes
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}
