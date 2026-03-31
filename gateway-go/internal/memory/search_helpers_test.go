package memory

import "testing"

func TestEscapeFTS(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "single token", input: "memory", want: `"memory"`},
		{name: "multi token", input: "memory search", want: `"memory" OR "search"`},
		{name: "quoted token", input: `"exact" term`, want: `"exact" OR "term"`},
		{name: "whitespace only", input: "   ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeFTS(tt.input); got != tt.want {
				t.Fatalf("escapeFTS(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTokenHelpers(t *testing.T) {
	if got := splitWhitespace("a\tb\n c  "); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("splitWhitespace returned unexpected tokens: %#v", got)
	}

	if got := splitTokens(" one   two "); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("splitTokens returned unexpected tokens: %#v", got)
	}

	if got := stripQuotes(`"hello"`); got != "hello" {
		t.Fatalf("stripQuotes quoted = %q, want hello", got)
	}
	if got := stripQuotes("hello"); got != "hello" {
		t.Fatalf("stripQuotes plain = %q, want hello", got)
	}

	if got := buildFTSQuery([]string{"a", "b", "c"}, "OR"); got != `"a" OR "b" OR "c"` {
		t.Fatalf("buildFTSQuery OR returned %q", got)
	}
	if got := buildFTSQuery([]string{"a", "b"}, "AND"); got != `"a" AND "b"` {
		t.Fatalf("buildFTSQuery AND returned %q", got)
	}
}

func TestRankToScore(t *testing.T) {
	if got := rankToScore(0); got != 0 {
		t.Fatalf("rankToScore(0) = %v, want 0", got)
	}

	better := rankToScore(-5)
	worse := rankToScore(-1)
	if better <= worse {
		t.Fatalf("expected rank -5 to score higher than rank -1: better=%v worse=%v", better, worse)
	}
	if better <= 0 || better >= 1 || worse <= 0 || worse >= 1 {
		t.Fatalf("expected scores in (0,1): better=%v worse=%v", better, worse)
	}
}
