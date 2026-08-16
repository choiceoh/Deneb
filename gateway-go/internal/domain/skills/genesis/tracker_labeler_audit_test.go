package genesis

import (
	"log/slog"
	"testing"
	"time"
)

// Blind Curator join: a confirmed-clean skill that concurrently fails its own
// workout cases is a labeler false-pass suspect; every other combination is not.
func TestLabelerBlindSpotsFlagsConfirmedSkillsThatFailOwnWorkoutCases(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	confirm := func(skill string) {
		t.Helper()
		if err := tr.logEvolveConfirmed(skill, HarnessEditAudit{}, true); err != nil {
			t.Fatal(err)
		}
	}
	workoutFail := func(skill, caseLabel string) {
		t.Helper()
		if err := tr.RecordUsage(UsageRecord{
			SkillName: skill, SessionKey: "workout:1", Success: false,
			ErrorMsg: "workout replay failed 1/2 assertions on case " + caseLabel + ": boom",
			Source:   usageSourceWorkout,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// sk-blind: confirmed AND fails two workout cases → flagged with count 2.
	confirm("sk-blind")
	workoutFail("sk-blind", "c1")
	workoutFail("sk-blind", "c2")
	// sk-clean: confirmed, no workout failures → not flagged.
	confirm("sk-clean")
	// sk-workout-only: workout failure without a confirm → not flagged
	// (that is L1's normal evolve signal, not a labeler contradiction).
	workoutFail("sk-workout-only", "c1")

	spots := tr.labelerBlindSpots(7 * 24 * time.Hour)
	if len(spots) != 1 || spots[0].Skill != "sk-blind" || spots[0].FailedCases != 2 {
		t.Fatalf("want sk-blind with 2 failed cases, got %+v", spots)
	}
	if spots[0].ConfirmedAt == 0 {
		t.Fatalf("blind spot must carry the confirm timestamp: %+v", spots[0])
	}

	// A window that predates all records yields nothing.
	time.Sleep(2 * time.Millisecond)
	if got := tr.labelerBlindSpots(time.Millisecond); len(got) != 0 {
		t.Fatalf("sub-ms window must exclude all joins: %+v", got)
	}
}
