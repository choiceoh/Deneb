package genesis

// Meta evolution — P2 of the RSI roadmap (slow loop, first slice).
//
// A weekly autonomous task proposes ONE meta-artifact revision per cycle,
// alternating producer/evaluator epochs (RQGM). This slice is PROPOSE-ONLY:
// the proposal is written next to the live artifact as <name>.proposed and
// recorded in the meta-experience ledger; it never touches the live file.
// Auto-adoption waits for the deterministic promotion benches (frozen anchor
// preservation, shadow-replay flip gate, judge-degradation gold pairs) —
// until those exist, adoption is an operator/slow-loop decision made by
// moving the .proposed file into place (the sidecar provenance then marks it
// revised, so deploys never clobber it).
//
// Meta-experience memory is mandatory (TPGO: memoryless meta-loops collapse):
// every cycle reads the ledger of prior revisions and their outcomes before
// proposing, so rejected directions are not re-proposed and adopted ones are
// preserved. Deterministic Go owns the gates; the LLM only writes prose.

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// Meta-evolution epochs alternate which half of the pipeline may change in a
// window: the producer prompt (candidate generation) or the evaluator prompt
// (judge). Never both — cadence asymmetry and one-change-per-window (RQGM).
const (
	metaEpochProducer  = "producer"
	metaEpochEvaluator = "evaluator"
)

// metaProposalMaxBytes caps a proposed artifact: prompts beyond this are a
// smell (the compiled defaults are 2-5KB) and would bloat every evolve call.
const metaProposalMaxBytes = 24 * 1024

// metaArtifactContracts are the deterministic anchors a proposed revision must
// preserve per artifact — the response-schema markers the Go parsers depend
// on. A proposal that drops one would silently break the pipeline, so the
// contract gate rejects it outright.
// NOTE: every response-schema addition to a prompt MUST add its anchor here
// in the same PR — a proposal generated against an older incumbent would
// otherwise silently drop the new schema when adopted (near-miss 2026-07-11:
// the first live proposal predated tool_gap and would have erased it).
var metaArtifactContracts = map[string][]string{
	generation.MetaEvolveSystemPrompt: {
		`"skip"`, `"changes"`, `"body"`, `"new_version"`,
		`"target_signature"`, `"reproduction_case"`, `"tool_gap"`,
	},
	generation.MetaSkillJudgeSystemPrompt: {
		`"pass"`, `"original_score"`, `"candidate_score"`, `"reason"`,
	},
}

// MetaRevisionRecord is one meta-experience ledger entry: a proposal (or a
// skipped cycle) for revising a meta artifact, with enough context that the
// next cycle — and later the P2 promotion benches — can reason about it.
type MetaRevisionRecord struct {
	CreatedAt   int64  `json:"createdAt"`
	Epoch       string `json:"epoch"`    // producer | evaluator
	Artifact    string `json:"artifact"` // artifact file name
	FromVersion string `json:"fromVersion"`
	ToVersion   string `json:"toVersion,omitempty"` // set when a proposal was produced
	Proposed    bool   `json:"proposed"`
	Reason      string `json:"reason,omitempty"` // proposal rationale, or skip/rejection cause
	// Action marks operator decisions ("adopted" | "rejected") recorded outside
	// the weekly cycle — meta-experience the next cycles read. Empty on cycle
	// records.
	Action string `json:"action,omitempty"`
	// Evaluator-epoch only: judge-degradation bench outcomes (BabelJudge) for
	// the incumbent and the proposal over the same gold pairs.
	BenchIncumbent *JudgeBenchOutcome `json:"benchIncumbent,omitempty"`
	BenchProposal  *JudgeBenchOutcome `json:"benchProposal,omitempty"`
	// Producer-epoch only: shadow-replay bench (CPE anchor preservation +
	// AgentDevel flip gate over generated candidates).
	BenchShadow *ProducerBenchOutcome `json:"benchShadow,omitempty"`
}

// metaRevisionLogPath mirrors the tracker's data-dir convention.
func (t *Tracker) metaRevisionLogPath() string {
	return filepath.Join(filepath.Dir(t.logPath), "meta_evolution_log.jsonl")
}

// LogMetaRevision appends one cycle outcome to the meta-experience ledger.
func (t *Tracker) LogMetaRevision(rec MetaRevisionRecord) error {
	if rec.CreatedAt == 0 {
		rec.CreatedAt = time.Now().UnixMilli()
	}
	return jsonlstore.Append(t.metaRevisionLogPath(), rec)
}

// RecentMetaRevisions returns the newest ledger entries, newest first.
func (t *Tracker) RecentMetaRevisions(limit int) ([]MetaRevisionRecord, error) {
	if limit <= 0 {
		limit = 10
	}
	entries, err := jsonlstore.Load[MetaRevisionRecord](t.metaRevisionLogPath())
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load meta revisions: %w", err)
	}
	out := make([]MetaRevisionRecord, 0, min(limit, len(entries)))
	for i := len(entries) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, entries[i])
	}
	return out, nil
}

// MetaEvolutionHealth summarizes the slow loop for the health scoreboard.
type MetaEvolutionHealth struct {
	Revisions7d  int    `json:"revisions7d"`
	Proposed7d   int    `json:"proposed7d"`
	LastArtifact string `json:"lastArtifact,omitempty"`
	LastEpoch    string `json:"lastEpoch,omitempty"`
	LastReason   string `json:"lastReason,omitempty"`
	LastProposed bool   `json:"lastProposed,omitempty"`
}

// MetaEvolutionHealth computes the 7-day slow-loop scoreboard from the ledger.
func (t *Tracker) MetaEvolutionHealth() MetaEvolutionHealth {
	var out MetaEvolutionHealth
	entries, err := t.RecentMetaRevisions(50)
	if err != nil || len(entries) == 0 {
		return out
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()
	for _, e := range entries {
		if e.CreatedAt < cutoff {
			continue
		}
		out.Revisions7d++
		if e.Proposed {
			out.Proposed7d++
		}
	}
	newest := entries[0]
	out.LastArtifact = newest.Artifact
	out.LastEpoch = newest.Epoch
	out.LastReason = common.TruncateRunes(newest.Reason, 200)
	out.LastProposed = newest.Proposed
	return out
}

// MetaEvolutionTask is the weekly slow-loop cycle. Registered like the other
// genesis autonomous tasks; a dev/live-test instance writes only under its
// isolated state dir, so no extra production gate is needed for propose-only.
type MetaEvolutionTask struct {
	Evolver *Evolver
	Meta    *generation.MetaArtifacts
	Tracker *Tracker
	Logger  *slog.Logger

	// OnProposal, when set, surfaces a written proposal to the operator (work
	// feed card). Best-effort: surfacing failures never affect the cycle.
	OnProposal func(artifact, epoch, reason, path string)

	// pending bench outcomes for the in-flight cycle's ledger write (set via
	// recordWithBench; Run is single-flight per task so no locking needed).
	pendingBenchIncumbent *JudgeBenchOutcome
	pendingBenchProposal  *JudgeBenchOutcome
	pendingBenchShadow    *ProducerBenchOutcome
}

// Name identifies the task in the autonomous scheduler.
func (t *MetaEvolutionTask) Name() string { return "meta-evolution" }

// Interval is the slow-loop cadence: fast loop 6h, slow loop 7d (roadmap P2).
func (t *MetaEvolutionTask) Interval() time.Duration { return 7 * 24 * time.Hour }

// Run executes one propose-only cycle.
func (t *MetaEvolutionTask) Run(ctx context.Context) error {
	if t.Evolver == nil || t.Meta == nil || t.Tracker == nil {
		return nil
	}
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}

	epoch, artifact := t.nextEpoch()
	fallback := generation.DefaultMetaArtifacts()[artifact]
	incumbent := t.Meta.Load(artifact, fallback)
	fromVersion := t.Meta.Version(artifact, fallback)

	record := func(proposed bool, toVersion, reason string) error {
		err := t.Tracker.LogMetaRevision(MetaRevisionRecord{
			Epoch:          epoch,
			Artifact:       artifact,
			FromVersion:    fromVersion,
			ToVersion:      toVersion,
			Proposed:       proposed,
			Reason:         reason,
			BenchIncumbent: t.pendingBenchIncumbent,
			BenchProposal:  t.pendingBenchProposal,
			BenchShadow:    t.pendingBenchShadow,
		})
		if err != nil {
			logger.Warn("meta-evolution: ledger write failed", "error", err)
		}
		return nil
	}

	evidence := t.assembleEvidence()
	proposal, reason, err := t.propose(ctx, artifact, incumbent, evidence)
	if err != nil {
		logger.Warn("meta-evolution: proposal generation failed", "artifact", artifact, "error", err)
		return record(false, "", "proposal generation failed: "+err.Error())
	}
	if proposal == "" {
		logger.Info("meta-evolution: cycle skipped by producer", "artifact", artifact, "reason", reason)
		return record(false, "", "skip: "+reason)
	}
	if rejectReason := metaProposalGate(artifact, incumbent, proposal); rejectReason != "" {
		logger.Info("meta-evolution: proposal rejected by contract gate",
			"artifact", artifact, "reason", rejectReason)
		return record(false, "", "contract gate rejected: "+rejectReason)
	}

	// Evaluator epoch: the judge-degradation bench is the ONLY fitness for a
	// judge-prompt revision (BabelJudge — a judge must never grade its own
	// revision). Incumbent and proposal replay the same gold pairs; a proposal
	// that regresses or misses the floor is rejected before it is surfaced.
	var benchIncumbent, benchProposal *JudgeBenchOutcome
	if epoch == metaEpochEvaluator {
		verdict := t.judgeBenchExecutor()
		if verdict == nil {
			logger.Warn("meta-evolution: no judge model wired, evaluator proposal dropped")
			return record(false, "", "judge bench unavailable: no model wired")
		}
		pairs := buildJudgeDegradationPairs(t.Evolver.catalogEntries(), judgeBenchMaxPairs)
		inc := runJudgeDegradationBench(ctx, incumbent, pairs, verdict)
		prop := runJudgeDegradationBench(ctx, proposal, pairs, verdict)
		benchIncumbent, benchProposal = &inc, &prop
		if rejectReason := judgeBenchDecision(inc, prop); rejectReason != "" {
			logger.Info("meta-evolution: proposal rejected by judge-degradation bench",
				"artifact", artifact, "incumbentRate", inc.Rate(), "proposalRate", prop.Rate(), "reason", rejectReason)
			return t.recordWithBenches(record, benchIncumbent, benchProposal, nil,
				false, "", "judge bench rejected: "+rejectReason)
		}
		logger.Info("meta-evolution: proposal cleared judge-degradation bench",
			"incumbentRate", inc.Rate(), "proposalRate", prop.Rate(), "pairs", prop.Total)
	}

	// Producer epoch: shadow-replay the same evolve scenarios through both
	// prompts and compare what they PRODUCE (CPE anchor preservation +
	// AgentDevel flip gate). With zero benchable scenarios the proposal stays
	// propose-only surfaced — manual adoption adjudicates until the corpus
	// can bench producer revisions.
	var benchShadow *ProducerBenchOutcome
	if epoch == metaEpochProducer {
		if gen := t.producerShadowExecutor(); gen != nil {
			scenarios := buildProducerShadowScenarios(t.Evolver.catalogEntries(), t.Tracker, producerBenchMaxSkills)
			shadow := runProducerShadowBench(ctx, incumbent, proposal, scenarios, gen)
			benchShadow = &shadow
			if rejectReason := producerBenchDecision(shadow); rejectReason != "" {
				logger.Info("meta-evolution: proposal rejected by producer shadow bench",
					"artifact", artifact, "skills", shadow.Skills, "flips", shadow.Flips, "reason", rejectReason)
				return t.recordWithBenches(record, nil, nil, benchShadow,
					false, "", "shadow bench rejected: "+rejectReason)
			}
			logger.Info("meta-evolution: proposal cleared producer shadow bench",
				"skills", shadow.Skills, "incumbentScore", shadow.IncumbentScore, "proposalScore", shadow.ProposalScore)
		}
	}

	path, werr := t.Meta.WriteProposal(artifact, proposal)
	if werr != nil {
		logger.Warn("meta-evolution: proposal write failed", "artifact", artifact, "error", werr)
		return t.recordWithBenches(record, benchIncumbent, benchProposal, benchShadow,
			false, "", "proposal write failed: "+werr.Error())
	}
	toVersion := generation.ContentSHA256(strings.TrimSpace(proposal))[:12]
	logger.Info("meta-evolution: revision proposed (propose-only — adoption is a separate decision)",
		"artifact", artifact, "epoch", epoch, "from", fromVersion, "to", toVersion, "path", path)
	if t.OnProposal != nil {
		t.OnProposal(artifact, epoch, reason, path)
	}
	return t.recordWithBenches(record, benchIncumbent, benchProposal, benchShadow, true, toVersion, reason)
}

// recordWithBenches stashes the bench outcomes for the closure-based ledger
// writer. The closure owns the shared fields; bench fields ride via the task.
func (t *MetaEvolutionTask) recordWithBenches(record func(bool, string, string) error,
	inc, prop *JudgeBenchOutcome, shadow *ProducerBenchOutcome, proposed bool, toVersion, reason string,
) error {
	t.pendingBenchIncumbent, t.pendingBenchProposal, t.pendingBenchShadow = inc, prop, shadow
	defer func() { t.pendingBenchIncumbent, t.pendingBenchProposal, t.pendingBenchShadow = nil, nil, nil }()
	return record(proposed, toVersion, reason)
}

// nextEpoch alternates producer/evaluator based on the last CYCLE entry —
// operator adopt/reject records (Action != "") don't consume an epoch.
func (t *MetaEvolutionTask) nextEpoch() (string, string) {
	prior, err := t.Tracker.RecentMetaRevisions(10)
	if err == nil {
		for _, p := range prior {
			if p.Action != "" {
				continue
			}
			if p.Epoch == metaEpochProducer {
				return metaEpochEvaluator, generation.MetaSkillJudgeSystemPrompt
			}
			break
		}
	}
	return metaEpochProducer, generation.MetaEvolveSystemPrompt
}

// assembleEvidence builds the compact evidence block the proposal prompt sees:
// the 7d health scoreboard, low-yield levers, and the meta-experience ledger.
func (t *MetaEvolutionTask) assembleEvidence() string {
	var b strings.Builder
	h := t.Tracker.EvolutionHealth()
	fmt.Fprintf(&b, "## 7일 진화 스코어보드\n- evolve %d건 (기각 %d, 롤백 %d, 확인 %d), confirmRate %.2f, falseAcceptRate %.2f (해소 %d건)\n",
		h.Evolves7d, h.EvolveRejected7d, h.EvolveRolledBack7d, h.EvolveConfirmed7d,
		h.ConfirmRate, h.FalseAcceptRate, h.ResolvedEvolves7d)
	if h.LastRejectedReason != "" {
		fmt.Fprintf(&b, "- 최근 기각: %s — %s\n", h.LastRejectedSkill, common.TruncateRunes(h.LastRejectedReason, 200))
	}
	if levers, err := t.Tracker.LowYieldLevers(3, 2, 0.5); err == nil && len(levers) > 0 {
		b.WriteString("\n## 저수율 레버 (반복 커밋되나 확인율 낮음)\n")
		for _, lv := range levers {
			fmt.Fprintf(&b, "- %s/%s: committed %d, confirmed %d, rolledBack %d (rate %.2f)\n",
				common.TruncateRunes(lv.Signature, 80), lv.Surface, lv.Committed, lv.Confirmed, lv.RolledBack, lv.ConfirmRate)
		}
	}
	if prior, err := t.Tracker.RecentMetaRevisions(5); err == nil && len(prior) > 0 {
		b.WriteString("\n## 이전 메타 수정 이력 (meta-experience — 기각된 방향 반복 금지)\n")
		for _, p := range prior {
			status := "제안됨"
			if !p.Proposed {
				status = "불발"
			}
			if p.Action != "" {
				status = "오퍼레이터 " + p.Action
			}
			fmt.Fprintf(&b, "- [%s] %s %s→%s: %s (%s)\n",
				p.Epoch, p.Artifact, p.FromVersion, p.ToVersion, common.TruncateRunes(p.Reason, 160), status)
		}
	}
	return b.String()
}

// metaProposalResp is the producer's verdict for a meta cycle.
type metaProposalResp struct {
	Skip          bool   `json:"skip"`
	Reason        string `json:"reason,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// propose asks the strongest wired model for one targeted artifact revision.
// Returns ("", reason, nil) for an explicit skip.
func (t *MetaEvolutionTask) propose(ctx context.Context, artifact, incumbent, evidence string) (string, string, error) {
	client, model := t.Evolver.teacherModelSnapshot()
	if client == nil {
		client, model = t.Evolver.primaryModel()
	}
	if client == nil {
		return "", "", fmt.Errorf("meta-evolution: no model wired")
	}
	userPrompt := fmt.Sprintf(`## 대상 아티팩트: %s

## 현재 내용
%s

## 증거
%s

위 증거에 근거해 이 시스템 프롬프트에서 딱 한 가지 약점을 고르고, 그 약점만 고친 전체 개정본을 revised_prompt로 제안하세요. 고칠 확신이 없으면 skip하세요.`,
		artifact, incumbent, evidence)
	text, err := client.Complete(ctx, llm.ChatRequest{
		Model:          model,
		Messages:       []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:         llm.SystemString(metaEvolutionSystemPrompt),
		MaxTokens:      12288,
		Temperature:    evolveTemperature(),
		Thinking:       t.Evolver.thinkingOff(model),
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return "", "", fmt.Errorf("meta-evolution LLM call: %w", err)
	}
	resp, perr := jsonutil.UnmarshalLLM[metaProposalResp](text)
	if perr != nil {
		return "", "", fmt.Errorf("meta-evolution: parse response (tail=%q): %w", tailRunes(text, 120), perr)
	}
	if resp.Skip || strings.TrimSpace(resp.RevisedPrompt) == "" {
		return "", strings.TrimSpace(resp.Reason), nil
	}
	return strings.TrimSpace(resp.RevisedPrompt), strings.TrimSpace(resp.Reason), nil
}

// metaProposalGate is the deterministic acceptance contract for a proposal.
// Returns "" when the proposal is admissible, else the rejection reason.
func metaProposalGate(artifact, incumbent, proposal string) string {
	trimmed := strings.TrimSpace(proposal)
	if len(trimmed) < generation.MetaArtifactMinBytes {
		return fmt.Sprintf("proposal too short (%d bytes < %d floor)", len(trimmed), generation.MetaArtifactMinBytes)
	}
	if len(trimmed) > metaProposalMaxBytes {
		return fmt.Sprintf("proposal too large (%d bytes > %d cap)", len(trimmed), metaProposalMaxBytes)
	}
	if trimmed == strings.TrimSpace(incumbent) {
		return "proposal identical to incumbent"
	}
	for _, anchor := range metaArtifactContracts[artifact] {
		if !strings.Contains(trimmed, anchor) {
			return fmt.Sprintf("response-schema anchor %s missing — Go parser contract broken", anchor)
		}
	}
	return ""
}

// metaEvolutionSystemPrompt governs the slow loop's producer. Deliberately a
// compiled constant, NOT a meta artifact: the loop must not edit its own
// governor (self-reference guard, at least until P3's verifier co-evolution
// brings independent oversight).
const metaEvolutionSystemPrompt = `당신은 AI 에이전트 자가개선 파이프라인의 메타 개선자입니다.
대상은 스킬을 고치는 프롬프트가 아니라, 스킬 개선 파이프라인 자체를 구동하는 시스템 프롬프트입니다.

## 원칙
1. 한 사이클에 딱 한 가지 약점만 고친다 — 광범위 rewrite 금지, targeted patch만
2. 증거 우선: 스코어보드·저수율 레버·기각 사유가 가리키는 약점만 겨냥한다. 증거가 약하면 skip
3. 출력 JSON 스키마 계약(파서가 읽는 필드명들)은 절대 바꾸지 않는다 — 지시문만 개선한다
4. 이전 메타 수정 이력에서 기각/불발된 방향은 반복하지 않는다
5. 확신이 없으면 skip — 나쁜 메타 수정은 모든 후속 evolve를 오염시킨다

## 출력 (JSON만)
{"skip": false, "reason": "무엇을 왜 고쳤는지 한 문장", "revised_prompt": "개정된 전체 프롬프트 텍스트"}
또는
{"skip": true, "reason": "이유"}`
