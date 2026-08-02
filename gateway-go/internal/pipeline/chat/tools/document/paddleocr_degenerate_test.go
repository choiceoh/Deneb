package document

import (
	"strings"
	"testing"
)

// Every case here is a page from the 2026-07-27 audit or its direct control.
// The old guard demanded ≥8 runes AND ≥4 letters per repeated line, so both
// real collapses (bar numbers, fingering rows) slipped through — and got
// CACHED, permanently serving garbage for those images.

func TestDegenerateCatchesBarNumberLoop(t *testing.T) {
	// Erlkönig p3: "40" emitted 1,020 times. Two runes, zero letters — exactly
	// what the old floors excluded.
	text := "3\n31\n34\n37\n" + strings.Repeat("40\n", 1020)
	if why := ocrDegenerate(text, ""); why == "" {
		t.Fatal("a bar-number loop must read degenerate")
	}
}

func TestDegenerateCatchesFingeringLoop(t *testing.T) {
	// Chopin p1: "4 4 3 2 1 2 3 2" ×249 — 15 runes but zero letters.
	text := "Nocturne\nFrédéric Chopin\n" + strings.Repeat("4 4 3 2 1 2 3 2\n", 249)
	if ocrDegenerate(text, "") == "" {
		t.Fatal("a fingering loop must read degenerate")
	}
}

func TestDegenerateCatchesTokenExhaustion(t *testing.T) {
	if ocrDegenerate("looks perfectly fine", "length") != "token-exhaustion" {
		t.Fatal("finish_reason=length is degeneration regardless of text shape")
	}
}

func TestDegenerateCatchesShortCycle(t *testing.T) {
	// Low-resolution loops emit a repeating multi-line block.
	text := strings.Repeat("3\n2\n1\n", 20)
	if ocrDegenerate(text, "") == "" {
		t.Fatal("a repeating cycle must read degenerate")
	}
}

func TestDegenerateCatchesGlyphSmear(t *testing.T) {
	text := "♩♭" + strings.Repeat("♦", 120)
	if ocrDegenerate(text, "") == "" {
		t.Fatal("a non-whitespace char run must read degenerate")
	}
}

func TestDegenerateIgnoresAlignmentSpaces(t *testing.T) {
	// The carol page: long alignment-space runs inside a healthy line. The
	// char-run rule fired on this offline until whitespace was excluded.
	text := "Good King Wenceslas\nJohn Mason Neale" + strings.Repeat(" ", 200) + "Anonymous\n가사 본문"
	if why := ocrDegenerate(text, "stop"); why != "" {
		t.Fatalf("alignment spaces are healthy, got %q", why)
	}
}

func TestDegenerateIgnoresHealthyRepetition(t *testing.T) {
	// Real documents repeat short cells legitimately below the floor.
	text := "품목 | 수량\n볼트 | 10\n너트 | 10\n와셔 | 10\n합계 | 30"
	if why := ocrDegenerate(text, "stop"); why != "" {
		t.Fatalf("a healthy table must pass, got %q", why)
	}
}

func TestTruncateAtLoopKeepsHealthyPrefix(t *testing.T) {
	// Chopin: the header must survive; table-mode rescue lost it (43 chars of
	// license text), truncation kept 222 chars of real header — best-of picks
	// truncation there.
	text := "A M.me Marie Pleyel\nNocturne\nFrédéric Chopin\n" + strings.Repeat("4 4 3 2 1 2 3 2\n", 100)
	got := truncateAtLoop(text)
	if !strings.Contains(got, "Frédéric Chopin") {
		t.Fatalf("the healthy prefix must survive: %q", got)
	}
	if strings.Count(got, "4 4 3 2 1 2 3 2") > 6 {
		t.Fatalf("the loop tail must be cut: %d reps", strings.Count(got, "4 4 3 2 1 2 3 2"))
	}
}
