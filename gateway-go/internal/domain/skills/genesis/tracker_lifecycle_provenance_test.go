package genesis

import (
	"log/slog"
	"testing"
)

// The certificate must survive the write→read round trip on both the accept
// and reject paths, and legacy (nil-provenance) writes must stay readable.
func TestTracker_EvolveProvenanceRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	orig, cand, margin := 6.5, 8.0, 1.5
	prov := &evolveProvenance{
		EvolveArtifactVersion: "aaaaaaaaaaaa",
		JudgeArtifactVersion:  "bbbbbbbbbbbb",
		JudgeModel:            "judge-model-x",
		JudgeScoreOriginal:    &orig,
		JudgeScoreCandidate:   &cand,
		HeldOutMargin:         &margin,
	}
	if err := tr.logEvolveWithProvenance("skill-a", "1.0.1", "desc", HarnessEditAudit{}, prov); err != nil {
		t.Fatal(err)
	}
	if err := tr.logEvolveRejectedWithProvenance("skill-b", "reason", HarnessEditAudit{}, prov); err != nil {
		t.Fatal(err)
	}
	if err := tr.LogEvolve("skill-legacy", "0.0.1", "no provenance"); err != nil {
		t.Fatal(err)
	}

	entries, err := tr.RecentLifecycleLog(10)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]LifecycleLogEntry{}
	for _, e := range entries {
		byName[e.SkillName] = e
	}
	got := byName["skill-a"].Provenance
	if got == nil || got.JudgeModel != "judge-model-x" || got.EvolveArtifactVersion != "aaaaaaaaaaaa" {
		t.Fatalf("accept-path provenance lost: %+v", got)
	}
	if got.JudgeScoreCandidate == nil || *got.JudgeScoreCandidate != cand || got.HeldOutMargin == nil || *got.HeldOutMargin != margin {
		t.Fatalf("scores/margin lost: %+v", got)
	}
	if byName["skill-b"].Provenance == nil {
		t.Fatal("reject-path provenance lost")
	}
	if byName["skill-legacy"].Provenance != nil {
		t.Fatal("legacy write must carry no provenance")
	}
}
