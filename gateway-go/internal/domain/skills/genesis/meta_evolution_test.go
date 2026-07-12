package genesis

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
)

// The deterministic contract gate is what stands between an LLM proposal and
// the .proposed file — it must reject schema-breaking, oversized, and no-op
// revisions regardless of how plausible the prose reads.
func TestMetaProposalGate(t *testing.T) {
	incumbent := strings.Repeat("현재 프롬프트 내용. ", 30)
	valid := incumbent + `
## 출력 (JSON만)
{"skip": false, "changes": {"body": "...", "new_version": "0.1.1", "target_signature": "...", "reproduction_case": {}}}
{"skip": true, "tool_gap": {"tool": "..."}}`

	cases := []struct {
		name     string
		artifact string
		proposal string
		wantPass bool
		wantIn   string
	}{
		{"valid evolve revision passes", generation.MetaEvolveSystemPrompt, valid, true, ""},
		{"identical proposal rejected", generation.MetaEvolveSystemPrompt, incumbent, false, "identical"},
		{"short stump rejected", generation.MetaEvolveSystemPrompt, "too short", false, "too short"},
		{"oversized rejected", generation.MetaEvolveSystemPrompt, valid + strings.Repeat("x", metaProposalMaxBytes), false, "too large"},
		{
			"dropped schema anchor rejected", generation.MetaEvolveSystemPrompt,
			strings.ReplaceAll(valid, `"reproduction_case"`, `"repro"`), false, "anchor",
		},
		{
			"judge contract enforced", generation.MetaSkillJudgeSystemPrompt,
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
	if epoch != metaEpochProducer || artifact != generation.MetaEvolveSystemPrompt {
		t.Fatalf("first cycle = %s/%s, want producer/evolve", epoch, artifact)
	}
	if err := tr.LogMetaRevision(MetaRevisionRecord{Epoch: epoch, Artifact: artifact, FromVersion: "aaa"}); err != nil {
		t.Fatal(err)
	}
	epoch, artifact = task.nextEpoch()
	if epoch != metaEpochEvaluator || artifact != generation.MetaSkillJudgeSystemPrompt {
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
		{Epoch: metaEpochProducer, Artifact: generation.MetaEvolveSystemPrompt, FromVersion: "v1", Reason: "first"},
		{Epoch: metaEpochEvaluator, Artifact: generation.MetaSkillJudgeSystemPrompt, FromVersion: "v2", ToVersion: "v3", Proposed: true, Reason: "tightened scoring rubric"},
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
	evidence := task.assembleEvidence(metaEpochProducer)
	if !strings.Contains(evidence, "tightened scoring rubric") {
		t.Fatalf("evidence lacks meta-experience memory:\n%s", evidence)
	}
	if !strings.Contains(evidence, "진화 스코어보드") {
		t.Fatalf("evidence lacks health scoreboard:\n%s", evidence)
	}
}

// P3 loop closure: the evaluator epoch grounds a judge-prompt revision on the
// live judge's OWN recent misses (and false-rejects), scoped to the incumbent
// judge version. The producer epoch never sees them; a clean judge yields
// nothing; a version mismatch is excluded.
func TestMetaEvolution_JudgeAccuracyEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	meta := generation.NewMetaArtifacts(t.TempDir(), slog.Default())
	task := &MetaEvolutionTask{Tracker: tr, Meta: meta}

	judgeFallback := generation.DefaultMetaArtifacts()[generation.MetaSkillJudgeSystemPrompt]
	version := meta.Version(generation.MetaSkillJudgeSystemPrompt, judgeFallback)

	// Incumbent judge: caught every blatant section-drop, missed 3/5 subtle
	// safety-drops, and has one suspected false-reject.
	if err := tr.LogJudgeAccuracy(JudgeAccuracyRecord{
		JudgeVersion: version,
		Pairs:        9,
		Correct:      6,
		ByClass: map[string][2]int{
			"section-drop": {4, 4}, // no miss — must not appear
			"safety-drop":  {2, 5}, // 3 missed — must appear
		},
		FalseRejects: []FalseRejectExhibit{{Skill: "sk", RejectReason: "over-tightened"}},
	}); err != nil {
		t.Fatal(err)
	}
	// A DIFFERENT judge version's misses must be ignored (may already be fixed).
	if err := tr.LogJudgeAccuracy(JudgeAccuracyRecord{
		JudgeVersion: "stale-version",
		ByClass:      map[string][2]int{"imperative-drop": {0, 6}},
	}); err != nil {
		t.Fatal(err)
	}

	ev := task.assembleEvidence(metaEpochEvaluator)
	if !strings.Contains(ev, "판정자 최근 오판") {
		t.Fatalf("evaluator epoch lacks the P3 miss block:\n%s", ev)
	}
	if !strings.Contains(ev, "safety-drop") || !strings.Contains(ev, "3/5") {
		t.Fatalf("evaluator epoch lost the missed class/count:\n%s", ev)
	}
	if strings.Contains(ev, "section-drop") {
		t.Fatalf("a fully-caught class leaked into the miss block:\n%s", ev)
	}
	if strings.Contains(ev, "imperative-drop") {
		t.Fatalf("a stale-version miss leaked in:\n%s", ev)
	}
	if !strings.Contains(ev, "false-reject") {
		t.Fatalf("evaluator epoch lost the balancing false-reject signal:\n%s", ev)
	}

	// The producer epoch must NOT be grounded on judge misses (different target).
	if prod := task.assembleEvidence(metaEpochProducer); strings.Contains(prod, "판정자 최근 오판") {
		t.Fatalf("producer epoch was grounded on judge misses:\n%s", prod)
	}
}

// A clean incumbent judge (no misses, no false-rejects) leaves the evaluator
// epoch exactly as it was — the closure is a no-op until labels accumulate.
func TestMetaEvolution_JudgeAccuracyEvidence_CleanJudge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	meta := generation.NewMetaArtifacts(t.TempDir(), slog.Default())
	task := &MetaEvolutionTask{Tracker: tr, Meta: meta}
	judgeFallback := generation.DefaultMetaArtifacts()[generation.MetaSkillJudgeSystemPrompt]
	version := meta.Version(generation.MetaSkillJudgeSystemPrompt, judgeFallback)

	if err := tr.LogJudgeAccuracy(JudgeAccuracyRecord{
		JudgeVersion: version,
		Pairs:        8,
		Correct:      8,
		ByClass:      map[string][2]int{"section-drop": {4, 4}, "safety-drop": {4, 4}},
	}); err != nil {
		t.Fatal(err)
	}
	if ev := task.assembleEvidence(metaEpochEvaluator); strings.Contains(ev, "판정자 최근 오판") {
		t.Fatalf("clean judge produced a miss block:\n%s", ev)
	}
}

// Propose-only invariant: WriteProposal never touches the live artifact.
func TestWriteProposal_DoesNotTouchLiveArtifact(t *testing.T) {
	dir := t.TempDir()
	m := generation.NewMetaArtifacts(dir, slog.Default())
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

// The 7d scoreboard reads the ledger: counts within the window, newest entry
// summarized.
func TestMetaEvolutionHealth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if h := tr.MetaEvolutionHealth(); h.Revisions7d != 0 || h.LastArtifact != "" {
		t.Fatalf("empty ledger scoreboard = %+v", h)
	}
	old := MetaRevisionRecord{Epoch: metaEpochProducer, Artifact: generation.MetaEvolveSystemPrompt, FromVersion: "v0", Reason: "ancient", CreatedAt: 1}
	if err := tr.LogMetaRevision(old); err != nil {
		t.Fatal(err)
	}
	fresh := MetaRevisionRecord{Epoch: metaEpochEvaluator, Artifact: generation.MetaSkillJudgeSystemPrompt, FromVersion: "v1", ToVersion: "v2", Proposed: true, Reason: "recent proposal"}
	if err := tr.LogMetaRevision(fresh); err != nil {
		t.Fatal(err)
	}
	h := tr.MetaEvolutionHealth()
	if h.Revisions7d != 1 || h.Proposed7d != 1 {
		t.Fatalf("window counts = %+v (ancient entry must be excluded)", h)
	}
	if h.LastArtifact != generation.MetaSkillJudgeSystemPrompt || !h.LastProposed || h.LastReason != "recent proposal" {
		t.Fatalf("newest summary = %+v", h)
	}
}

// OnProposal surfacing must not be reachable from skip/rejection paths — only
// a gate-passing proposal calls it. (Direct wiring check: callback rides Run's
// success tail; here we pin the nil-safety contract.)
func TestMetaEvolutionTask_OnProposalNilSafe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	// No Evolver/Meta wired → Run is a no-op and must not touch OnProposal.
	task := &MetaEvolutionTask{Tracker: tr, OnProposal: func(_, _, _, _ string, _ bool) { t.Fatal("OnProposal called on no-op run") }}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// Acceleration knobs: cadence and bench-scale env overrides with sane bounds.
func TestAccelerationKnobs(t *testing.T) {
	task := &MetaEvolutionTask{}
	t.Setenv("DENEB_META_EVOLUTION_INTERVAL_DAYS", "")
	if task.Interval() != 7*24*time.Hour {
		t.Fatalf("default meta interval = %v", task.Interval())
	}
	t.Setenv("DENEB_META_EVOLUTION_INTERVAL_DAYS", "2")
	if task.Interval() != 2*24*time.Hour {
		t.Fatalf("accelerated meta interval = %v", task.Interval())
	}
	t.Setenv("DENEB_META_EVOLUTION_INTERVAL_DAYS", "-1")
	if task.Interval() != 7*24*time.Hour {
		t.Fatalf("invalid override must fall back: %v", task.Interval())
	}

	t.Setenv("DENEB_META_BENCH_SCALE", "")
	if metaBenchScale() != 1 {
		t.Fatalf("default bench scale = %d", metaBenchScale())
	}
	t.Setenv("DENEB_META_BENCH_SCALE", "3")
	if metaBenchScale() != 3 {
		t.Fatalf("bench scale = %d", metaBenchScale())
	}
	t.Setenv("DENEB_META_BENCH_SCALE", "99")
	if metaBenchScale() != 1 {
		t.Fatalf("out-of-bounds scale must fall back: %d", metaBenchScale())
	}

	w := &SkillWorkoutTask{}
	t.Setenv("DENEB_SKILL_WORKOUT_INTERVAL_HOURS", "4")
	if w.Interval() != 4*time.Hour {
		t.Fatalf("workout interval = %v", w.Interval())
	}
	t.Setenv("DENEB_SKILL_WORKOUT_INTERVAL_HOURS", "")
	if w.Interval() != skillWorkoutInterval {
		t.Fatalf("default workout interval = %v", w.Interval())
	}
}

// P5-5: the operator-utility aggregate reads the ledger's Action field and
// classifies 7d feed-card verdicts. Cycle records (Action=="") are excluded;
// ancient entries fall outside the window; adoptionRate = adopted/(adopted+
// rejected); an empty ledger yields a zero value (quiet, not broken).
func TestOperatorUtilitySignals(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	// Empty ledger → zero value (fresh install is quiet).
	if u := tr.OperatorUtilitySignals(); u.Adopted7d != 0 || u.Rejected7d != 0 || u.Reverted7d != 0 || u.AdoptionRate != 0 {
		t.Fatalf("empty ledger utility = %+v", u)
	}
	// Ancient verdict (outside 7d) must be excluded.
	if err := tr.LogMetaRevision(MetaRevisionRecord{Action: "adopted", Reason: "old", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	// Fresh feed-card verdicts across all three action kinds + a cycle record
	// (Action=="") that must NOT count as a verdict.
	now := time.Now().UnixMilli()
	for _, r := range []MetaRevisionRecord{
		{Action: "", Reason: "cycle skipped", CreatedAt: now}, // not a verdict
		{Action: "adopted", Reason: "operator adopted from feed card", CreatedAt: now},
		{Action: "rejected", Reason: "operator rejected from feed card", CreatedAt: now},
		{Action: "auto_adopted", Reason: "bench-gated auto-adoption", CreatedAt: now},
		{Action: "operator_reverted", Reason: "operator reverted", CreatedAt: now},
		{Action: "auto_reverted", Reason: "rollback watch fired", CreatedAt: now},
	} {
		if err := tr.LogMetaRevision(r); err != nil {
			t.Fatal(err)
		}
	}
	u := tr.OperatorUtilitySignals()
	// adopted + auto_adopted = 2; rejected = 1; operator_reverted + auto_reverted = 2.
	if u.Adopted7d != 2 || u.Rejected7d != 1 || u.Reverted7d != 2 {
		t.Fatalf("7d counts = %+v (want adopted=2 rejected=1 reverted=2)", u)
	}
	// adoptionRate = 2/(2+1) ≈ 0.667.
	if math.Abs(u.AdoptionRate-2.0/3.0) > 1e-9 {
		t.Fatalf("adoptionRate = %.4f, want %.4f", u.AdoptionRate, 2.0/3.0)
	}
	if u.LastDecisionAt != now {
		t.Fatalf("lastDecisionAt = %d, want %d", u.LastDecisionAt, now)
	}
}

// P5-5: the evidence block surfaces the advisory verdict line when feed-card
// decisions exist, and is absent (empty string) on a fresh install — the
// data-gated-not-broken distinction. The block must be marked advisory so the
// producer knows it is prose-grounding, not a gate.
func TestMetaEvolution_OperatorUtilityEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	task := &MetaEvolutionTask{Tracker: tr}
	// Fresh install: no verdicts → block is empty (quiet).
	if ev := task.assembleOperatorUtilityEvidence(); ev != "" {
		t.Fatalf("fresh install produced a utility block:\n%s", ev)
	}
	// With verdicts: the block appears in both epochs (advisory is epoch-agnostic).
	now := time.Now().UnixMilli()
	if err := tr.LogMetaRevision(MetaRevisionRecord{Action: "adopted", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := tr.LogMetaRevision(MetaRevisionRecord{Action: "rejected", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	block := task.assembleOperatorUtilityEvidence()
	if block == "" {
		t.Fatal("utility block empty despite verdicts")
	}
	if !strings.Contains(block, "자문") || !strings.Contains(block, "게이트 아님") {
		t.Fatalf("block missing advisory marker:\n%s", block)
	}
	if !strings.Contains(block, "채택 1") || !strings.Contains(block, "기각 1") {
		t.Fatalf("block missing verdict counts:\n%s", block)
	}
	// adoptionRate should render (1/(1+1) = 50%).
	if !strings.Contains(block, "50%") {
		t.Fatalf("block missing adoption rate:\n%s", block)
	}
	// The full assembled evidence must carry the block in BOTH epochs.
	for _, epoch := range []string{metaEpochProducer, metaEpochEvaluator} {
		if ev := task.assembleEvidence(epoch); !strings.Contains(ev, "운영자 피드카드 결정") {
			t.Fatalf("%s epoch evidence lacks utility block:\n%s", epoch, ev)
		}
	}
}

// P5-5: the advisory snapshot survives a JSON round-trip on the ledger record
// — the audit trail records what the operator's verdicts looked like at each
// cycle. A nil pointer (older records) must stay nil, not deserialize to a
// zero struct, so the absence is distinguishable from "all zero".
func TestMetaRevisionRecord_OperatorUtilityRoundTrip(t *testing.T) {
	rec := MetaRevisionRecord{
		Epoch: metaEpochProducer,
		OperatorUtility: &OperatorUtilitySignals{
			Adopted7d: 3, Rejected7d: 1, Reverted7d: 0, AdoptionRate: 0.75,
		},
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var back MetaRevisionRecord
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.OperatorUtility == nil || back.OperatorUtility.Adopted7d != 3 || back.OperatorUtility.AdoptionRate != 0.75 {
		t.Fatalf("round-trip lost advisory data: %+v", back.OperatorUtility)
	}
	// A record without the field must deserialize to nil (absence ≠ zero).
	legacy := []byte(`{"epoch":"producer","createdAt":1}`)
	var legacyRec MetaRevisionRecord
	if err := json.Unmarshal(legacy, &legacyRec); err != nil {
		t.Fatal(err)
	}
	if legacyRec.OperatorUtility != nil {
		t.Fatalf("legacy record got a non-nil utility: %+v", legacyRec.OperatorUtility)
	}
}
