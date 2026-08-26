package genesis

import "testing"

// A real-use case must survive a recency window that synthetic lanes flood.
// Measured 2026-08-26: 3 of the 7 skills holding one had it already evicted.
func TestRecentValidationCasesReservesRealUseSlot(t *testing.T) {
	real := SkillValidationCaseRecord{
		SkillName: "weekly-report", ID: "real-1",
		Description: "실사용 실패 케이스", Source: "auto-failed-skill-use",
	}
	entries := []SkillValidationCaseRecord{real}
	for i := range 12 {
		entries = append(entries, SkillValidationCaseRecord{
			SkillName: "weekly-report", ID: "synth-" + string(rune('a'+i)),
			Description: "합성 케이스 " + string(rune('a'+i)), Source: "adversarial-coverage",
		})
	}

	got := reserveRealUseCase(entries, "weekly-report", nil, map[string]struct{}{}, newestFirst(entries, 5), 5)
	if len(got) != 5 {
		t.Fatalf("window size changed: got %d want 5", len(got))
	}
	var kept bool
	for _, rec := range got {
		if rec.ID == "real-1" {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("real-use case evicted by synthetic backfill: %+v", got)
	}
	// The displaced slot is the oldest synthetic one, not a fresh case.
	if got[0].Source != "adversarial-coverage" {
		t.Fatalf("recency order broken: head = %q", got[0].Source)
	}
}

// With no real-use case in the ledger the window is untouched.
func TestRecentValidationCasesLeavesSyntheticWindowAlone(t *testing.T) {
	var entries []SkillValidationCaseRecord
	for i := range 8 {
		entries = append(entries, SkillValidationCaseRecord{
			SkillName: "weekly-report", ID: "synth-" + string(rune('a'+i)),
			Description: "합성 " + string(rune('a'+i)), Source: "curriculum",
		})
	}
	want := newestFirst(entries, 5)
	got := reserveRealUseCase(entries, "weekly-report", nil, map[string]struct{}{}, want, 5)
	if len(got) != len(want) {
		t.Fatalf("window mutated without a real-use case: %d vs %d", len(got), len(want))
	}
}

func newestFirst(entries []SkillValidationCaseRecord, limit int) []SkillValidationCaseRecord {
	out := make([]SkillValidationCaseRecord, 0, limit)
	for i := len(entries) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, entries[i])
	}
	return out
}
