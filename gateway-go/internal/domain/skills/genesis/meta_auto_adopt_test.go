package genesis

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
)

// Meta rollback watch: a recent adoption whose health snapshot the current 7d
// window regresses hard against gets auto-reverted; healthy or under-sampled
// windows stay put.
func TestMaybeRevertAdoptionTriggersRollbackOnlyOnHardHealthRegression(t *testing.T) {
	setup := func(t *testing.T) (*MetaEvolutionTask, *generation.MetaArtifacts, string) {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		tr, err := NewTracker(slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		metaDir := filepath.Join(t.TempDir(), "meta")
		meta := generation.NewMetaArtifacts(metaDir, slog.Default())
		task := &MetaEvolutionTask{Tracker: tr, Meta: meta, Logger: slog.Default()}
		return task, meta, metaDir
	}
	adoptFixture := func(t *testing.T, meta *generation.MetaArtifacts) {
		t.Helper()
		// Materialize an incumbent, write + adopt a proposal so a .rollback
		// backup exists (the state the watch would revert to).
		meta.MaterializeDefaults(map[string]string{"prompt.md": strings.Repeat("incumbent v1. ", 20)})
		if _, err := meta.WriteProposal("prompt.md", strings.Repeat("adopted v2. ", 20)); err != nil {
			t.Fatal(err)
		}
		if _, err := meta.AdoptProposal("prompt.md"); err != nil {
			t.Fatal(err)
		}
	}
	// shape the 7d evolution health: n resolved evolves with the given number
	// of rollbacks (falseAcceptRate = rollbacks/resolved).
	shapeHealth := func(t *testing.T, tr *Tracker, confirmed, rolledBack int) {
		t.Helper()
		for i := 0; i < confirmed; i++ {
			if err := tr.LogEvolveWithAudit("hsk", "1.0.1", "d", HarnessEditAudit{}); err != nil {
				t.Fatal(err)
			}
			if err := tr.logEvolveConfirmed("hsk", HarnessEditAudit{}, true); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < rolledBack; i++ {
			if err := tr.LogEvolveWithAudit("hsk", "1.0.2", "d", HarnessEditAudit{}); err != nil {
				t.Fatal(err)
			}
			if err := tr.logEvolveRolledBack("hsk"); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("hard FAR regression reverts and notifies", func(t *testing.T) {
		task, meta, metaDir := setup(t)
		adoptFixture(t, meta)
		// Snapshot at adoption: healthy (FAR 0.0).
		if err := task.Tracker.LogMetaRevision(MetaRevisionRecord{
			Artifact: "prompt.md", ToVersion: "v2", Action: "auto_adopted",
			AdoptionHealth: &MetaAdoptionHealth{ConfirmRate: 1.0, FalseAcceptRate: 0.0, Resolved: 4},
		}); err != nil {
			t.Fatal(err)
		}
		// Current window: 2 confirmed / 3 rolled back → FAR 0.6 (jump +0.6).
		shapeHealth(t, task.Tracker, 2, 3)

		var notified string
		task.OnReverted = func(artifact, _ string) { notified = artifact }
		task.maybeRevertAdoption(slog.Default())

		if got, _ := os.ReadFile(filepath.Join(metaDir, "prompt.md")); !strings.Contains(string(got), "incumbent v1") {
			t.Fatalf("live artifact not restored: %q", got)
		}
		if notified != "prompt.md" {
			t.Fatalf("OnReverted = %q", notified)
		}
		ledger, _ := task.Tracker.RecentMetaRevisions(5)
		if len(ledger) == 0 || ledger[0].Action != "auto_reverted" {
			t.Fatalf("ledger head = %+v", ledger)
		}
	})

	t.Run("healthy window stays adopted", func(t *testing.T) {
		task, meta, metaDir := setup(t)
		adoptFixture(t, meta)
		if err := task.Tracker.LogMetaRevision(MetaRevisionRecord{
			Artifact: "prompt.md", ToVersion: "v2", Action: "auto_adopted",
			AdoptionHealth: &MetaAdoptionHealth{ConfirmRate: 0.8, FalseAcceptRate: 0.2, Resolved: 4},
		}); err != nil {
			t.Fatal(err)
		}
		shapeHealth(t, task.Tracker, 4, 1) // FAR 0.2, no jump

		task.OnReverted = func(artifact, _ string) { t.Fatalf("healthy adoption reverted: %s", artifact) }
		task.maybeRevertAdoption(slog.Default())
		if got, _ := os.ReadFile(filepath.Join(metaDir, "prompt.md")); !strings.Contains(string(got), "adopted v2") {
			t.Fatalf("adoption rolled back on healthy window: %q", got)
		}
	})

	t.Run("under-sampled window never reverts", func(t *testing.T) {
		task, meta, metaDir := setup(t)
		adoptFixture(t, meta)
		if err := task.Tracker.LogMetaRevision(MetaRevisionRecord{
			Artifact: "prompt.md", ToVersion: "v2", Action: "auto_adopted",
			AdoptionHealth: &MetaAdoptionHealth{ConfirmRate: 1.0, FalseAcceptRate: 0.0, Resolved: 0},
		}); err != nil {
			t.Fatal(err)
		}
		shapeHealth(t, task.Tracker, 0, 2) // FAR 1.0 but only 2 resolved < min 4

		task.OnReverted = func(artifact, _ string) { t.Fatalf("under-sampled revert: %s", artifact) }
		task.maybeRevertAdoption(slog.Default())
		if got, _ := os.ReadFile(filepath.Join(metaDir, "prompt.md")); !strings.Contains(string(got), "adopted v2") {
			t.Fatalf("under-sampled window reverted: %q", got)
		}
	})

	t.Run("operator revert supersedes the watch", func(t *testing.T) {
		task, meta, _ := setup(t)
		adoptFixture(t, meta)
		now := time.Now().UnixMilli()
		for _, rec := range []MetaRevisionRecord{
			{Artifact: "prompt.md", ToVersion: "v2", Action: "auto_adopted", AdoptionHealth: &MetaAdoptionHealth{Resolved: 4}, CreatedAt: now - 1000},
			{Artifact: "prompt.md", Action: "operator_reverted", CreatedAt: now - 500},
		} {
			if err := task.Tracker.LogMetaRevision(rec); err != nil {
				t.Fatal(err)
			}
		}
		shapeHealth(t, task.Tracker, 0, 5) // terrible health, but newest record is the revert
		task.OnReverted = func(artifact, _ string) { t.Fatalf("watch re-fired after operator revert: %s", artifact) }
		task.maybeRevertAdoption(slog.Default())
	})
}

// A judge adoption justified by miss-rate prose must roll back when the
// usable (non-storm) probe ledger is clean — storm-inflated "놓침" was the
// only signal that got the patch adopted.
func TestMaybeRevertStormPoisonedEvaluatorAdoption(t *testing.T) {
	setup := func(t *testing.T) (*MetaEvolutionTask, *generation.MetaArtifacts, string) {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		tr, err := NewTracker(slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		metaDir := filepath.Join(t.TempDir(), "meta")
		meta := generation.NewMetaArtifacts(metaDir, slog.Default())
		artifact := generation.MetaSkillJudgeSystemPrompt
		incumbent := strings.Repeat("judge incumbent v1. ", 20)
		meta.MaterializeDefaults(map[string]string{artifact: incumbent})
		if _, err := meta.WriteProposal(artifact, strings.Repeat("judge adopted v2. ", 20)); err != nil {
			t.Fatal(err)
		}
		if _, err := meta.AdoptProposal(artifact); err != nil {
			t.Fatal(err)
		}
		return &MetaEvolutionTask{Tracker: tr, Meta: meta, Logger: slog.Default()}, meta, metaDir
	}
	seedCleanPairs := func(t *testing.T, tr *Tracker, version string, pairs int) {
		t.Helper()
		if err := tr.logJudgeAccuracy(judgeAccuracyRecord{
			JudgeVersion: version,
			Pairs:        pairs,
			Correct:      pairs,
			ByClass:      map[string][2]int{"fake-tool": {pairs, pairs}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("miss-cited adoption reverts when usable ledger is clean", func(t *testing.T) {
		task, meta, metaDir := setup(t)
		artifact := generation.MetaSkillJudgeSystemPrompt
		fallback := generation.DefaultMetaArtifacts()[artifact]
		version := meta.Version(artifact, fallback)
		if err := task.Tracker.LogMetaRevision(MetaRevisionRecord{
			Epoch: metaEpochEvaluator, Artifact: artifact, Proposed: true,
			ToVersion: version, Reason: "imperative-weaken 놓침 7/7 — tighten judge",
		}); err != nil {
			t.Fatal(err)
		}
		if err := task.Tracker.LogMetaRevision(MetaRevisionRecord{
			Artifact: artifact, ToVersion: version, Action: "auto_adopted",
			Reason: "operator adopted evaluator patch citing miss rates",
		}); err != nil {
			t.Fatal(err)
		}
		seedCleanPairs(t, task.Tracker, version, stormPoisonedJudgeMinPairs)

		var notified string
		task.OnReverted = func(name, _ string) { notified = name }
		task.maybeRevertStormPoisonedEvaluatorAdoption(slog.Default())

		if got, _ := os.ReadFile(filepath.Join(metaDir, artifact)); !strings.Contains(string(got), "judge incumbent v1") {
			t.Fatalf("live judge not restored: %q", got)
		}
		if notified != artifact {
			t.Fatalf("OnReverted = %q", notified)
		}
		ledger, _ := task.Tracker.RecentMetaRevisions(5)
		if len(ledger) == 0 || ledger[0].Action != "auto_reverted" {
			t.Fatalf("ledger head = %+v", ledger)
		}
	})

	t.Run("adoption without miss citation stays", func(t *testing.T) {
		task, meta, metaDir := setup(t)
		artifact := generation.MetaSkillJudgeSystemPrompt
		fallback := generation.DefaultMetaArtifacts()[artifact]
		version := meta.Version(artifact, fallback)
		if err := task.Tracker.LogMetaRevision(MetaRevisionRecord{
			Artifact: artifact, ToVersion: version, Action: "auto_adopted",
			Reason: "style polish only",
		}); err != nil {
			t.Fatal(err)
		}
		seedCleanPairs(t, task.Tracker, version, stormPoisonedJudgeMinPairs)

		task.OnReverted = func(name, _ string) { t.Fatalf("non-miss adoption reverted: %s", name) }
		task.maybeRevertStormPoisonedEvaluatorAdoption(slog.Default())
		if got, _ := os.ReadFile(filepath.Join(metaDir, artifact)); !strings.Contains(string(got), "judge adopted v2") {
			t.Fatalf("adoption rolled back without miss citation: %q", got)
		}
	})

	t.Run("unclean usable ledger keeps the patch", func(t *testing.T) {
		task, meta, metaDir := setup(t)
		artifact := generation.MetaSkillJudgeSystemPrompt
		fallback := generation.DefaultMetaArtifacts()[artifact]
		version := meta.Version(artifact, fallback)
		if err := task.Tracker.LogMetaRevision(MetaRevisionRecord{
			Artifact: artifact, ToVersion: version, Action: "auto_adopted",
			Reason: "판정 놓침 과다",
		}); err != nil {
			t.Fatal(err)
		}
		if err := task.Tracker.logJudgeAccuracy(judgeAccuracyRecord{
			JudgeVersion: version, Pairs: stormPoisonedJudgeMinPairs,
			Correct: stormPoisonedJudgeMinPairs - 2,
			ByClass: map[string][2]int{"fake-tool": {stormPoisonedJudgeMinPairs - 2, stormPoisonedJudgeMinPairs}},
			Misses:  []judgeMissExhibit{{Skill: "sk", Degradation: "fake-tool", Verdict: "passed_defect"}},
		}); err != nil {
			t.Fatal(err)
		}

		task.OnReverted = func(name, _ string) { t.Fatalf("real-miss adoption reverted: %s", name) }
		task.maybeRevertStormPoisonedEvaluatorAdoption(slog.Default())
		if got, _ := os.ReadFile(filepath.Join(metaDir, artifact)); !strings.Contains(string(got), "judge adopted v2") {
			t.Fatalf("real miss evidence should keep the patch: %q", got)
		}
	})
}

// The kill switch flips the success tail back to propose-only.
func TestMetaAutoAdoptEnabledDefaultsOnAndStopsOnKillSwitch(t *testing.T) {
	t.Setenv("DENEB_META_AUTO_ADOPT", "")
	if !metaAutoAdoptEnabled() {
		t.Fatal("default must be enabled (operator mandate)")
	}
	t.Setenv("DENEB_META_AUTO_ADOPT", "0")
	if metaAutoAdoptEnabled() {
		t.Fatal("kill switch ignored")
	}
}

// Adoption backup/rollback round-trip at the artifact layer.
func TestAdoptProposal_RollbackBackup(t *testing.T) {
	dir := t.TempDir()
	m := generation.NewMetaArtifacts(dir, slog.Default())
	m.MaterializeDefaults(map[string]string{"prompt.md": strings.Repeat("v1 incumbent. ", 20)})
	if _, err := m.WriteProposal("prompt.md", strings.Repeat("v2 proposal. ", 20)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AdoptProposal("prompt.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "prompt.md.rollback")); err != nil {
		t.Fatalf("rollback backup missing: %v", err)
	}
	restored, err := m.RevertAdoption("prompt.md")
	if err != nil || restored == "" {
		t.Fatalf("revert failed: %v", err)
	}
	if got := m.Load("prompt.md", "fb"); !strings.Contains(got, "v1 incumbent") {
		t.Fatalf("live artifact after revert = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "prompt.md.rollback")); err == nil {
		t.Fatal("rollback backup not consumed")
	}
}
