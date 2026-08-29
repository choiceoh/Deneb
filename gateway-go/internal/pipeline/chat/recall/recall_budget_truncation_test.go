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

	block, dropped := formatRecallEvidenceAt(budgetEvidence(3), now, true, true)
	if dropped != 0 {
		t.Errorf("a set that fits reported %d drops:\n%s", dropped, block)
	}

	many := budgetEvidence(40)
	block, dropped = formatRecallEvidenceAt(many, now, true, true)
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
	block, dropped := formatRecallEvidenceAt(budgetEvidence(40), time.Now(), true, true)
	if dropped == 0 {
		t.Fatal("probe did not truncate")
	}
	if !hasEvidence(block) {
		t.Fatal("probe block carries no evidence rows")
	}
	// truncated=true is what Build now reports for a budget cut.
	if shouldFreeze(true, true, block) {
		t.Error("a budget-truncated snapshot was accepted for freezing")
	}
	// A complete snapshot still freezes — the guard must not swallow the good case.
	full, dropped := formatRecallEvidenceAt(budgetEvidence(3), time.Now(), true, true)
	if dropped != 0 {
		t.Fatalf("control set truncated unexpectedly: %d", dropped)
	}
	if !shouldFreeze(true, false, full) {
		t.Error("a complete snapshot was refused for freezing")
	}
}

// The evidence header points at a tool for opening a source=file row in full.
// A restricted preset cannot reach `files` — none of researcher/implementer/
// verifier allow it, and the allow-list gates fetch_tools activation too — so
// naming it there is an instruction the run cannot follow, the same shape the
// skills block used to carry. knowledge(op="read") rides the same allow-lists
// as the wiki surfaces those presets keep, so it is named either way.
func TestRecallHeaderNamesOnlyReachableFileRoutes(t *testing.T) {
	ev := budgetEvidence(1)

	reachable, _ := formatRecallEvidenceAt(ev, time.Now(), true, true)
	if !strings.Contains(reachable, "files 도구") {
		t.Errorf("unrestricted run lost the files pointer:\n%s", reachable)
	}

	restricted, _ := formatRecallEvidenceAt(ev, time.Now(), false, true)
	if strings.Contains(restricted, "files 도구") {
		t.Errorf("restricted run was told to use a tool it cannot reach:\n%s", restricted)
	}
	if !strings.Contains(restricted, `knowledge(op="read"`) {
		t.Errorf("restricted run lost the route it CAN take:\n%s", restricted)
	}
	// The rest of the header is unchanged — this is a pointer fix, not a rewrite.
	for _, phrase := range []string{"근거가 부족하면 부족하다고 말하라", "보관된 파일의 일치 구절"} {
		if !strings.Contains(restricted, phrase) {
			t.Errorf("header lost %q:\n%s", phrase, restricted)
		}
	}
}
