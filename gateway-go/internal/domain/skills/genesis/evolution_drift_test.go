package genesis

import (
	"log/slog"
	"testing"
)

func driftTracker(t *testing.T) *Tracker {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// shapeEvolutionHealth writes confirmed/rolled-back evolves so the 7d health
// window (FalseAcceptRate = rolledBack/resolved) takes a known shape.
func shapeEvolutionHealth(t *testing.T, tr *Tracker, confirmed, rolledBack int) {
	t.Helper()
	for i := 0; i < confirmed; i++ {
		if err := tr.LogEvolveWithAudit("sk", "1.0.1", "d", HarnessEditAudit{}); err != nil {
			t.Fatal(err)
		}
		if err := tr.LogEvolveConfirmed("sk", HarnessEditAudit{}, true); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < rolledBack; i++ {
		if err := tr.LogEvolveWithAudit("sk", "1.0.2", "d", HarnessEditAudit{}); err != nil {
			t.Fatal(err)
		}
		if err := tr.LogEvolveRolledBack("sk"); err != nil {
			t.Fatal(err)
		}
	}
}

// The self-brake must engage on each drift class and stay clear on a healthy
// trajectory.
func TestAuditEvolutionDrift(t *testing.T) {
	t.Run("healthy trajectory is not frozen", func(t *testing.T) {
		tr := driftTracker(t)
		shapeEvolutionHealth(t, tr, 5, 1) // FAR 0.17
		if v := tr.AuditEvolutionDrift(); v.Frozen {
			t.Fatalf("healthy trajectory frozen: %+v", v.Signals)
		}
	})

	t.Run("judge going soft (high false-accept) freezes", func(t *testing.T) {
		tr := driftTracker(t)
		shapeEvolutionHealth(t, tr, 2, 4) // FAR 0.67 over 6 resolved
		v := tr.AuditEvolutionDrift()
		if !v.Frozen || !hasSignal(v, "judge_soft") {
			t.Fatalf("high false-accept did not trip judge_soft: %+v", v.Signals)
		}
	})

	t.Run("thin sample does not freeze on rate alone", func(t *testing.T) {
		tr := driftTracker(t)
		shapeEvolutionHealth(t, tr, 0, 2) // FAR 1.0 but only 2 resolved < min 4
		if v := tr.AuditEvolutionDrift(); v.Frozen {
			t.Fatalf("thin sample froze: %+v", v.Signals)
		}
	})

	t.Run("meta-revert spike freezes", func(t *testing.T) {
		tr := driftTracker(t)
		for _, r := range []MetaRevisionRecord{
			{Artifact: "a.md", Action: "auto_adopted"},
			{Artifact: "b.md", Action: "auto_reverted"},
			{Artifact: "c.md", Action: "auto_adopted"},
			{Artifact: "d.md", Action: "auto_reverted"},
		} {
			if err := tr.LogMetaRevision(r); err != nil {
				t.Fatal(err)
			}
		}
		v := tr.AuditEvolutionDrift()
		if !v.Frozen || !hasSignal(v, "meta_revert_spike") {
			t.Fatalf("revert spike not detected: %+v", v.Signals)
		}
	})

	t.Run("adoption monotony (diversity collapse) freezes", func(t *testing.T) {
		tr := driftTracker(t)
		for i := 0; i < 3; i++ {
			if err := tr.LogMetaRevision(MetaRevisionRecord{Artifact: "evolve-system-prompt.md", Action: "auto_adopted"}); err != nil {
				t.Fatal(err)
			}
		}
		v := tr.AuditEvolutionDrift()
		if !v.Frozen || !hasSignal(v, "adoption_monotony") {
			t.Fatalf("monotony not detected: %+v", v.Signals)
		}
	})

	t.Run("a revert between adoptions breaks the monotony streak", func(t *testing.T) {
		tr := driftTracker(t)
		// newest first when read: adopt, adopt, REVERT, adopt → streak = 2
		for _, r := range []MetaRevisionRecord{
			{Artifact: "x.md", Action: "auto_adopted"},
			{Artifact: "x.md", Action: "auto_reverted"},
			{Artifact: "x.md", Action: "auto_adopted"},
			{Artifact: "x.md", Action: "auto_adopted"},
		} {
			if err := tr.LogMetaRevision(r); err != nil {
				t.Fatal(err)
			}
		}
		if v := tr.AuditEvolutionDrift(); hasSignal(v, "adoption_monotony") {
			t.Fatalf("revert should have broken the streak: %+v", v.Signals)
		}
	})

	t.Run("broken verifier (failed planted defects) freezes", func(t *testing.T) {
		tr := driftTracker(t)
		if err := tr.LogJudgeAccuracy(JudgeAccuracyRecord{Pairs: 12, Correct: 4}); err != nil {
			t.Fatal(err)
		}
		v := tr.AuditEvolutionDrift()
		if !v.Frozen || !hasSignal(v, "verifier_broken") {
			t.Fatalf("broken verifier not detected: %+v", v.Signals)
		}
	})
}

// The persisted self-brake marker gates AutoAdoptFrozen and only logs on a
// state transition.
func TestRunEvolutionDriftAudit_Transitions(t *testing.T) {
	tr := driftTracker(t)
	if tr.AutoAdoptFrozen() {
		t.Fatal("fresh tracker should not be frozen")
	}

	// Drive a freeze.
	shapeEvolutionHealth(t, tr, 1, 5) // FAR 0.83
	var transitions []bool
	tr.RunEvolutionDriftAudit(func(frozen bool, _ []string) { transitions = append(transitions, frozen) })
	if !tr.AutoAdoptFrozen() {
		t.Fatal("drift did not engage the self-brake")
	}
	if len(transitions) != 1 || !transitions[0] {
		t.Fatalf("freeze transition not fired once: %v", transitions)
	}

	// Re-running on the same frozen state must NOT re-fire the transition.
	tr.RunEvolutionDriftAudit(func(bool, []string) { t.Fatal("transition re-fired without a state change") })

	// Recover: enough clean confirms to pull FAR under the ceiling.
	shapeEvolutionHealth(t, tr, 20, 0)
	tr.RunEvolutionDriftAudit(func(frozen bool, _ []string) { transitions = append(transitions, frozen) })
	if tr.AutoAdoptFrozen() {
		t.Fatal("recovery did not release the self-brake")
	}
	if len(transitions) != 2 || transitions[1] {
		t.Fatalf("release transition not fired: %v", transitions)
	}
}

func hasSignal(v DriftVerdict, kind string) bool {
	for _, s := range v.Signals {
		if s.Kind == kind {
			return true
		}
	}
	return false
}
