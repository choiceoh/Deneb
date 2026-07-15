package genesis

import (
	"strings"
	"testing"
	"time"
)

func TestClassifyUnfixableUnderperformerFiresOnlyAboveRollbackFloorAndRate(t *testing.T) {
	cfg := defaultSkillCuratorConfig() // MinRollbacks=2, RatePct=50

	tests := []struct {
		name        string
		count       skillUtilityCount
		wantArchive bool
	}{
		{"rollback thrash archives", skillUtilityCount{evolves: 2, rollbacks: 2}, true},
		{"single rollback below floor", skillUtilityCount{evolves: 1, rollbacks: 1}, false},
		{"no committed evolve never fires", skillUtilityCount{evolves: 0, rollbacks: 3}, false},
		{"improving skill (low rollback rate)", skillUtilityCount{evolves: 10, rollbacks: 2}, false},
		{"exactly at rate + floor archives", skillUtilityCount{evolves: 4, rollbacks: 2}, true},
	}
	for _, tc := range tests {
		reason := classifyUnfixableUnderperformer(tc.count, cfg)
		if (reason != "") != tc.wantArchive {
			t.Errorf("%s: archive=%v, reason=%q", tc.name, reason != "", reason)
		}
		if tc.wantArchive && !strings.Contains(reason, "unfixable underperformer") {
			t.Errorf("%s: reason missing label: %q", tc.name, reason)
		}
	}
}

func TestClassifyUnfixableUnderperformer_WorkoutFailuresInReason(t *testing.T) {
	cfg := defaultSkillCuratorConfig()
	reason := classifyUnfixableUnderperformer(skillUtilityCount{evolves: 2, rollbacks: 2, workoutFailures: 3}, cfg)
	if !strings.Contains(reason, "3 workout failures") {
		t.Errorf("workout failures should corroborate in reason: %q", reason)
	}
	// But workout failures alone (no rollback thrash) must NOT trigger archival:
	// a skill failing its own cases is a signal to EVOLVE, not to remove.
	if r := classifyUnfixableUnderperformer(skillUtilityCount{workoutFailures: 9}, cfg); r != "" {
		t.Errorf("workout failures alone must not archive: %q", r)
	}
}

// The load-bearing behavior: an ACTIVELY-USED (never idle) but unfixable
// underperformer is archived by the utility path, which the idle path could
// never reach. A healthy skill with the same recency stays active.
func TestApplySkillCuratorTransitions_ArchivesActivelyUsedRollbackThrasher(t *testing.T) {
	tracker := newTestTracker(t)
	recent := time.Now().UnixMilli()

	for _, name := range []string{"thrasher", "healthy"} {
		if err := tracker.markSkillAgentCreatedLockedForTest(name, recent); err != nil {
			t.Fatalf("mark %s: %v", name, err)
		}
	}
	// thrasher: 2 committed evolves, both rolled back → rollback rate 100%.
	if err := tracker.LogEvolve("thrasher", "v2", "attempt 1"); err != nil {
		t.Fatalf("LogEvolve: %v", err)
	}
	if err := tracker.logEvolveRolledBack("thrasher"); err != nil {
		t.Fatalf("LogEvolveRolledBack: %v", err)
	}
	if err := tracker.LogEvolve("thrasher", "v3", "attempt 2"); err != nil {
		t.Fatalf("LogEvolve: %v", err)
	}
	if err := tracker.logEvolveRolledBack("thrasher"); err != nil {
		t.Fatalf("LogEvolveRolledBack: %v", err)
	}
	// healthy: 3 committed evolves, 1 rollback → below the min-rollback floor.
	for i := 0; i < 3; i++ {
		if err := tracker.LogEvolve("healthy", "v", "ok"); err != nil {
			t.Fatalf("LogEvolve healthy: %v", err)
		}
	}
	if err := tracker.logEvolveRolledBack("healthy"); err != nil {
		t.Fatalf("LogEvolveRolledBack healthy: %v", err)
	}

	// MinIdleHours=2 with brand-new skills → the idle path skips both (idle≈0),
	// so any archival here MUST come from the utility path.
	summary, err := tracker.applySkillCuratorTransitions(time.Now(), skillCuratorConfig{
		MinIdleHours:           2,
		StaleAfterDays:         30,
		ArchiveAfterDays:       90,
		UtilityWindowDays:      30,
		UtilityMinRollbacks:    2,
		UtilityRollbackRatePct: 50,
	})
	if err != nil {
		t.Fatalf("ApplySkillCuratorTransitions: %v", err)
	}
	if summary.ArchivedUnfixable != 1 {
		t.Fatalf("ArchivedUnfixable = %d, want 1 (summary %+v)", summary.ArchivedUnfixable, summary)
	}
	if summary.Archived != 0 || summary.MarkedStale != 0 {
		t.Errorf("idle path should not have fired on brand-new skills: %+v", summary)
	}

	states := curatorStates(t, tracker)
	if states["thrasher"] != SkillCuratorStateArchived {
		t.Errorf("thrasher should be archived, got %q", states["thrasher"])
	}
	if states["healthy"] != SkillCuratorStateActive {
		t.Errorf("healthy should stay active, got %q", states["healthy"])
	}
}

func TestApplySkillCuratorTransitions_PinnedThrasherRejectsUtilityArchival(t *testing.T) {
	tracker := newTestTracker(t)
	recent := time.Now().UnixMilli()
	if err := tracker.markSkillAgentCreatedLockedForTest("pinned-thrasher", recent); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if err := tracker.SetSkillPinned("pinned-thrasher", true); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if err := tracker.LogEvolve("pinned-thrasher", "v2", "a"); err != nil {
		t.Fatalf("LogEvolve: %v", err)
	}
	if err := tracker.logEvolveRolledBack("pinned-thrasher"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if err := tracker.LogEvolve("pinned-thrasher", "v3", "b"); err != nil {
		t.Fatalf("LogEvolve: %v", err)
	}
	if err := tracker.logEvolveRolledBack("pinned-thrasher"); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	summary, err := tracker.applySkillCuratorTransitions(time.Now(), skillCuratorConfig{
		MinIdleHours:           2,
		UtilityMinRollbacks:    2,
		UtilityRollbackRatePct: 50,
	})
	if err != nil {
		t.Fatalf("ApplySkillCuratorTransitions: %v", err)
	}
	if summary.ArchivedUnfixable != 0 {
		t.Fatalf("pinned thrasher must not be archived: %+v", summary)
	}
	if curatorStates(t, tracker)["pinned-thrasher"] != SkillCuratorStateActive {
		t.Errorf("pinned thrasher should remain active")
	}
}

func curatorStates(t *testing.T, tracker *Tracker) map[string]string {
	t.Helper()
	report, err := tracker.SkillCuratorReport("")
	if err != nil {
		t.Fatalf("SkillCuratorReport: %v", err)
	}
	states := map[string]string{}
	for _, rec := range report {
		states[rec.SkillName] = rec.State
	}
	return states
}
