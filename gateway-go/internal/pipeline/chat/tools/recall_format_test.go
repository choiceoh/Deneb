package tools

import (
	"strings"
	"testing"
)

func TestRecallHeader(t *testing.T) {
	if got := recallHeader("탑솔라", 3, "wiki"); !strings.Contains(got, "🔍") ||
		!strings.Contains(got, "탑솔라") || !strings.Contains(got, "3건") || !strings.Contains(got, "wiki") {
		t.Errorf("recallHeader missing parts: %q", got)
	}
	if got := recallHeader("q", 0, ""); strings.Contains(got, ", )") {
		t.Errorf("empty extra should not leave a dangling separator: %q", got)
	}
}

func TestRecallRow(t *testing.T) {
	row := recallRow(2, "w:인물/홍길동", "2026-06-06", "  핵심 담당자  ")
	if !strings.Contains(row, "2. `w:인물/홍길동`") {
		t.Errorf("row missing indexed ref: %q", row)
	}
	if !strings.Contains(row, "· 2026-06-06") {
		t.Errorf("row missing meta suffix: %q", row)
	}
	// Snippet is trimmed then indented by exactly 3 spaces and newline-terminated.
	if !strings.Contains(row, "   핵심 담당자\n") {
		t.Errorf("snippet should be trimmed and indented: %q", row)
	}

	// No meta → no dangling separator.
	if got := recallRow(1, "p:msg7", "", "x"); strings.Contains(got, "`p:msg7` ·") {
		t.Errorf("empty meta should omit separator: %q", got)
	}
}
