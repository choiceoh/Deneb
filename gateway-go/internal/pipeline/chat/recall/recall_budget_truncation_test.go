package recall

import (
	"strings"
	"testing"
	"time"
)

func budgetEvidence(n int) []recallEvidence {
	out := make([]recallEvidence, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, recallEvidence{
			Kind:   "wiki",
			Source: "프로젝트/테스트/로그.md",
			Note:   strings.Repeat("가", 320),
			Score:  1.50,
			At:     time.Now().Add(-time.Hour).UnixMilli(),
		})
	}
	return out
}

// The character budget silently dropped ranked rows: the renderer just broke
// out of the loop. Nothing downstream could tell a full snapshot from a
// shortened one.
func TestFormatRecallEvidenceReportsBudgetDrops(t *testing.T) {
	now := time.Now()

	block, dropped := formatRecallEvidenceAt(budgetEvidence(3), now)
	if dropped != 0 {
		t.Errorf("a set that fits reported %d drops:\n%s", dropped, block)
	}

	many := budgetEvidence(40)
	block, dropped = formatRecallEvidenceAt(many, now)
	if dropped <= 0 {
		t.Fatalf("40 rows of 320 runes did not exceed the %d-char budget (len=%d)", recallMaxChars, len(block))
	}
	if len(block) > recallMaxChars {
		t.Errorf("rendered block overran the budget: %d > %d", len(block), recallMaxChars)
	}
	rendered := strings.Count(block, "- source=")
	if rendered+dropped != len(many) {
		t.Errorf("rendered %d + dropped %d != %d rows", rendered, dropped, len(many))
	}
	// The block must still be well-formed — the drop happens before the close tag.
	if !strings.HasSuffix(strings.TrimSpace(block), strings.TrimSpace(recallContextCloseTag)) {
		t.Errorf("truncated block lost its close tag:\n%s", block)
	}
}

// A budget-shortened snapshot is degraded the same way a deadline-cut one is,
// so it must not be frozen: the snapshot store is first-write-wins with no
// expiry, and freezing would pin the gap onto every later turn about the topic.
func TestBudgetTruncatedSnapshotIsNotFrozen(t *testing.T) {
	block, dropped := formatRecallEvidenceAt(budgetEvidence(40), time.Now())
	if dropped == 0 {
		t.Fatal("probe did not truncate")
	}
	if !hasEvidence(block) {
		t.Fatal("probe block carries no evidence rows")
	}
	// truncated=true is what Build now reports for a budget cut.
	if ShouldFreeze(true, true, block) {
		t.Error("a budget-truncated snapshot was accepted for freezing")
	}
	// A complete snapshot still freezes — the guard must not swallow the good case.
	full, dropped := formatRecallEvidenceAt(budgetEvidence(3), time.Now())
	if dropped != 0 {
		t.Fatalf("control set truncated unexpectedly: %d", dropped)
	}
	if !ShouldFreeze(true, false, full) {
		t.Error("a complete snapshot was refused for freezing")
	}
}
