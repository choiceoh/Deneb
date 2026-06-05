package textutil

import (
	"testing"
	"unicode/utf8"
)

// "한국어" is 3 runes × 3 bytes = 9 bytes; "한a국" is 3+1+3 = 7 bytes.
func TestTruncateBytes(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 3, "hel"},
		{"hello", 5, "hello"},
		{"hello", 9, "hello"},
		{"한국어", 9, "한국어"}, // fits exactly
		{"한국어", 7, "한국"},  // 7 lands mid 3rd rune → back off to 6
		{"한국어", 8, "한국"},  // 8 also mid → back off to 6
		{"한국어", 6, "한국"},  // exact rune boundary
		{"한a국", 4, "한a"},  // 한(3)+a(1) → 4 is a clean boundary
		{"한a국", 3, "한"},   // boundary after 한
		{"", 5, ""},
		{"한국어", 0, ""},
	}
	for _, c := range cases {
		got := TruncateBytes(c.in, c.max)
		if got != c.want {
			t.Errorf("TruncateBytes(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("TruncateBytes(%q, %d) = %q is not valid UTF-8", c.in, c.max, got)
		}
		if c.max > 0 && len(got) > c.max {
			t.Errorf("TruncateBytes(%q, %d) len = %d, exceeds budget", c.in, c.max, len(got))
		}
	}
}

func TestTailBytes(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 3, "llo"},
		{"hello", 5, "hello"},
		{"hello", 9, "hello"},
		{"한국어", 9, "한국어"},
		{"한국어", 7, "국어"}, // start 2 is mid 1st rune → advance to 3
		{"한국어", 8, "국어"},
		{"한국어", 6, "국어"}, // exact: last 6 bytes start at a boundary
		{"한a국", 4, "a국"}, // last 4 bytes start mid 한 → advance to a
		{"", 5, ""},
		{"한국어", 0, ""},
	}
	for _, c := range cases {
		got := TailBytes(c.in, c.max)
		if got != c.want {
			t.Errorf("TailBytes(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("TailBytes(%q, %d) = %q is not valid UTF-8", c.in, c.max, got)
		}
		if c.max > 0 && len(got) > c.max {
			t.Errorf("TailBytes(%q, %d) len = %d, exceeds budget", c.in, c.max, len(got))
		}
	}
}
