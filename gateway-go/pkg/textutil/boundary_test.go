package textutil

import (
	"strings"
	"testing"
)

func TestLastSentenceEnd(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"latin terminator plus space", "One. Two. and a tail", len("One. Two. ")},
		{"newline counts as the separator", "One.\nstill going", len("One.\n")},
		{"cjk terminator stands alone", "한 문장입니다。이어서", len("한 문장입니다。")},
		{"terminator at the very end is not settled", "no separator after this.", 0},
		{"no sentence at all", "just a running clause", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := LastSentenceEnd(tc.in); got != tc.want {
				t.Fatalf("LastSentenceEnd(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestCutAtBoundaryPrefersTheLargestStructuralCut(t *testing.T) {
	s := "first line\nsecond line\nthird"
	if got := CutAtBoundary(s, 20); got != len("first line\n") {
		t.Fatalf("cut = %d, want the last line break inside the window", got)
	}
	// No line break in the window — fall to the last sentence end, then a space.
	if got := CutAtBoundary("One. Two. Three four", 12); got != len("One. Two. ") {
		t.Fatalf("cut = %d, want the sentence end", got)
	}
	if got := CutAtBoundary("aaaa bbbb cccc dddd", 12); got != len("aaaa bbbb ") {
		t.Fatalf("cut = %d, want the last space", got)
	}
}

func TestCutAtBoundaryAlwaysAdvancesAndKeepsRunesWhole(t *testing.T) {
	// A run with no boundary at all still has to make progress, without
	// splitting a multi-byte rune.
	blob := strings.Repeat("한", 100)
	cut := CutAtBoundary(blob, 50)
	if cut <= 0 || cut > 50 {
		t.Fatalf("cut = %d, want 0 < cut <= 50", cut)
	}
	if strings.ContainsRune(blob[:cut], '�') || !isValidUTF8(blob[:cut]) {
		t.Fatal("cut split a rune in half")
	}
}

func TestCutAtBoundaryReturnsShortTextWhole(t *testing.T) {
	if got := CutAtBoundary("short", 100); got != len("short") {
		t.Fatalf("cut = %d, want the whole string", got)
	}
	if got := CutAtBoundary("anything", 0); got != len("anything") {
		t.Fatalf("cut with no budget = %d, want the whole string", got)
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}
