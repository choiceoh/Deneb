package genesis

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
)

// H1 regression pin: a producer-epoch proposal must NEVER auto-adopt when no
// producer shadow generator is wired (primary client nil, propose succeeded
// via the teacher fallback). Before the fix the bench block was skipped and
// the proposal fell through to unbenched auto-adoption; the evaluator and
// genesis epochs already dropped in the same situation.
func TestMetaEvolution_ProducerDropsWithoutShadowGenerator(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	// Teacher returns a gate-passing evolve-prompt revision (all response
	// schema anchors preserved). Primary is nil → producerShadowExecutor nil.
	revised := "# evolve\n건전한 개정. 반드시 JSON: " +
		`"skip" "changes" "body" "new_version" "target_signature" "reproduction_case" "tool_gap"` +
		"\n" + strings.Repeat("추가 지침 문장. ", 20)
	payload, _ := json.Marshal(map[string]any{"skip": false, "revised_prompt": revised})
	teacher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeTestSSEJSON(t, w, string(payload))
	}))
	defer teacher.Close()

	e := NewEvolver(nil, skills.NewCatalog(nil), tr, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.SetTeacher(llm.NewClient(teacher.URL, "test-key"), "teacher")

	metaDir := filepath.Join(t.TempDir(), "meta")
	meta := generation.NewMetaArtifacts(metaDir, slog.Default())
	meta.MaterializeDefaults(nil) // seed compiled defaults incl. the evolve prompt

	adopted := false
	task := &MetaEvolutionTask{
		Tracker: tr, Meta: meta, Evolver: e, Logger: slog.Default(),
		OnProposal: func(_, _, _, _ string, isAdoption bool) {
			if isAdoption {
				adopted = true
			}
		},
	}
	// nextEpoch on a fresh ledger returns producer.
	if epoch, _ := task.nextEpoch(); epoch != metaEpochProducer {
		t.Fatalf("precondition: first epoch = %q, want producer", epoch)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if adopted {
		t.Fatal("producer epoch auto-adopted with no shadow bench (H1)")
	}
	ledger, err := tr.RecentMetaRevisions(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger) == 0 || !strings.Contains(ledger[0].Reason, "producer bench unavailable") {
		t.Fatalf("expected a drop record, ledger head = %+v", ledger)
	}
}

// The deterministic contract gate is what stands between an LLM proposal and
// the .proposed file — it must reject schema-breaking, oversized, and no-op
// revisions regardless of how plausible the prose reads.
// A skip cycle carries no bench by default; with DENEB_META_BENCH_ON_SKIP=1
// (set by the P5-2 calibration drop-in) it benches the INCUMBENT alone so the
// calibration ladder keeps accumulating per-epoch samples even when the
// producer has nothing to propose. The cycle must stay a skip either way —
// the incumbent-only run is measurement, never a gate input.
func TestMetaEvolution_SkipCycleBenchesIncumbentOnlyWhenCalibrationKnobSet(t *testing.T) {
	run := func(t *testing.T, knob bool) MetaRevisionRecord {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DENEB_STATE_DIR", t.TempDir())
		if knob {
			t.Setenv("DENEB_META_BENCH_ON_SKIP", "1")
		} else {
			t.Setenv("DENEB_META_BENCH_ON_SKIP", "") // t.Setenv outlives the helper
		}
		tr, err := NewTracker(slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		// Teacher declines to propose → the cycle is a skip.
		payload, _ := json.Marshal(map[string]any{"skip": true, "reason": "개정할 근거 없음"})
		teacher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			writeTestSSEJSON(t, w, string(payload))
		}))
		defer teacher.Close()

		quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
		e := NewEvolver(nil, skills.NewCatalog(nil), tr, "", quiet)
		e.SetTeacher(llm.NewClient(teacher.URL, "test-key"), "teacher")

		meta := generation.NewMetaArtifacts(filepath.Join(t.TempDir(), "meta"), quiet)
		meta.MaterializeDefaults(nil)

		// Rotate to the GENESIS epoch: its shadow-scenario corpus is
		// compiled-in, so the incumbent-only bench has scenarios without any
		// catalog fixture.
		if err := tr.LogMetaRevision(MetaRevisionRecord{Epoch: metaEpochProducer, Artifact: "a", FromVersion: "aaa"}); err != nil {
			t.Fatal(err)
		}
		if err := tr.LogMetaRevision(MetaRevisionRecord{Epoch: metaEpochEvaluator, Artifact: "b", FromVersion: "bbb"}); err != nil {
			t.Fatal(err)
		}
		task := &MetaEvolutionTask{
			Tracker: tr, Meta: meta, Evolver: e, Logger: quiet,
			GenesisGen: func(context.Context, string, string) (string, error) {
				return `{"skip": false, "skill": {"name": "bench-stub", "description": "벤치 스텁", "category": "work", "body": "# stub\n절차."}}`, nil
			},
		}
		if epoch, _ := task.nextEpoch(); epoch != metaEpochGenesis {
			t.Fatalf("precondition: epoch = %q, want genesis", epoch)
		}
		if err := task.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		ledger, err := tr.RecentMetaRevisions(1)
		if err != nil || len(ledger) == 0 {
			t.Fatalf("no cycle record (err=%v)", err)
		}
		head := ledger[0]
		if head.Proposed || !strings.HasPrefix(head.Reason, "skip:") {
			t.Fatalf("cycle must stay a skip: %+v", head)
		}
		return head
	}

	withKnob := run(t, true)
	if withKnob.BenchGenesis == nil || withKnob.BenchGenesis.Scenarios == 0 {
		t.Fatalf("knob on: skip cycle must carry an incumbent-only genesis bench, got %+v", withKnob.BenchGenesis)
	}
	withoutKnob := run(t, false)
	if withoutKnob.BenchGenesis != nil {
		t.Fatalf("knob off: skip cycle must stay benchless, got %+v", withoutKnob.BenchGenesis)
	}
}

// A skip-cycle incumbent-only bench whose every scenario itself skips carries
// ZERO gradable pairs — counting it toward the calibration ladder's per-epoch
// n manufactured invalid samples (2026-07-16: producer skip-bench, 4 skills,
// all "one-sided skip/unparsable"). Such a bench must be dropped, not
// recorded; the cycle stays a benchless skip.
func TestMetaEvolution_SkipCycleZeroSampleBenchIsDropped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	t.Setenv("DENEB_META_BENCH_ON_SKIP", "1")
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	// Teacher declines to propose → the cycle is a skip.
	payload, _ := json.Marshal(map[string]any{"skip": true, "reason": "개정할 근거 없음"})
	teacher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeTestSSEJSON(t, w, string(payload))
	}))
	defer teacher.Close()

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	e := NewEvolver(nil, skills.NewCatalog(nil), tr, "", quiet)
	e.SetTeacher(llm.NewClient(teacher.URL, "test-key"), "teacher")

	meta := generation.NewMetaArtifacts(filepath.Join(t.TempDir(), "meta"), quiet)
	meta.MaterializeDefaults(nil)

	// Rotate to the genesis epoch (compiled-in shadow-scenario corpus).
	if err := tr.LogMetaRevision(MetaRevisionRecord{Epoch: metaEpochProducer, Artifact: "a", FromVersion: "aaa"}); err != nil {
		t.Fatal(err)
	}
	if err := tr.LogMetaRevision(MetaRevisionRecord{Epoch: metaEpochEvaluator, Artifact: "b", FromVersion: "bbb"}); err != nil {
		t.Fatal(err)
	}
	task := &MetaEvolutionTask{
		Tracker: tr, Meta: meta, Evolver: e, Logger: quiet,
		// The genesis producer honestly skips every scenario → zero
		// both-sides-scored pairs.
		GenesisGen: func(context.Context, string, string) (string, error) {
			return `{"skip": true, "reason": "기존 스킬로 충분"}`, nil
		},
	}
	if epoch, _ := task.nextEpoch(); epoch != metaEpochGenesis {
		t.Fatalf("precondition: epoch = %q, want genesis", epoch)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	ledger, err := tr.RecentMetaRevisions(1)
	if err != nil || len(ledger) == 0 {
		t.Fatalf("no cycle record (err=%v)", err)
	}
	if ledger[0].BenchGenesis != nil {
		t.Fatalf("zero-sample skip bench must be dropped, got %+v", ledger[0].BenchGenesis)
	}
}

func TestMetaProposalGateRejectsIdenticalOversizedAndSchemaBreakingProposals(t *testing.T) {
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

// Epoch rotation reads the meta-experience ledger: producer, then evaluator,
// then genesis, then back to producer — one part of the pipeline per window.
func TestNextEpochRotatesProducerEvaluatorGenesisAndIgnoresActionRecords(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
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
	epoch, artifact = task.nextEpoch()
	if epoch != metaEpochGenesis || artifact != generation.MetaGenesisSystemPrompt {
		t.Fatalf("third cycle = %s/%s, want genesis/genesis-prompt", epoch, artifact)
	}
	if err := tr.LogMetaRevision(MetaRevisionRecord{Epoch: epoch, Artifact: artifact, FromVersion: "ccc"}); err != nil {
		t.Fatal(err)
	}
	epoch, _ = task.nextEpoch()
	if epoch != metaEpochProducer {
		t.Fatalf("fourth cycle = %s, want producer (rotation wraps)", epoch)
	}
	// An operator adopt/reject feed-card record carries an EMPTY Epoch (matching
	// workfeed_meta_proposal.go) and must not consume a rotation.
	if err := tr.LogMetaRevision(MetaRevisionRecord{Artifact: artifact, Action: "adopted"}); err != nil {
		t.Fatal(err)
	}
	if epoch, _ = task.nextEpoch(); epoch != metaEpochProducer {
		t.Fatalf("epochless action record consumed an epoch: %s", epoch)
	}
	// A successful auto-adoption stamps Action="auto_adopted" on the CYCLE record
	// (which carries an Epoch) — it MUST still rotate. Otherwise a producer that
	// keeps auto-adopting freezes rotation on producer and the evaluator/genesis
	// prompts are never revised (the bug this guards).
	if err := tr.LogMetaRevision(MetaRevisionRecord{Epoch: metaEpochProducer, Artifact: artifact, Action: "auto_adopted"}); err != nil {
		t.Fatal(err)
	}
	if epoch, _ = task.nextEpoch(); epoch != metaEpochEvaluator {
		t.Fatalf("auto_adopted cycle did not rotate: got %s, want evaluator", epoch)
	}
}

// The ledger is the meta-experience memory — it must survive round-trips and
// surface newest-first, and the evidence block must include it.
func TestMetaRevisionLedgerNewestFirstAndDisplayedInEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
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
	evidence := task.assembleEvidence(context.Background(), metaEpochProducer)
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
func TestAssembleEvidenceLoadsEvaluatorEpochOnIncumbentJudgeMissesOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
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
	if err := tr.logJudgeAccuracy(judgeAccuracyRecord{
		JudgeVersion: version,
		Pairs:        9,
		Correct:      6,
		ByClass: map[string][2]int{
			"section-drop": {4, 4}, // no miss — must not appear
			"safety-drop":  {2, 5}, // 3 missed — must appear
		},
		FalseRejects: []falseRejectExhibit{{Skill: "sk", RejectReason: "over-tightened"}},
	}); err != nil {
		t.Fatal(err)
	}
	// A DIFFERENT judge version's misses must be ignored (may already be fixed).
	if err := tr.logJudgeAccuracy(judgeAccuracyRecord{
		JudgeVersion: "stale-version",
		ByClass:      map[string][2]int{"imperative-drop": {0, 6}},
	}); err != nil {
		t.Fatal(err)
	}

	ev := task.assembleEvidence(context.Background(), metaEpochEvaluator)
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
	if !strings.Contains(ev, "예/아니오 점검 항목") {
		t.Fatalf("evaluator epoch lost the binary-rubric direction hint (BINEVAL):\n%s", ev)
	}

	// The producer epoch must NOT be grounded on judge misses (different target).
	if prod := task.assembleEvidence(context.Background(), metaEpochProducer); strings.Contains(prod, "판정자 최근 오판") {
		t.Fatalf("producer epoch was grounded on judge misses:\n%s", prod)
	}
}

// Organic labels (real-usage false-accepts) ground the evaluator epoch even
// when the synthetic lane has nothing: a baseline-confirmed rollback of an
// evolve the INCUMBENT judge accepted must appear; one accepted by a stale
// judge version must not.
func TestAssembleEvidenceLoadsOrganicFalseAcceptsForIncumbentJudgeVersion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	meta := generation.NewMetaArtifacts(t.TempDir(), slog.Default())
	task := &MetaEvolutionTask{Tracker: tr, Meta: meta}
	judgeFallback := generation.DefaultMetaArtifacts()[generation.MetaSkillJudgeSystemPrompt]
	version := meta.Version(generation.MetaSkillJudgeSystemPrompt, judgeFallback)

	rollback := func(skill, judgeVersion string) {
		t.Helper()
		if err := tr.logEvolveWithProvenance(skill, "1.1", "d", HarnessEditAudit{},
			&evolveProvenance{JudgeArtifactVersion: judgeVersion}); err != nil {
			t.Fatal(err)
		}
		tr.mu.Lock()
		tr.pendingBaselineTest[skill] = &rollbackBaselineTest{Reject: true}
		tr.mu.Unlock()
		if err := tr.logEvolveRolledBack(skill); err != nil {
			t.Fatal(err)
		}
	}
	rollback("sk-live", version)
	rollback("sk-stale", "superseded-version")

	ev := task.assembleEvidence(context.Background(), metaEpochEvaluator)
	if !strings.Contains(ev, "실전 false-accept 1건") || !strings.Contains(ev, "sk-live") {
		t.Fatalf("evaluator epoch lost the organic false-accept label:\n%s", ev)
	}
	if strings.Contains(ev, "sk-stale") {
		t.Fatalf("a stale-judge organic label leaked in:\n%s", ev)
	}
	// The producer epoch stays ungrounded on judge labels (different target).
	if prod := task.assembleEvidence(context.Background(), metaEpochProducer); strings.Contains(prod, "실전 false-accept") {
		t.Fatalf("producer epoch was grounded on organic labels:\n%s", prod)
	}
}

// Category segmentation (evaluator preference collapse, 2606.16682): a
// category-local miss concentration must be named in the evaluator evidence;
// a fully-caught category must not appear.
func TestAssembleEvidenceSurfacesCategorySkewWithoutFullyCaughtCategories(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	meta := generation.NewMetaArtifacts(t.TempDir(), slog.Default())
	task := &MetaEvolutionTask{Tracker: tr, Meta: meta}
	judgeFallback := generation.DefaultMetaArtifacts()[generation.MetaSkillJudgeSystemPrompt]
	version := meta.Version(generation.MetaSkillJudgeSystemPrompt, judgeFallback)

	if err := tr.logJudgeAccuracy(judgeAccuracyRecord{
		JudgeVersion: version,
		Pairs:        8, Correct: 6,
		ByClass:    map[string][2]int{"safety-drop": {6, 8}},
		ByCategory: map[string][2]int{"mail": {2, 4}, "wiki": {4, 4}},
	}); err != nil {
		t.Fatal(err)
	}
	ev := task.assembleEvidence(context.Background(), metaEpochEvaluator)
	if !strings.Contains(ev, "카테고리별 놓침 분포") || !strings.Contains(ev, "mail 2/4") {
		t.Fatalf("category skew line missing:\n%s", ev)
	}
	if strings.Contains(ev, "wiki") {
		t.Fatalf("fully-caught category leaked into the skew line:\n%s", ev)
	}
}

// A clean incumbent judge (no misses, no false-rejects) leaves the evaluator
// epoch exactly as it was — the closure is a no-op until labels accumulate.
func TestAssembleEvidenceLeavesMissBlockEmptyForCleanJudge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	meta := generation.NewMetaArtifacts(t.TempDir(), slog.Default())
	task := &MetaEvolutionTask{Tracker: tr, Meta: meta}
	judgeFallback := generation.DefaultMetaArtifacts()[generation.MetaSkillJudgeSystemPrompt]
	version := meta.Version(generation.MetaSkillJudgeSystemPrompt, judgeFallback)

	if err := tr.logJudgeAccuracy(judgeAccuracyRecord{
		JudgeVersion: version,
		Pairs:        8,
		Correct:      8,
		ByClass:      map[string][2]int{"section-drop": {4, 4}, "safety-drop": {4, 4}},
	}); err != nil {
		t.Fatal(err)
	}
	if ev := task.assembleEvidence(context.Background(), metaEpochEvaluator); strings.Contains(ev, "판정자 최근 오판") {
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
func TestMetaEvolutionHealthWindowsRevisionsAndDisplaysNewest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
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
	// Skip cycles must not inflate 「개정(7일)」.
	if err := tr.LogMetaRevision(MetaRevisionRecord{
		Epoch: metaEpochProducer, Artifact: generation.MetaEvolveSystemPrompt, Reason: "skip: no clear weakness",
	}); err != nil {
		t.Fatal(err)
	}
	h := tr.MetaEvolutionHealth()
	if h.Revisions7d != 1 || h.Proposed7d != 1 {
		t.Fatalf("window counts = %+v (ancient+skip must be excluded; only the proposal counts)", h)
	}
	if h.LastArtifact != generation.MetaEvolveSystemPrompt || h.LastProposed {
		t.Fatalf("newest summary should be the skip row for display, not count as revision: %+v", h)
	}
}

// OnProposal surfacing must not be reachable from skip/rejection paths — only
// a gate-passing proposal calls it. (Direct wiring check: callback rides Run's
// success tail; here we pin the nil-safety contract.)
func TestMetaEvolutionTask_OnProposalNilSafe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
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
func TestIntervalAndBenchScaleEnvOverridesWithBoundsFallback(t *testing.T) {
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
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	// Empty ledger → zero value (fresh install is quiet).
	if u := tr.operatorUtilitySignals(); u.Adopted7d != 0 || u.Rejected7d != 0 || u.Reverted7d != 0 || u.AdoptionRate != 0 {
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
	u := tr.operatorUtilitySignals()
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
func TestAssembleOperatorUtilityEvidenceAdvisoryBlockAppearsInBothEpochsWhenVerdictsExist(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
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
		if ev := task.assembleEvidence(context.Background(), epoch); !strings.Contains(ev, "운영자 피드카드 결정") {
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
		OperatorUtility: &operatorUtilitySignals{
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

// P5-5: the runtime-health advisory block appears in the evidence when
// RuntimeHealth is wired and returns non-empty; it is absent when the closure
// is nil (dev without agentlog) or returns empty (fresh install). The block
// must be marked advisory so the producer knows it is prose-grounding, not a
// gate.
func TestAssembleEvidenceRuntimeHealthBlockAbsentUnlessClosureNonEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	// Nil closure → block absent (quiet).
	nilTask := &MetaEvolutionTask{Tracker: tr}
	if ev := nilTask.assembleEvidence(context.Background(), metaEpochProducer); strings.Contains(ev, "런타임 건강") {
		t.Fatalf("nil RuntimeHealth produced a block:\n%s", ev)
	}
	// Empty closure → block absent (fresh install quiet).
	emptyTask := &MetaEvolutionTask{Tracker: tr, RuntimeHealth: func(context.Context) string { return "" }}
	if ev := emptyTask.assembleEvidence(context.Background(), metaEpochProducer); strings.Contains(ev, "런타임 건강") {
		t.Fatalf("empty RuntimeHealth produced a block:\n%s", ev)
	}
	// Non-empty → block present in BOTH epochs, marked advisory.
	wiredTask := &MetaEvolutionTask{
		Tracker: tr,
		RuntimeHealth: func(context.Context) string {
			return "- p95 8200ms, 오류율 2.1%"
		},
	}
	for _, epoch := range []string{metaEpochProducer, metaEpochEvaluator} {
		ev := wiredTask.assembleEvidence(context.Background(), epoch)
		if !strings.Contains(ev, "런타임 건강") {
			t.Fatalf("%s epoch evidence lacks runtime-health block:\n%s", epoch, ev)
		}
		if !strings.Contains(ev, "자문") || !strings.Contains(ev, "게이트 아님") {
			t.Fatalf("%s epoch runtime-health block missing advisory marker:\n%s", epoch, ev)
		}
		if !strings.Contains(ev, "8200ms") {
			t.Fatalf("%s epoch runtime-health block lost the closure content:\n%s", epoch, ev)
		}
	}
}

// P5-5: the codebase-health advisory block appears when QualityBench is wired
// and returns non-empty; absent when nil or empty. Both epochs carry it.
func TestAssembleEvidenceQualityBenchBlockAbsentUnlessClosureNonEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	// Nil closure → absent.
	nilTask := &MetaEvolutionTask{Tracker: tr}
	if ev := nilTask.assembleEvidence(context.Background(), metaEpochProducer); strings.Contains(ev, "코드베이스 건강") {
		t.Fatalf("nil QualityBench produced a block")
	}
	// Non-empty → present in both epochs.
	wired := &MetaEvolutionTask{
		Tracker: tr,
		QualityBench: func(context.Context) string {
			return "- 종합 82.7, 약한 기둥: change-locality 55"
		},
	}
	for _, epoch := range []string{metaEpochProducer, metaEpochEvaluator} {
		ev := wired.assembleEvidence(context.Background(), epoch)
		if !strings.Contains(ev, "코드베이스 건강") {
			t.Fatalf("%s epoch lacks quality-bench block", epoch)
		}
		if !strings.Contains(ev, "82.7") {
			t.Fatalf("%s epoch lost closure content", epoch)
		}
	}
}

// Low-confidence routing (ANCHOR 2606.06114): a bench that cannot show a
// measurable improvement (margin <= 0) routes the adoption to the operator;
// an improving bench (or a benchless cycle) does not.
func TestMetaLowConfidenceReasonReturnsFlatOrEqualMarginsNotImprovingOnes(t *testing.T) {
	worse := &judgeBenchOutcome{Correct: 8, Total: 10}
	same := &judgeBenchOutcome{Correct: 8, Total: 10}
	better := &judgeBenchOutcome{Correct: 9, Total: 10}
	if metaLowConfidenceReason(worse, same, nil, nil) == "" {
		t.Fatal("equal judge margin must be low-confidence")
	}
	if metaLowConfidenceReason(worse, better, nil, nil) != "" {
		t.Fatal("improving judge margin must be confident")
	}
	// Scored benches carry Skills ≥ 1 by construction; Skills == 0 is the
	// separate "nothing scored" reason.
	if metaLowConfidenceReason(nil, nil, &producerBenchOutcome{Skills: 1, IncumbentScore: 0.6, ProposalScore: 0.6}, nil) == "" {
		t.Fatal("flat shadow margin must be low-confidence")
	}
	if metaLowConfidenceReason(nil, nil, &producerBenchOutcome{Skills: 1, IncumbentScore: 0.5, ProposalScore: 0.7}, nil) != "" {
		t.Fatal("improving shadow margin must be confident")
	}
	if metaLowConfidenceReason(nil, nil, nil, nil) != "" {
		t.Fatal("benchless cycle keeps documented behavior (not routed)")
	}
}

// annotateReason: an empty producer reason must not leave a dangling " — "
// in verdict cards or the ledger.
func TestAnnotateReasonJoinsWithEmDashOrReturnsBareNoteWhenEmpty(t *testing.T) {
	if got := annotateReason("", "저신뢰 라우팅: margin 0.8→0.8"); got != "저신뢰 라우팅: margin 0.8→0.8" {
		t.Fatalf("empty reason must yield the bare note, got %q", got)
	}
	if got := annotateReason("  ", "note"); got != "note" {
		t.Fatalf("whitespace reason must yield the bare note, got %q", got)
	}
	if got := annotateReason("judge drift", "note"); got != "judge drift — note" {
		t.Fatalf("non-empty reason must join with em-dash, got %q", got)
	}
}
