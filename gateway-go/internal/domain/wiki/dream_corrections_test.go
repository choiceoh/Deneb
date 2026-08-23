package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The 반증 queue round-trips: a recorded correction stays pending until a
// cycle consumes it; blank notes never record; the critique prompt carries the
// corrections as an explicit 반증 block only when present.
func TestDreamCorrectionsPendingConsumeRoundTrip(t *testing.T) {
	s, wd := newVerifyStore(t)
	dir := s.Dir()

	if got := wd.pendingDreamCorrections(); len(got) != 0 {
		t.Fatalf("fresh queue must be empty, got %+v", got)
	}
	if err := RecordDreamCorrection(dir, "workfeed-correct", "사용자/나.md", "내 출근 시간은 9시가 아니라 10시다"); err != nil {
		t.Fatal(err)
	}
	// Blank note is a no-op.
	if err := RecordDreamCorrection(dir, "workfeed-correct", "", "   "); err != nil {
		t.Fatal(err)
	}
	pending := wd.pendingDreamCorrections()
	if len(pending) != 1 {
		t.Fatalf("pending = %+v, want 1", pending)
	}
	if pending[0].Note == "" || pending[0].Page != "사용자/나.md" || pending[0].ID == "" {
		t.Fatalf("record = %+v", pending[0])
	}

	// Consumption clears pending; a corrupt state file re-surfaces them (the
	// safe direction — reconsidered rather than lost).
	wd.consumeDreamCorrections(pending)
	if got := wd.pendingDreamCorrections(); len(got) != 0 {
		t.Fatalf("consumed corrections must leave the queue, got %+v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, dreamCorrectionsStateFile), []byte("not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := wd.pendingDreamCorrections(); len(got) != 1 {
		t.Fatalf("corrupt state must resurface corrections, got %d", len(got))
	}
}

func TestCritiquePromptCarriesDisconfirmingBlock(t *testing.T) {
	updates := []wikiUpdate{
		{Action: "create", Path: "업무/a.md", Title: "A", Summary: "첫 제안", Content: "내용"},
		{Action: "create", Path: "업무/b.md", Title: "B", Summary: "둘째 제안", Content: "내용"},
	}
	corrections := []DreamCorrection{{ID: "dc-1", Page: "사용자/나.md", Note: "내 출근 시간은 10시다"}}

	withBlock := buildCritiquePrompt(updates, "INDEX", corrections)
	if !strings.Contains(withBlock, "사용자 반증") || !strings.Contains(withBlock, "내 출근 시간은 10시다") ||
		!strings.Contains(withBlock, "사용자/나.md") {
		t.Fatalf("prompt must carry the 반증 block:\n%s", withBlock)
	}
	if !strings.Contains(withBlock, "사용자 반증과 충돌") {
		t.Fatalf("drop criteria must mention 반증 conflicts:\n%s", withBlock)
	}

	without := buildCritiquePrompt(updates, "INDEX", nil)
	if strings.Contains(without, "## 사용자 반증") || strings.Contains(without, "출근 시간은 10시다") {
		t.Fatalf("no corrections must mean no block:\n%s", without)
	}
}
