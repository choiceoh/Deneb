package genesis

import (
	"log/slog"
	"strings"
	"testing"
)

// The deterministic contract gate is what stands between an LLM proposal and
// the .proposed file — it must reject schema-breaking, oversized, and no-op
// revisions regardless of how plausible the prose reads.
func TestMetaProposalGate(t *testing.T) {
	incumbent := strings.Repeat("현재 프롬프트 내용. ", 30)
	valid := incumbent + `
## 출력 (JSON만)
{"skip": false, "changes": {"body": "...", "new_version": "0.1.1", "target_signature": "...", "reproduction_case": {}}}`

	cases := []struct {
		name     string
		artifact string
		proposal string
		wantPass bool
		wantIn   string
	}{
		{"valid evolve revision passes", MetaEvolveSystemPrompt, valid, true, ""},
		{"identical proposal rejected", MetaEvolveSystemPrompt, incumbent, false, "identical"},
		{"short stump rejected", MetaEvolveSystemPrompt, "too short", false, "too short"},
		{"oversized rejected", MetaEvolveSystemPrompt, valid + strings.Repeat("x", metaProposalMaxBytes), false, "too large"},
		{
			"dropped schema anchor rejected", MetaEvolveSystemPrompt,
			strings.ReplaceAll(valid, `"reproduction_case"`, `"repro"`), false, "anchor",
		},
		{
			"judge contract enforced", MetaSkillJudgeSystemPrompt,
			strings.Repeat("점수 없이 서술만 하는 judge 프롬프트. ", 20), false, `"pass"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := metaProposalGate(tc.artifact, incumbent, tc.proposal)
			if tc.wantPass && got != "" {
				t.Fatalf("expected pass, got rejection: %s", got)
			}
			if !tc.wantPass {
				if got == "" {
					t.Fatal("expected rejection, gate passed")
				}
				if !strings.Contains(got, tc.wantIn) {
					t.Fatalf("rejection %q does not mention %q", got, tc.wantIn)
				}
			}
		})
	}
}

// Epoch alternation reads the meta-experience ledger: producer first, then
// evaluator, then producer again — one half of the pipeline per window.
func TestMetaEvolution_EpochAlternation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	task := &MetaEvolutionTask{Tracker: tr}

	epoch, artifact := task.nextEpoch()
	if epoch != metaEpochProducer || artifact != MetaEvolveSystemPrompt {
		t.Fatalf("first cycle = %s/%s, want producer/evolve", epoch, artifact)
	}
	if err := tr.LogMetaRevision(MetaRevisionRecord{Epoch: epoch, Artifact: artifact, FromVersion: "aaa"}); err != nil {
		t.Fatal(err)
	}
	epoch, artifact = task.nextEpoch()
	if epoch != metaEpochEvaluator || artifact != MetaSkillJudgeSystemPrompt {
		t.Fatalf("second cycle = %s/%s, want evaluator/judge", epoch, artifact)
	}
	if err := tr.LogMetaRevision(MetaRevisionRecord{Epoch: epoch, Artifact: artifact, FromVersion: "bbb"}); err != nil {
		t.Fatal(err)
	}
	epoch, _ = task.nextEpoch()
	if epoch != metaEpochProducer {
		t.Fatalf("third cycle = %s, want producer", epoch)
	}
}

// The ledger is the meta-experience memory — it must survive round-trips and
// surface newest-first, and the evidence block must include it.
func TestMetaEvolution_LedgerAndEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []MetaRevisionRecord{
		{Epoch: metaEpochProducer, Artifact: MetaEvolveSystemPrompt, FromVersion: "v1", Reason: "first"},
		{Epoch: metaEpochEvaluator, Artifact: MetaSkillJudgeSystemPrompt, FromVersion: "v2", ToVersion: "v3", Proposed: true, Reason: "tightened scoring rubric"},
	} {
		if err := tr.LogMetaRevision(r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := tr.RecentMetaRevisions(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].Proposed || got[0].ToVersion != "v3" {
		t.Fatalf("newest-first ledger = %+v", got)
	}

	task := &MetaEvolutionTask{Tracker: tr}
	evidence := task.assembleEvidence()
	if !strings.Contains(evidence, "tightened scoring rubric") {
		t.Fatalf("evidence lacks meta-experience memory:\n%s", evidence)
	}
	if !strings.Contains(evidence, "진화 스코어보드") {
		t.Fatalf("evidence lacks health scoreboard:\n%s", evidence)
	}
}

// Propose-only invariant: WriteProposal never touches the live artifact.
func TestWriteProposal_DoesNotTouchLiveArtifact(t *testing.T) {
	dir := t.TempDir()
	m := NewMetaArtifacts(dir, slog.Default())
	live := strings.Repeat("live artifact content. ", 20)
	m.MaterializeDefaults(map[string]string{"prompt.md": live})

	path, err := m.WriteProposal("prompt.md", strings.Repeat("proposed revision. ", 20))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "prompt.md.proposed") {
		t.Fatalf("proposal path = %s", path)
	}
	if got := m.Load("prompt.md", "fallback"); got != strings.TrimSpace(live) {
		t.Fatalf("live artifact changed: %q", got)
	}
}
