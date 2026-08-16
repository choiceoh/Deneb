package genesis

import (
	"log/slog"
	"os"
	"testing"
)

func driftTracker(t *testing.T) *Tracker {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
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
		if err := tr.logEvolveConfirmed("sk", HarnessEditAudit{}, true); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < rolledBack; i++ {
		if err := tr.LogEvolveWithAudit("sk", "1.0.2", "d", HarnessEditAudit{}); err != nil {
			t.Fatal(err)
		}
		if err := tr.logEvolveRolledBack("sk"); err != nil {
			t.Fatal(err)
		}
	}
}

// The self-brake must engage on each drift class and stay clear on a healthy
// trajectory.
func TestAuditEvolutionDrift_FreezesOnDriftSignalsStaysClearOnHealthyTrajectory(t *testing.T) {
	t.Run("healthy trajectory is not frozen", func(t *testing.T) {
		tr := driftTracker(t)
		shapeEvolutionHealth(t, tr, 5, 1) // FAR 0.17
		if v := tr.auditEvolutionDrift(); v.Frozen {
			t.Fatalf("healthy trajectory frozen: %+v", v.Signals)
		}
	})

	t.Run("judge going soft (high false-accept) freezes", func(t *testing.T) {
		tr := driftTracker(t)
		shapeEvolutionHealth(t, tr, 2, 4) // FAR 0.67 over 6 resolved
		v := tr.auditEvolutionDrift()
		if !v.Frozen || !hasSignal(v, "judge_soft") {
			t.Fatalf("high false-accept did not trip judge_soft: %+v", v.Signals)
		}
	})

	t.Run("thin sample does not freeze on rate alone", func(t *testing.T) {
		tr := driftTracker(t)
		shapeEvolutionHealth(t, tr, 0, 2) // FAR 1.0 but only 2 resolved < min 4
		if v := tr.auditEvolutionDrift(); v.Frozen {
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
		v := tr.auditEvolutionDrift()
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
		v := tr.auditEvolutionDrift()
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
		if v := tr.auditEvolutionDrift(); hasSignal(v, "adoption_monotony") {
			t.Fatalf("revert should have broken the streak: %+v", v.Signals)
		}
	})

	t.Run("broken verifier (failed planted defects) freezes", func(t *testing.T) {
		tr := driftTracker(t)
		// No ByClass breakdown (legacy record) — the aggregate fallback path.
		if err := tr.logJudgeAccuracy(judgeAccuracyRecord{Pairs: 12, Correct: 4}); err != nil {
			t.Fatal(err)
		}
		v := tr.auditEvolutionDrift()
		if !v.Frozen || !hasSignal(v, "verifier_broken") {
			t.Fatalf("broken verifier not detected: %+v", v.Signals)
		}
	})

	t.Run("must-catch misses trip verifier_broken even when the aggregate looks fine", func(t *testing.T) {
		tr := driftTracker(t)
		// Blatant 1/4 caught is breakage; the passing subtle probes hold the
		// aggregate at 6/9 = 0.67, which the old all-class rate would miss.
		if err := tr.logJudgeAccuracy(judgeAccuracyRecord{
			Pairs: 9, Correct: 6,
			ByClass: map[string][2]int{
				"section-drop":    {1, 1},
				"fake-tool":       {0, 1},
				"truncation":      {0, 1},
				"overfit":         {0, 1},
				"imperative-drop": {3, 3},
				"safety-drop":     {2, 2},
			},
		}); err != nil {
			t.Fatal(err)
		}
		v := tr.auditEvolutionDrift()
		if !v.Frozen || !hasSignal(v, "verifier_broken") {
			t.Fatalf("must-catch misses not detected: %+v", v.Signals)
		}
	})

	t.Run("weaken-tier misses alone do not trip verifier_broken", func(t *testing.T) {
		tr := driftTracker(t)
		// Curriculum-ladder run (#3602): every blatant probe caught, every
		// tier-3 weaken probe missed. The aggregate 4/10 = 0.40 sits under the
		// floor — scoring must-catch classes only keeps the healthy judge
		// unfrozen (weaken misses are P3 fuel, not breakage).
		if err := tr.logJudgeAccuracy(judgeAccuracyRecord{
			Pairs: 10, Correct: 4,
			ByClass: map[string][2]int{
				"section-drop":      {1, 1},
				"fake-tool":         {1, 1},
				"truncation":        {1, 1},
				"overfit":           {1, 1},
				"imperative-weaken": {0, 3},
				"scope-narrow":      {0, 3},
			},
		}); err != nil {
			t.Fatal(err)
		}
		if v := tr.auditEvolutionDrift(); hasSignal(v, "verifier_broken") {
			t.Fatalf("weaken-tier misses tripped verifier_broken: %+v", v.Signals)
		}
	})

	t.Run("run with no must-catch pairs yields no verifier evidence", func(t *testing.T) {
		tr := driftTracker(t)
		if err := tr.logJudgeAccuracy(judgeAccuracyRecord{
			Pairs: 4, Correct: 0,
			ByClass: map[string][2]int{
				"imperative-drop": {0, 2},
				"safety-drop":     {0, 2},
			},
		}); err != nil {
			t.Fatal(err)
		}
		if v := tr.auditEvolutionDrift(); hasSignal(v, "verifier_broken") {
			t.Fatalf("subtle-only run tripped verifier_broken: %+v", v.Signals)
		}
	})
}

// The persisted self-brake marker gates AutoAdoptFrozen and only logs on a
// state transition.
func TestRunEvolutionDriftAudit_FiresTransitionCallbackOnlyOnStateChangeAndRecovery(t *testing.T) {
	tr := driftTracker(t)
	if tr.AutoAdoptFrozen() {
		t.Fatal("fresh tracker should not be frozen")
	}

	// Drive a freeze.
	shapeEvolutionHealth(t, tr, 1, 5) // FAR 0.83
	var transitions []bool
	tr.runEvolutionDriftAudit(func(frozen bool, _ []string) { transitions = append(transitions, frozen) })
	if !tr.AutoAdoptFrozen() {
		t.Fatal("drift did not engage the self-brake")
	}
	if len(transitions) != 1 || !transitions[0] {
		t.Fatalf("freeze transition not fired once: %v", transitions)
	}

	// Re-running on the same frozen state must NOT re-fire the transition.
	tr.runEvolutionDriftAudit(func(bool, []string) { t.Fatal("transition re-fired without a state change") })

	// Recover: enough clean confirms to pull FAR under the ceiling.
	shapeEvolutionHealth(t, tr, 20, 0)
	tr.runEvolutionDriftAudit(func(frozen bool, _ []string) { transitions = append(transitions, frozen) })
	if tr.AutoAdoptFrozen() {
		t.Fatal("recovery did not release the self-brake")
	}
	if len(transitions) != 2 || transitions[1] {
		t.Fatalf("release transition not fired: %v", transitions)
	}
}

// H5: the self-brake must fail CLOSED. An unreadable/corrupt marker, or a
// present-but-empty one, reads as frozen — a loop that cannot read its own
// brake must not default to "go".
func TestAutoAdoptFrozen_FailsClosed(t *testing.T) {
	t.Run("corrupt marker reads frozen", func(t *testing.T) {
		tr := driftTracker(t)
		if err := os.WriteFile(tr.autoAdoptFreezePath(), []byte("{not json\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if !tr.AutoAdoptFrozen() {
			t.Fatal("corrupt freeze marker must fail closed (frozen)")
		}
	})
	t.Run("present-but-empty marker reads frozen", func(t *testing.T) {
		tr := driftTracker(t)
		if err := os.WriteFile(tr.autoAdoptFreezePath(), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
		if !tr.AutoAdoptFrozen() {
			t.Fatal("empty freeze marker must fail closed (frozen)")
		}
	})
	t.Run("absent marker reads not-frozen", func(t *testing.T) {
		tr := driftTracker(t)
		if tr.AutoAdoptFrozen() {
			t.Fatal("absent marker (fresh install) must read not-frozen")
		}
	})
}

func hasSignal(v driftVerdict, kind string) bool {
	for _, s := range v.Signals {
		if s.Kind == kind {
			return true
		}
	}
	return false
}
