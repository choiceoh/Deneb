package genesis

import (
	"log/slog"
	"strings"
	"testing"
)

// A rejected candidate must survive storage intact enough to be SCORED: the
// false-reject miner replays the stored body through validation cases, and a
// body chopped at a fraction of a real SKILL.md would always under-score, so
// the only organic P3 label source could never fire.
func TestRecordRejectedSkillEditKeepsFullSkillSizedBody(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tracker, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	// Larger than every bundled SKILL.md (~14K runes at the top end).
	body := strings.Repeat("\ud55c", 15000)
	if err := tracker.RecordRejectedSkillEdit(RejectedSkillEditRecord{
		SkillName:     "youtube-summary-cards",
		Reason:        "judge rejected",
		CandidateBody: body,
	}); err != nil {
		t.Fatalf("RecordRejectedSkillEdit: %v", err)
	}

	got, err := tracker.RecentRejectedSkillEdits("youtube-summary-cards", 1)
	if err != nil {
		t.Fatalf("RecentRejectedSkillEdits: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 buffered edit, got %d", len(got))
	}
	if runes := len([]rune(got[0].CandidateBody)); runes != 15000 {
		t.Fatalf("body was truncated to %d runes; a scorable body must survive whole", runes)
	}
}
