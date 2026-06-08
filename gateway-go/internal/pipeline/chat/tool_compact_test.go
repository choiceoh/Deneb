package chat

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

func TestCompactToolOutput_StripsANSI(t *testing.T) {
	// >= compactMinInputBytes so the compactor runs; ANSI wraps the payload.
	big := "\x1b[31m" + strings.Repeat("payload\n", 400) + "\x1b[0m"
	got := CompactToolOutput(context.Background(), "exec", big)
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("ANSI escapes not stripped: %q", got[:40])
	}
}

func TestCompactToolOutput_SmallPassthrough(t *testing.T) {
	small := "\x1b[31mred\x1b[0m" // well under compactMinInputBytes
	if got := CompactToolOutput(context.Background(), "exec", small); got != small {
		t.Fatalf("small output should pass through unchanged, got %q", got)
	}
}

func TestCompactToolOutput_CollapsesDuplicateLines(t *testing.T) {
	big := strings.Repeat("Building...\n", 500) // ~6KB, 500 identical lines
	got := CompactToolOutput(context.Background(), "exec", big)
	if !strings.Contains(got, "Building... (×500)") {
		t.Fatalf("adjacent duplicates not collapsed: %.80q", got)
	}
	if len(got) >= len(big) {
		t.Fatalf("compaction did not shrink output: %d >= %d", len(got), len(big))
	}
}

func TestCompactToolOutput_NoChangeReturnsOriginal(t *testing.T) {
	// 200 unique lines, no ANSI, no adjacent dupes → nothing to compact.
	var sb strings.Builder
	for i := range 200 {
		sb.WriteString("line " + strconv.Itoa(i) + " content\n")
	}
	out := sb.String()
	if got := CompactToolOutput(context.Background(), "exec", out); got != out {
		t.Fatal("clean output should be returned unchanged (pass-through guard)")
	}
}

func TestDedupeAdjacentLines(t *testing.T) {
	// Non-blank run gets a (×N) marker.
	got := dedupeAdjacentLines(strings.Repeat("same\n", 10))
	if !strings.Contains(got, "same (×10)") {
		t.Fatalf("expected run marker, got %q", got)
	}

	// Non-consecutive repeats survive (adjacent-only).
	in := "a\nb\na\nb\na\nb\na\nb\na\nb" // 10 lines, alternating
	if got := dedupeAdjacentLines(in); got != in {
		t.Fatalf("non-adjacent repeats must survive, got %q", got)
	}

	// Under the line threshold → untouched.
	short := "x\nx\nx"
	if got := dedupeAdjacentLines(short); got != short {
		t.Fatalf("below threshold should be unchanged, got %q", got)
	}

	// Blank-line run collapses to a single blank line.
	blanks := "head\n" + strings.Repeat("\n", 10) + "tail\nx\ny\nz\nw"
	got = dedupeAdjacentLines(blanks)
	if strings.Contains(got, "\n\n\n") {
		t.Fatalf("blank run not collapsed: %q", got)
	}
}

func TestDedupeAdjacentLines_RuneSafe(t *testing.T) {
	got := dedupeAdjacentLines(strings.Repeat("한글 로그\n", 10))
	if !strings.Contains(got, "한글 로그 (×10)") {
		t.Fatalf("Korean line dedup broken: %q", got)
	}
}
