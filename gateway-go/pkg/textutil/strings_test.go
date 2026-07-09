package textutil

import "testing"

func TestFirstNonBlank(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"all empty", []string{"", "   ", "\t\n"}, ""},
		{"first wins trimmed", []string{"  ok  ", "later"}, "ok"},
		{"skips blanks", []string{"", "  ", "found", "ignored"}, "found"},
		{"none", nil, ""},
	}
	for _, c := range cases {
		if got := FirstNonBlank(c.in...); got != c.want {
			t.Errorf("%s: FirstNonBlank(%v) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	// Returns the ORIGINAL (un-trimmed) value.
	if got := FirstNonEmpty("  keep me  ", "x"); got != "  keep me  " {
		t.Errorf("FirstNonEmpty should return un-trimmed value, got %q", got)
	}
	if got := FirstNonEmpty("", "  ", "second"); got != "second" {
		t.Errorf("FirstNonEmpty skip-blank failed, got %q", got)
	}
	if got := FirstNonEmpty("", "   "); got != "" {
		t.Errorf("FirstNonEmpty all-blank should be empty, got %q", got)
	}
}

func TestTruncateRunes(t *testing.T) {
	// Korean is never split mid-character.
	if got := TruncateRunes("가나다라마", 3, "..."); got != "가나다..." {
		t.Errorf("TruncateRunes CJK = %q, want 가나다...", got)
	}
	// Fits → unchanged, no suffix.
	if got := TruncateRunes("short", 10, "..."); got != "short" {
		t.Errorf("TruncateRunes no-trim = %q, want short", got)
	}
	// Custom suffix (incl. empty).
	if got := TruncateRunes("abcdef", 3, ""); got != "abc" {
		t.Errorf("TruncateRunes empty suffix = %q, want abc", got)
	}
	if got := TruncateRunes("abcdef", 3, "…"); got != "abc…" {
		t.Errorf("TruncateRunes ellipsis suffix = %q, want abc…", got)
	}
	// Exact length boundary — no truncation.
	if got := TruncateRunes("abc", 3, "..."); got != "abc" {
		t.Errorf("TruncateRunes exact boundary = %q, want abc", got)
	}
}

func TestDedupeStrings(t *testing.T) {
	in := []string{"  b  ", "a", "", "b", "a", "  ", "c"}
	got := DedupeStrings(in)
	want := []string{"b", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf("DedupeStrings = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("DedupeStrings[%d] = %q, want %q (order preserved)", i, got[i], want[i])
		}
	}
}
