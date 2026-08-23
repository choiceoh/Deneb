package genesis

// Meta evolution — P2 of the RSI roadmap (slow loop).
//
// A weekly autonomous task proposes ONE meta-artifact revision per cycle,
// alternating producer/evaluator epochs (RQGM). Every proposal clears the
// deterministic promotion benches for its epoch (judge-degradation gold pairs
// for evaluator, shadow-replay flip for producer) before it is surfaced.
//
// Adoption is now AUTOMATIC by default (DENEB_META_AUTO_ADOPT, on unless "0"):
// a bench-cleared proposal is written straight over the live artifact, an
// evolution-health snapshot arms the meta rollback watch, and the feed card
// carries a post-hoc revert. When auto-adopt is off, the proposal is written
// next to the live artifact as <name>.proposed and adoption is an operator
// decision (moving the file into place; the sidecar provenance then marks it
// revised, so deploys never clobber it). Either way the drift self-brake
// (evolution_drift.go) can freeze auto-adoption back to propose-only.
//
// Meta-experience memory is mandatory (TPGO: memoryless meta-loops collapse):
// every cycle reads the ledger of prior revisions and their outcomes before
// proposing, so rejected directions are not re-proposed and adopted ones are
// preserved. Deterministic Go owns the gates; the LLM only writes prose.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// Meta-evolution epochs rotate which part of the pipeline may change in a
// window: the producer prompt (evolve candidate generation), the evaluator
// prompt (judge), or the genesis prompt (new-skill extraction — RSI P5-4
// slice 2, the third artifact in the rotation). Never more than one —
// cadence asymmetry and one-change-per-window (RQGM).
const (
	metaEpochProducer  = "producer"
	metaEpochEvaluator = "evaluator"
	metaEpochGenesis   = "genesis"
)

// metaProposalMaxBytes caps a proposed artifact: prompts beyond this are a
// smell (the compiled defaults are 2-5KB) and would bloat every evolve call.
const metaProposalMaxBytes = 24 * 1024

// judgeMissEvidenceRuns bounds how many recent judge-accuracy lane runs the
// evaluator epoch reads to ground a judge-prompt revision on the judge's own
// labeled mistakes (P3). 10 runs at the accelerated 4h cadence ≈ recent behavior.
const judgeMissEvidenceRuns = 10

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
	generation.MetaGenesisSystemPrompt: {
		`"skip"`, `"skill"`, `"name"`, `"category"`, `"description"`, `"body"`,
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
	// Action marks adoption-lifecycle records outside the propose step:
	// "auto_adopted" | "adopted" | "rejected" | "auto_reverted" |
	// "operator_reverted" — meta-experience the next cycles read. Empty on
	// cycle records.
	Action string `json:"action,omitempty"`
	// AdoptionHealth snapshots the 7d evolution health at adoption time — the
	// revert watch compares the current window against it.
	AdoptionHealth *MetaAdoptionHealth `json:"adoptionHealth,omitempty"`
	// Evaluator-epoch only: judge-degradation bench outcomes (BabelJudge) for
	// the incumbent and the proposal over the same gold pairs.
	BenchIncumbent *judgeBenchOutcome `json:"benchIncumbent,omitempty"`
	BenchProposal  *judgeBenchOutcome `json:"benchProposal,omitempty"`
	// Producer-epoch only: shadow-replay bench (CPE anchor preservation +
	// AgentDevel flip gate over generated candidates).
	BenchShadow *producerBenchOutcome `json:"benchShadow,omitempty"`
	// Genesis-epoch only: genesis shadow bench (fixed scenarios scored by the
	// production admissibility gate — RSI P5-4 slice 2).
	BenchGenesis *genesisBenchOutcome `json:"benchGenesis,omitempty"`
	// OperatorUtility is ADVISORY-ONLY (never a gate input, P5-5): what the
	// operator's feed-card accept/reject verdicts looked like at this cycle.
	// Recorded for diagnosis/audit; the deterministic gates ignore it. Mirrors
	// the subtle-vs-blatant judge-degradation split — informs the producer's
	// prose, never decides adoption.
	OperatorUtility *operatorUtilitySignals `json:"operatorUtility,omitempty"`
	// RevisionClass classifies a PROPOSED revision against its incumbent:
	// "structural" | "parametric" (L1.5-trap telemetry, Bilevel Autoresearch
	// 2603.23420 — parameter-level tweaks showed no reliable gain there). Set
	// on cycle records that produced a proposal; ADVISORY, no gate reads it.
	RevisionClass string `json:"revisionClass,omitempty"`
}

// operatorUtilitySignals summarizes 7d operator feed-card decisions for the
// meta-evidence block. ADVISORY ONLY (P5-5): grounds the producer's prose on
// operator-visible utility; no gate reads it. Feed-card decisions are already
// ledgered as MetaRevisionRecord.Action entries (adopted/rejected/
// operator_reverted) — this is a read-side aggregate, not a new signal source.
type operatorUtilitySignals struct {
	Adopted7d  int `json:"adopted7d"`
	Rejected7d int `json:"rejected7d"`
	Reverted7d int `json:"reverted7d"`
	// Expired7d counts proposals whose verdict window closed with no operator
	// decision — "no verdict" is its own label, not silence.
	Expired7d      int     `json:"expired7d,omitempty"`
	AdoptionRate   float64 `json:"adoptionRate"` // adopted/(adopted+rejected), 0 when no verdicts
	LastDecisionAt int64   `json:"lastDecisionAt,omitempty"`
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

// metaLedgerFloorMs is the earliest CreatedAt a meta-revision row can honestly
// carry: the slow loop did not exist before P1 landed (2026-07-11, #3430), so a
// row stamped earlier is not a revision — it is contamination. On 2026-08-16 a
// genesis package test run wrote fixtures (createdAt≈1000, artifact "evolve.md",
// action auto_adopted) into the production ledger; two days later the drift
// brake read them as "20 consecutive adoptions of the same artifact" and froze
// auto-adoption for a third time on evidence that was never real.
var metaLedgerFloorMs = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC).UnixMilli()

// metaLedgerFutureSlackMs is how far past "now" a row may be stamped before it
// is implausible (clock skew tolerance; the ledger is append-only and local).
const metaLedgerFutureSlackMs = int64(24 * time.Hour / time.Millisecond)

// metaRevisionPlausible reports whether a ledger row sits inside the time band a
// real revision can occupy. Every consumer reads the ledger through
// RecentMetaRevisions, so this one predicate keeps the drift brake, the epoch
// rotation, the class balance and the status surface on the same evidence.
// Rows are excluded, never deleted — quarantining the file is an operator act.
func metaRevisionPlausible(r MetaRevisionRecord, nowMs int64) bool {
	return r.CreatedAt >= metaLedgerFloorMs && r.CreatedAt <= nowMs+metaLedgerFutureSlackMs
}

// RecentMetaRevisions returns the newest plausible ledger entries, newest first.
// Implausible rows (outside the metaRevisionPlausible band) are skipped and
// counted — see MetaLedgerImplausibleRows — so contaminated rows can never
// again drive a freeze, a rotation, or a status line.
func (t *Tracker) RecentMetaRevisions(limit int) ([]MetaRevisionRecord, error) {
	if limit <= 0 {
		limit = 10
	}
	entries, err := jsonlstore.Load[MetaRevisionRecord](t.metaRevisionLogPath())
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load meta revisions: %w", err)
	}
	nowMs := time.Now().UnixMilli()
	out := make([]MetaRevisionRecord, 0, min(limit, len(entries)))
	implausible := 0
	for i := len(entries) - 1; i >= 0; i-- {
		if !metaRevisionPlausible(entries[i], nowMs) {
			implausible++
			continue
		}
		if len(out) < limit {
			out = append(out, entries[i])
		}
	}
	t.noteMetaLedgerImplausible(implausible)
	return out, nil
}

// noteMetaLedgerImplausible records the exclusion count of the latest ledger
// read and warns once per distinct count (not per read — the ledger is read on
// every status call).
func (t *Tracker) noteMetaLedgerImplausible(n int) {
	t.metaLedgerImplausible.Store(int64(n))
	if n == 0 {
		return
	}
	if prev := t.metaLedgerImplausibleLogged.Swap(int64(n)); prev != int64(n) && t.logger != nil {
		t.logger.Warn("meta-evolution ledger: implausible rows excluded from every consumer — quarantine them, they are contamination not revisions",
			"rows", n, "floor", time.UnixMilli(metaLedgerFloorMs).UTC().Format("2006-01-02"))
	}
}

// MetaLedgerImplausibleRows reports how many meta-revision rows the latest
// ledger read excluded as implausible (0 = clean ledger). Surfaced on
// rsi_status L2 so a contaminated ledger is visible instead of silently
// shaping the loop.
func (t *Tracker) MetaLedgerImplausibleRows() int {
	return int(t.metaLedgerImplausible.Load())
}

// MetaAdoptionHealth is the evolution-health snapshot recorded with an
// adoption, and the deterministic basis for the meta rollback watch.
type MetaAdoptionHealth struct {
	ConfirmRate     float64 `json:"confirmRate"`
	FalseAcceptRate float64 `json:"falseAcceptRate"`
	Resolved        int     `json:"resolved"`
}

// Meta rollback watch thresholds: revert an adoption when the CURRENT 7d
// window regresses this hard against the adoption snapshot with a minimum
// sample. Conservative on purpose — the benches already gated the adoption;
// this net catches what they missed.
const (
	metaRevertMinResolved     = 4
	metaRevertFARJump         = 0.25
	metaRevertConfirmDrop     = 0.30
	metaRevertWatchWindowDays = 14
)

// metaBenchScale multiplies both bench corpora (judge gold pairs, producer
// shadow scenarios) — DENEB_META_BENCH_SCALE, default 1. Benches are synthetic
// and clock-free, so scale is bounded only by weekly LLM budget.
func metaBenchScale() int {
	if v := strings.TrimSpace(os.Getenv("DENEB_META_BENCH_SCALE")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 10 {
			return n
		}
	}
	return 1
}

// metaAutoAdoptEnabled reports whether bench-gated proposals adopt themselves
// (operator mandate 2026-07-11: no human approval in the loop). Kill switch:
// DENEB_META_AUTO_ADOPT=0.
func metaAutoAdoptEnabled() bool {
	return os.Getenv("DENEB_META_AUTO_ADOPT") != "0"
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
		// Skip cycles (Action=="" && !Proposed) are not revisions — counting
		// them inflated L2 「개정(7일)」 whenever the producer declined to patch.
		if e.Action == "" && !e.Proposed {
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

// operatorUtilitySignals summarizes 7d operator feed-card decisions from the
// meta-experience ledger. ADVISORY ONLY (P5-5): surfaces what the operator
// accepted/rejected/reverted to the meta-evidence block; never read by any
// gate, drift brake, or promotion bench. Feed-card decisions are ledgered as
// MetaRevisionRecord.Action entries — this aggregates that field.
func (t *Tracker) operatorUtilitySignals() operatorUtilitySignals {
	var out operatorUtilitySignals
	entries, err := t.RecentMetaRevisions(50)
	if err != nil || len(entries) == 0 {
		return out
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()
	for _, e := range entries {
		if e.CreatedAt < cutoff {
			continue
		}
		switch e.Action {
		case "adopted", "auto_adopted":
			out.Adopted7d++
		case "rejected":
			out.Rejected7d++
		case "operator_reverted", "auto_reverted":
			out.Reverted7d++
		case metaActionExpired:
			out.Expired7d++
			continue // not a decision — it must not move LastDecisionAt
		default:
			continue // cycle records (Action=="") are not feed-card verdicts
		}
		if e.CreatedAt > out.LastDecisionAt {
			out.LastDecisionAt = e.CreatedAt
		}
	}
	verdicts := out.Adopted7d + out.Rejected7d
	if verdicts > 0 {
		out.AdoptionRate = float64(out.Adopted7d) / float64(verdicts)
	}
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

	// OnProposal, when set, surfaces a written proposal (adopted=false: awaiting
	// the operator; adopted=true: auto-adopted notification with a revert veto).
	// Best-effort: surfacing failures never affect the cycle.
	OnProposal func(artifact, epoch, reason, path string, adopted bool)
	// OnProposalExpired, when set, lets the server settle the feed card of a
	// proposal whose operator verdict never came (see expireStaleProposals).
	OnProposalExpired func(artifact, path, reason string)
	// OnReverted, when set, notifies the operator that the meta rollback watch
	// reverted an adoption.
	OnReverted func(artifact, reason string)
	// OnDriftFreeze, when set, notifies the operator when the self-brake
	// engages or releases (auto-adopt freeze transition).
	OnDriftFreeze func(frozen bool, reasons []string)
	// RuntimeHealth, when set, injects a compact runtime-health summary (p95
	// latency, error rate, timeout/tool-error signals) as ADVISORY evidence
	// into the meta-evidence block (RSI P5-5). Grounds the producer's prose on
	// operator-experienced runtime utility; no gate reads it. Nil = skip.
	RuntimeHealth func(ctx context.Context) string
	// QualityBench, when set, injects a compact codebase-health summary
	// (overall score, weakest pillars) as ADVISORY evidence (RSI P5-5). Grounds
	// the producer on structural quality; no gate reads it. Nil = skip.
	QualityBench func(ctx context.Context) string
	// RSIBench, when set, injects a compact RSI Bench summary (process+utility)
	// as ADVISORY evidence (P5-5). Grounds the producer on loop compounding;
	// no gate reads it. Nil = skip.
	RSIBench func(ctx context.Context) string
	// GenesisGen, when set, executes one genesis generation with an explicit
	// system prompt on the PRODUCTION genesis model — the genesis-epoch shadow
	// bench's executor (server wires generation.Service.ShadowGenerate). Nil
	// drops genesis-epoch proposals (bench unavailable), mirroring the
	// evaluator epoch's no-judge behavior.
	GenesisGen genesisShadowGenFn

	// pending bench outcomes for the in-flight cycle's ledger write (set via
	// recordWithBench; Run is single-flight per task so no locking needed).
	pendingBenchIncumbent *judgeBenchOutcome
	pendingBenchProposal  *judgeBenchOutcome
	pendingBenchShadow    *producerBenchOutcome
	pendingBenchGenesis   *genesisBenchOutcome
	pendingAdoptionHealth *MetaAdoptionHealth
	pendingAction         string
	// pendingOperatorUtility is the ADVISORY snapshot stashed for the cycle's
	// ledger write (P5-5); set once in Run, copied into MetaRevisionRecord.
	pendingOperatorUtility *operatorUtilitySignals
	// pendingRevisionClass is the structural/parametric classification of the
	// in-flight proposal (L1.5-trap telemetry); set after the contract gate.
	pendingRevisionClass string
}

// Name identifies the task in the autonomous scheduler.
func (t *MetaEvolutionTask) Name() string { return "meta-evolution" }

// Interval is the slow-loop cadence: fast loop 6h, slow loop 7d (roadmap P2).
// DENEB_META_EVOLUTION_INTERVAL_DAYS accelerates the calibration phase — the
// one-change-per-window principle (RQGM) holds at any cadence, and the revert
// watch's min-sample gate keeps thin windows from mis-firing.
func (t *MetaEvolutionTask) Interval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("DENEB_META_EVOLUTION_INTERVAL_DAYS")); v != "" {
		if days, err := strconv.Atoi(v); err == nil && days > 0 {
			return time.Duration(days) * 24 * time.Hour
		}
	}
	return 7 * 24 * time.Hour
}

// metaActionExpired is the ledger action for a proposal whose verdict window
// closed without an operator decision. It is not a verdict: nothing was
// adopted or rejected on the merits, the proposal was simply discarded so the
// lane can re-propose from fresh evidence instead of holding a stale card.
const metaActionExpired = "expired"

// defaultMetaVerdictExpiryDays bounds how long a propose-only / low-confidence
// proposal may wait for an operator verdict. 2026-07-13 → 08-22: the feed-card
// verdict channel produced 0 adopt / 0 reject for the whole calibration
// window while low-confidence revisions sat in "운영자 verdict 대기" — and a
// later proposal for the same artifact silently overwrote the pending file
// under the old card. Override with DENEB_META_VERDICT_EXPIRY_DAYS.
const defaultMetaVerdictExpiryDays = 7

func metaVerdictExpiry() time.Duration {
	if v := strings.TrimSpace(os.Getenv("DENEB_META_VERDICT_EXPIRY_DAYS")); v != "" {
		if days, err := strconv.Atoi(v); err == nil && days > 0 {
			return time.Duration(days) * 24 * time.Hour
		}
	}
	return defaultMetaVerdictExpiryDays * 24 * time.Hour
}

// pendingProposalSince returns when the artifact's current proposal started
// waiting: the newest ledger row that PROPOSED it (Proposed && no action), or
// the .proposed file's mtime when the ledger has no such row (legacy files).
func (t *MetaEvolutionTask) pendingProposalSince(artifact string, fileMod time.Time) time.Time {
	if revs, err := t.Tracker.RecentMetaRevisions(50); err == nil {
		for _, r := range revs { // newest first
			if r.Artifact != artifact {
				continue
			}
			if r.Proposed && r.Action == "" {
				return time.UnixMilli(r.CreatedAt)
			}
		}
	}
	return fileMod
}

// expireStaleProposals discards every pending .proposed older than the verdict
// window: the file is removed (RejectProposal), the ledger records
// metaActionExpired, and OnProposalExpired lets the server settle the card.
// Deterministic, no LLM; runs at the top of every cycle so the lane never
// holds more than one verdict window of stale work.
func (t *MetaEvolutionTask) expireStaleProposals(logger *slog.Logger, now time.Time) int {
	if t.Meta == nil || t.Tracker == nil {
		return 0
	}
	expiry := metaVerdictExpiry()
	names := make([]string, 0, len(generation.DefaultMetaArtifacts()))
	for name := range generation.DefaultMetaArtifacts() {
		names = append(names, name)
	}
	sort.Strings(names)
	expired := 0
	for _, name := range names {
		path, mod, ok := t.Meta.ProposalInfo(name)
		if !ok {
			continue
		}
		since := t.pendingProposalSince(name, mod)
		if now.Sub(since) < expiry {
			continue
		}
		if err := t.Meta.RejectProposal(name); err != nil {
			logger.Warn("meta-evolution: stale proposal could not be discarded", "artifact", name, "error", err)
			continue
		}
		days := int(expiry / (24 * time.Hour))
		reason := fmt.Sprintf("verdict expired after %dd without operator decision — proposal discarded (pending since %s); the lane may re-propose from fresh evidence",
			days, since.UTC().Format("2006-01-02"))
		fromVersion := t.Meta.Version(name, generation.DefaultMetaArtifacts()[name])
		if err := t.Tracker.LogMetaRevision(MetaRevisionRecord{
			Artifact:    name,
			FromVersion: fromVersion,
			Action:      metaActionExpired,
			Reason:      reason,
		}); err != nil {
			logger.Warn("meta-evolution: expiry ledger write failed", "artifact", name, "error", err)
		}
		logger.Info("meta-evolution: pending proposal expired without verdict", "artifact", name, "pendingSince", since.UTC().Format(time.RFC3339))
		if t.OnProposalExpired != nil {
			t.OnProposalExpired(name, path, reason)
		}
		expired++
	}
	return expired
}

// Run executes one propose-only cycle.
func (t *MetaEvolutionTask) Run(ctx context.Context) error {
	if t.Evolver == nil || t.Meta == nil || t.Tracker == nil {
		return nil
	}
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}
	// Verdict windows close before a new cycle starts: a stale pending proposal
	// must not survive into (and be silently overwritten by) this cycle.
	t.expireStaleProposals(logger, time.Now())

	t.maybeRevertAdoption(logger)
	t.maybeRevertStormPoisonedEvaluatorAdoption(logger)
	// Self-brake: read the ledgers for reward-hacking/drift signals and freeze
	// auto-adoption if the trajectory has gone bad (meta-monitor). Transition
	// surfaces to the operator via OnDriftFreeze. Use the FRESH verdict at the
	// adopt gate below, not only the persisted marker: if the freeze-transition
	// write failed, AutoAdoptFrozen would still read "not frozen" and the cycle
	// would auto-adopt despite tripped signals (RSI code eval H5).
	driftVerdict := t.Tracker.runEvolutionDriftAudit(t.OnDriftFreeze)

	epoch, artifact := t.nextEpoch()
	fallback := generation.DefaultMetaArtifacts()[artifact]
	incumbent := t.Meta.Load(artifact, fallback)
	fromVersion := t.Meta.Version(artifact, fallback)

	record := func(proposed bool, toVersion, reason string) error {
		err := t.Tracker.LogMetaRevision(MetaRevisionRecord{
			Epoch:           epoch,
			Artifact:        artifact,
			FromVersion:     fromVersion,
			ToVersion:       toVersion,
			Proposed:        proposed,
			Reason:          reason,
			BenchIncumbent:  t.pendingBenchIncumbent,
			BenchProposal:   t.pendingBenchProposal,
			BenchShadow:     t.pendingBenchShadow,
			BenchGenesis:    t.pendingBenchGenesis,
			AdoptionHealth:  t.pendingAdoptionHealth,
			Action:          t.pendingAction,
			OperatorUtility: t.pendingOperatorUtility,
			RevisionClass:   t.pendingRevisionClass,
		})
		if err != nil {
			logger.Warn("meta-evolution: ledger write failed", "error", err)
		}
		return nil
	}

	// P5-5: snapshot operator-visible utility ONCE per cycle (ADVISORY — never
	// a gate input). Computed before evidence assembly so the producer's prose
	// AND the ledger record both see the same picture. Ground rule: the
	// deterministic gates ignore this entirely.
	util := t.Tracker.operatorUtilitySignals()
	t.pendingOperatorUtility = &util

	evidence := t.assembleEvidence(ctx, epoch)
	proposal, reason, err := t.propose(ctx, artifact, incumbent, evidence)
	if err != nil {
		logger.Warn("meta-evolution: proposal generation failed", "artifact", artifact, "error", err)
		return record(false, "", "proposal generation failed: "+err.Error())
	}
	if proposal == "" {
		logger.Info("meta-evolution: cycle skipped by producer", "artifact", artifact, "reason", reason)
		// Calibration-window opt-in (DENEB_META_BENCH_ON_SKIP=1, set by the
		// P5-2 drop-in): a skip cycle normally leaves no bench sample, which
		// starves the calibration ladder's per-epoch n exactly as low-hanging
		// revisions dry up. An incumbent-only bench run is the purest variance
		// sample calibration wants (bench noise, zero artifact delta). NEVER a
		// gate input — the cycle stays a skip regardless of the outcome.
		if inc, shadow, gen := t.benchIncumbentOnSkip(ctx, epoch, incumbent); inc != nil || shadow != nil || gen != nil {
			t.pendingBenchGenesis = gen
			return t.recordWithBenches(record, inc, nil, shadow, false, "", "skip: "+reason)
		}
		return record(false, "", "skip: "+reason)
	}
	if rejectReason := metaProposalGate(artifact, incumbent, proposal); rejectReason != "" {
		logger.Info("meta-evolution: proposal rejected by contract gate",
			"artifact", artifact, "reason", rejectReason)
		return record(false, "", "contract gate rejected: "+rejectReason)
	}
	// L1.5-trap telemetry (ADVISORY): classify the surviving proposal as a
	// structural mechanism change vs a parametric tweak. Ledgered + surfaced;
	// never a gate input.
	revClass, revDetail := classifyMetaRevision(incumbent, proposal)
	t.pendingRevisionClass = revClass
	logger.Info("meta-evolution: proposal classified",
		"artifact", artifact, "class", revClass, "detail", revDetail)

	// Evaluator epoch: the judge-degradation bench is the ONLY fitness for a
	// judge-prompt revision (BabelJudge — a judge must never grade its own
	// revision). Incumbent and proposal replay the same gold pairs; a proposal
	// that regresses or misses the floor is rejected before it is surfaced.
	var benchIncumbent, benchProposal *judgeBenchOutcome
	if epoch == metaEpochEvaluator {
		verdict := t.judgeBenchExecutor()
		if verdict == nil {
			logger.Warn("meta-evolution: no judge model wired, evaluator proposal dropped")
			return record(false, "", "judge bench unavailable: no model wired")
		}
		pairs := buildJudgeDegradationPairs(t.Evolver.catalogEntries(), judgeBenchMaxPairs*metaBenchScale())
		inc := runJudgeDegradationBench(ctx, incumbent, pairs, verdict)
		prop := runJudgeDegradationBench(ctx, proposal, pairs, verdict)
		benchIncumbent, benchProposal = &inc, &prop
		if rejectReason := judgeBenchDecision(inc, prop); rejectReason != "" {
			logger.Info("meta-evolution: proposal rejected by judge-degradation bench",
				"artifact", artifact, "incumbentRate", inc.rate(), "proposalRate", prop.rate(), "reason", rejectReason)
			return t.recordWithBenches(record, benchIncumbent, benchProposal, nil,
				false, "", "judge bench rejected: "+rejectReason)
		}
		logger.Info("meta-evolution: proposal cleared judge-degradation bench",
			"incumbentRate", inc.rate(), "proposalRate", prop.rate(), "pairs", prop.Total)
	}

	// AgentDevel flip gate). Without a wired shadow generator the proposal is
	// DROPPED — a producer revision must never adopt unbenched (mirrors the
	// evaluator/genesis epochs; the previous fall-through auto-adopted with no
	// bench when the primary client was nil, RSI code eval H1). With zero
	// benchable scenarios, the proposal stays propose-only surfaced; manual
	// adoption adjudicates until the corpus can bench producer revisions.
	var benchShadow *producerBenchOutcome
	if epoch == metaEpochProducer {
		gen := t.producerShadowExecutor()
		if gen == nil {
			logger.Warn("meta-evolution: no producer shadow generator wired, producer proposal dropped")
			return record(false, "", "producer bench unavailable: no generator wired")
		}
		scenarios := buildProducerShadowScenarios(t.Evolver.catalogEntries(), t.Tracker, producerBenchMaxSkills*metaBenchScale())
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

	// Genesis epoch: shadow-replay fixed session scenarios through both
	// prompts and score the outputs with the production admissibility gate
	// (RSI P5-4 slice 2). A flip on a scenario the incumbent handles cleanly
	// rejects; without a wired generator the proposal is dropped — a
	// genesis revision must never adopt unbenched (mirrors the evaluator
	// epoch's no-judge behavior).
	var benchGenesis *genesisBenchOutcome
	if epoch == metaEpochGenesis {
		if t.GenesisGen == nil {
			logger.Warn("meta-evolution: no genesis generator wired, genesis proposal dropped")
			return record(false, "", "genesis bench unavailable: generator not wired")
		}
		out := runGenesisShadowBench(ctx, incumbent, proposal, genesisShadowScenarios(), t.GenesisGen)
		benchGenesis = &out
		t.pendingBenchGenesis = benchGenesis
		if rejectReason := genesisBenchDecision(out); rejectReason != "" {
			logger.Info("meta-evolution: proposal rejected by genesis shadow bench",
				"artifact", artifact, "scenarios", out.Scenarios, "flips", out.Flips, "reason", rejectReason)
			return t.recordWithBenches(record, nil, nil, nil,
				false, "", "genesis bench rejected: "+rejectReason)
		}
		logger.Info("meta-evolution: proposal cleared genesis shadow bench",
			"scenarios", out.Scenarios, "incumbentIssues", out.IncumbentIssues, "proposalIssues", out.ProposalIssues,
			"incumbentSkips", out.IncumbentSkips, "proposalSkips", out.ProposalSkips)
	}

	path, werr := t.Meta.WriteProposal(artifact, proposal)
	if werr != nil {
		logger.Warn("meta-evolution: proposal write failed", "artifact", artifact, "error", werr)
		return t.recordWithBenches(record, benchIncumbent, benchProposal, benchShadow,
			false, "", "proposal write failed: "+werr.Error())
	}
	toVersion := generation.ContentSHA256(strings.TrimSpace(proposal))[:12]
	// Low-confidence routing (ANCHOR 2606.06114 — human intervention is most
	// valuable at output verification): a proposal the bench CLEARED but could
	// not show improving (margin <= 0) is not auto-adopted; it surfaces as a
	// propose-only feed card requesting an explicit operator verdict. Scarce
	// operator attention goes exactly to the adoptions the deterministic
	// evidence cannot decide. Benchless cycles keep their documented behavior.
	lowConfidence := metaLowConfidenceReason(benchIncumbent, benchProposal, benchShadow, benchGenesis)
	if lowConfidence != "" {
		logger.Info("meta-evolution: revision routed to operator verdict (bench-cleared but low-confidence)",
			"artifact", artifact, "epoch", epoch, "margin", lowConfidence)
		if t.OnProposal != nil {
			// The verdict card must say WHY the loop is asking instead of
			// auto-adopting — the margin is the whole context for the decision.
			t.OnProposal(artifact, epoch, annotateReason(reason, "저신뢰 라우팅: "+lowConfidence), path, false)
		}
		return t.recordWithBenches(record, benchIncumbent, benchProposal, benchShadow,
			true, toVersion, annotateReason(reason, "[저신뢰: "+lowConfidence+" — 운영자 verdict 대기]"))
	}
	if metaAutoAdoptEnabled() && !driftVerdict.Frozen && !t.Tracker.AutoAdoptFrozen() {
		// Operator mandate (2026-07-11): the deterministic gate chain (contract
		// + epoch bench) IS the approver. Adopt immediately; the ledger health
		// snapshot arms the revert watch and the feed card carries a post-hoc
		// veto (되돌리기). Both the fresh drift verdict AND the persisted marker
		// must clear — the fresh one catches a freeze whose marker write failed.
		if adoptedVersion, aerr := t.Meta.AdoptProposal(artifact); aerr != nil {
			logger.Warn("meta-evolution: auto-adoption failed, falling back to propose-only",
				"artifact", artifact, "error", aerr)
		} else {
			h := t.Tracker.EvolutionHealth()
			t.pendingAdoptionHealth = &MetaAdoptionHealth{
				ConfirmRate:     h.ConfirmRate,
				FalseAcceptRate: h.FalseAcceptRate,
				Resolved:        h.ResolvedEvolves7d,
			}
			t.pendingAction = "auto_adopted"
			logger.Info("meta-evolution: revision AUTO-ADOPTED (bench-gated; revert watch armed)",
				"artifact", artifact, "epoch", epoch, "from", fromVersion, "to", adoptedVersion)
			if t.OnProposal != nil {
				t.OnProposal(artifact, epoch, reason, filepath.Join(filepath.Dir(path), artifact), true)
			}
			return t.recordWithBenches(record, benchIncumbent, benchProposal, benchShadow, true, adoptedVersion, reason)
		}
	}
	logger.Info("meta-evolution: revision proposed (propose-only — adoption is a separate decision)",
		"artifact", artifact, "epoch", epoch, "from", fromVersion, "to", toVersion, "path", path)
	if t.OnProposal != nil {
		t.OnProposal(artifact, epoch, reason, path, false)
	}
	return t.recordWithBenches(record, benchIncumbent, benchProposal, benchShadow, true, toVersion, reason)
}

// benchIncumbentOnSkip runs the epoch's bench against the incumbent alone on a
// skip cycle — opt-in via DENEB_META_BENCH_ON_SKIP=1 (the P5-2 calibration
// drop-in sets it; the knob dies with the window when the harvest removes the
// drop-in). Producer/genesis epochs bench incumbent-vs-incumbent, which
// measures the bench's own noise floor. An empty corpus yields NO sample — a
// zero-pair "bench" must not inflate the ladder's per-epoch n.
func (t *MetaEvolutionTask) benchIncumbentOnSkip(ctx context.Context, epoch, incumbent string) (*judgeBenchOutcome, *producerBenchOutcome, *genesisBenchOutcome) {
	if strings.TrimSpace(os.Getenv("DENEB_META_BENCH_ON_SKIP")) != "1" {
		return nil, nil, nil
	}
	switch epoch {
	case metaEpochEvaluator:
		verdict := t.judgeBenchExecutor()
		if verdict == nil {
			return nil, nil, nil
		}
		pairs := buildJudgeDegradationPairs(t.Evolver.catalogEntries(), judgeBenchMaxPairs*metaBenchScale())
		if len(pairs) == 0 {
			return nil, nil, nil
		}
		out := runJudgeDegradationBench(ctx, incumbent, pairs, verdict)
		return &out, nil, nil
	case metaEpochProducer:
		gen := t.producerShadowExecutor()
		if gen == nil {
			return nil, nil, nil
		}
		scenarios := buildProducerShadowScenarios(t.Evolver.catalogEntries(), t.Tracker, producerBenchMaxSkills*metaBenchScale())
		if len(scenarios) == 0 {
			return nil, nil, nil
		}
		out := runProducerShadowBench(ctx, incumbent, incumbent, scenarios, gen)
		if out.Skills == 0 {
			// Every scenario skipped or failed to parse on at least one side —
			// zero gradable pairs. Counting that toward the ladder's per-epoch n
			// manufactured invalid calibration samples (2026-07-16: 4 skills,
			// all "one-sided skip/unparsable"). No sample, no count.
			return nil, nil, nil
		}
		return nil, &out, nil
	case metaEpochGenesis:
		if t.GenesisGen == nil {
			return nil, nil, nil
		}
		scenarios := genesisShadowScenarios()
		if len(scenarios) == 0 {
			return nil, nil, nil
		}
		out := runGenesisShadowBench(ctx, incumbent, incumbent, scenarios, t.GenesisGen)
		if out.Scenarios == 0 {
			// Same invariant as the producer arm: zero both-sides-scored
			// scenarios is no sample and must not inflate the ladder's n.
			return nil, nil, nil
		}
		return nil, nil, &out
	}
	return nil, nil, nil
}

// recordWithBenches stashes the bench outcomes for the closure-based ledger
// writer. The closure owns the shared fields; bench fields ride via the task.
func (t *MetaEvolutionTask) recordWithBenches(record func(bool, string, string) error,
	inc, prop *judgeBenchOutcome, shadow *producerBenchOutcome, proposed bool, toVersion, reason string,
) error {
	t.pendingBenchIncumbent, t.pendingBenchProposal, t.pendingBenchShadow = inc, prop, shadow
	defer func() {
		t.pendingBenchIncumbent, t.pendingBenchProposal, t.pendingBenchShadow = nil, nil, nil
		t.pendingBenchGenesis = nil // set directly by the genesis-epoch branch
		t.pendingAdoptionHealth, t.pendingAction = nil, ""
		t.pendingOperatorUtility = nil
		t.pendingRevisionClass = ""
	}()
	return record(proposed, toVersion, reason)
}

// maybeRevertAdoption is the meta rollback watch: for each artifact whose
// newest adoption-lifecycle record is a recent (auto_)adoption with a health
// snapshot, compare the CURRENT 7d evolution health — a hard regression
// restores the pre-adoption backup and records the reversal. Deterministic
// and conservative; the benches remain the primary gate.
func (t *MetaEvolutionTask) maybeRevertAdoption(logger *slog.Logger) {
	prior, err := t.Tracker.RecentMetaRevisions(20)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-metaRevertWatchWindowDays * 24 * time.Hour).UnixMilli()
	seen := map[string]bool{}
	for _, p := range prior { // newest first
		if p.Action == "" || seen[p.Artifact] {
			continue
		}
		seen[p.Artifact] = true // only the newest lifecycle record per artifact counts
		if (p.Action != "auto_adopted" && p.Action != "adopted") || p.AdoptionHealth == nil || p.CreatedAt < cutoff {
			continue
		}
		h := t.Tracker.EvolutionHealth()
		if h.ResolvedEvolves7d < metaRevertMinResolved {
			continue // not enough post-adoption evidence yet
		}
		farJump := h.FalseAcceptRate - p.AdoptionHealth.FalseAcceptRate
		confirmDrop := p.AdoptionHealth.ConfirmRate - h.ConfirmRate
		if farJump < metaRevertFARJump && confirmDrop < metaRevertConfirmDrop {
			continue
		}
		reason := fmt.Sprintf("evolution health regressed since adoption: falseAcceptRate %.2f→%.2f, confirmRate %.2f→%.2f (resolved %d)",
			p.AdoptionHealth.FalseAcceptRate, h.FalseAcceptRate, p.AdoptionHealth.ConfirmRate, h.ConfirmRate, h.ResolvedEvolves7d)
		restored, rerr := t.Meta.RevertAdoption(p.Artifact)
		if rerr != nil {
			logger.Warn("meta-evolution: revert watch fired but restore failed",
				"artifact", p.Artifact, "error", rerr)
			continue
		}
		if lerr := t.Tracker.LogMetaRevision(MetaRevisionRecord{
			Artifact:    p.Artifact,
			FromVersion: p.ToVersion,
			ToVersion:   restored,
			Action:      "auto_reverted",
			Reason:      reason,
		}); lerr != nil {
			logger.Warn("meta-evolution: revert ledger write failed", "artifact", p.Artifact, "error", lerr)
		}
		logger.Warn("meta-evolution: adoption AUTO-REVERTED by the rollback watch",
			"artifact", p.Artifact, "reason", reason)
		if t.OnReverted != nil {
			t.OnReverted(p.Artifact, reason)
		}
	}
}

// stormPoisonedJudgeMinPairs is how many usable planted-defect pairs must
// clear with zero fuel misses before a miss-rate-justified judge adoption is
// treated as storm-poisoned and reverted.
const stormPoisonedJudgeMinPairs = 12

// maybeRevertStormPoisonedEvaluatorAdoption undoes a judge-prompt adoption
// that was justified by infra-error-inflated P3 miss rates. When the
// incumbent's usable probe ledger shows zero fuel misses over a minimum pair
// budget (and no real false-accept labels) and the adoption/proposal reason
// cited miss rates, restore the pre-adoption backup. Also invoked from the
// judge-accuracy lane so heal does not wait on the slow meta cadence.
func (t *MetaEvolutionTask) maybeRevertStormPoisonedEvaluatorAdoption(logger *slog.Logger) {
	if t.Meta == nil || t.Tracker == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	artifact := generation.MetaSkillJudgeSystemPrompt
	prior, err := t.Tracker.RecentMetaRevisions(30)
	if err != nil {
		return
	}
	var adopt *MetaRevisionRecord
	var proposalReason string
	for i := range prior {
		p := &prior[i]
		if p.Artifact != artifact {
			continue
		}
		if adopt == nil && (p.Action == "adopted" || p.Action == "auto_adopted") {
			adopt = p
			continue
		}
		if adopt != nil && p.Proposed && proposalReason == "" {
			proposalReason = p.Reason
			break
		}
	}
	if adopt == nil {
		return
	}
	reasonBlob := adopt.Reason + " " + proposalReason
	if !strings.Contains(reasonBlob, "놓침") && !strings.Contains(strings.ToLower(reasonBlob), "miss") {
		return // adoption was not justified by miss-rate evidence
	}
	fallback := generation.DefaultMetaArtifacts()[artifact]
	version := t.Meta.Version(artifact, fallback)
	if version == "" || (adopt.ToVersion != "" && adopt.ToVersion != version) {
		return // live artifact no longer matches the adopted revision
	}
	ev := t.collectJudgeAccuracyEvidence(version)
	pairs, ok := ev.stormPoisonHealEligible(stormPoisonedJudgeMinPairs)
	if !ok {
		return // real fuel misses or false-accept labels remain — keep the patch
	}
	restored, rerr := t.Meta.RevertAdoption(artifact)
	if rerr != nil {
		logger.Warn("meta-evolution: storm-poisoned judge revert failed",
			"artifact", artifact, "error", rerr)
		return
	}
	reason := fmt.Sprintf("reverted storm-poisoned judge adoption %s: usable probe ledger is clean (%d pairs, 0 fuel misses) — adoption cited infra-inflated miss rates",
		adopt.ToVersion, pairs)
	if lerr := t.Tracker.LogMetaRevision(MetaRevisionRecord{
		Artifact:    artifact,
		FromVersion: adopt.ToVersion,
		ToVersion:   restored,
		Action:      "auto_reverted",
		Reason:      reason,
	}); lerr != nil {
		logger.Warn("meta-evolution: storm-poisoned revert ledger write failed", "error", lerr)
	}
	logger.Warn("meta-evolution: storm-poisoned judge adoption AUTO-REVERTED",
		"artifact", artifact, "from", adopt.ToVersion, "to", restored, "pairs", pairs)
	if t.OnReverted != nil {
		t.OnReverted(artifact, reason)
	}
}

// nextEpoch rotates producer → evaluator → genesis based on the last CYCLE
// entry. A cycle record always carries a non-empty Epoch; adoption-lifecycle
// records (auto_reverted, and the operator feed-card adopted/rejected/
// operator_reverted) have an empty Epoch and must not consume a rotation. Key
// off Epoch, NOT Action: a successful auto-adoption stamps Action="auto_adopted"
// on the cycle record itself, so skipping Action!="" froze rotation on producer
// whenever cycles kept auto-adopting (the evaluator/genesis prompts then never
// got revised). An unknown/legacy epoch value falls back to producer.
func (t *MetaEvolutionTask) nextEpoch() (string, string) {
	prior, err := t.Tracker.RecentMetaRevisions(10)
	if err == nil {
		for _, p := range prior {
			if p.Epoch == "" {
				continue
			}
			switch p.Epoch {
			case metaEpochProducer:
				return metaEpochEvaluator, generation.MetaSkillJudgeSystemPrompt
			case metaEpochEvaluator:
				return metaEpochGenesis, generation.MetaGenesisSystemPrompt
			}
			break
		}
	}
	return metaEpochProducer, generation.MetaEvolveSystemPrompt
}

// assembleEvidence builds the compact evidence block the proposal prompt sees:
// the 7d health scoreboard, low-yield levers, and the meta-experience ledger.
// For the evaluator epoch it also appends the live judge's own labeled mistakes
// (P3), so a judge-prompt revision targets real blind spots (see
// assembleJudgeAccuracyEvidence).
func (t *MetaEvolutionTask) assembleEvidence(ctx context.Context, epoch string) string {
	var b strings.Builder
	h := t.Tracker.EvolutionHealth()
	fmt.Fprintf(&b, "## 7일 진화 스코어보드\n- evolve %d건 (기각 %d, 롤백 %d, 확인 %d), confirmRate %.2f, falseAcceptRate %.2f (해소 %d건)\n",
		h.Evolves7d, h.EvolveRejected7d, h.EvolveRolledBack7d, h.EvolveConfirmed7d,
		h.ConfirmRate, h.FalseAcceptRate, h.ResolvedEvolves7d)
	if h.LastRejectedReason != "" {
		fmt.Fprintf(&b, "- 최근 기각: %s — %s\n", h.LastRejectedSkill, common.TruncateRunes(h.LastRejectedReason, 200))
	}
	if levers, err := t.Tracker.lowYieldLevers(3, 2, 0.5); err == nil && len(levers) > 0 {
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
	if epoch == metaEpochProducer {
		b.WriteString(t.assembleRevisionClassEvidence())
	}
	if epoch == metaEpochEvaluator {
		b.WriteString(t.assembleJudgeAccuracyEvidence())
	}
	if epoch == metaEpochGenesis {
		b.WriteString(t.assembleGenesisEvidence())
	}
	if spots := t.Tracker.labelerBlindSpots(evolutionHealthWindow); len(spots) > 0 {
		// Blind Curator (2607.07436): confirmed-clean skills that fail their own
		// workout cases = usage-labeler false-pass suspects. ADVISORY — grounds
		// the producer on label-fidelity risk; no gate reads it.
		b.WriteString("\n## 라벨러 사각 의심 (자문 — 게이트 아님; 실사용 라벨은 성공인데 자체 held-out 케이스는 실패)\n")
		for _, s := range spots {
			fmt.Fprintf(&b, "- %s: 워크아웃 실패 케이스 %d건 (confirm은 통과)\n", s.Skill, s.FailedCases)
		}
	}
	if t.RuntimeHealth != nil {
		if line := strings.TrimSpace(t.RuntimeHealth(ctx)); line != "" {
			b.WriteString("\n## 런타임 건강 (자문 — 게이트 아님, P5-5)\n")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	if t.QualityBench != nil {
		if line := strings.TrimSpace(t.QualityBench(ctx)); line != "" {
			b.WriteString("\n## 코드베이스 건강 (자문 — 게이트 아님, P5-5)\n")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	if t.RSIBench != nil {
		if line := strings.TrimSpace(t.RSIBench(ctx)); line != "" {
			b.WriteString("\n## RSI 벤치 (자문 — 게이트 아님, P5-5)\n")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString(t.assembleOperatorUtilityEvidence())
	return b.String()
}

// assembleOperatorUtilityEvidence grounds the producer's prose on what the
// operator has actually accepted or rejected from the feed card (P5-5).
// ADVISORY ONLY: this informs the LLM's prose about operator-perceived value;
// the deterministic gates (contract + epoch benches) are unaffected. Empty on a
// fresh install (no feed-card verdicts yet) so a young loop stays quiet — the
// distinction between "no data yet" and "data exists" is the whole point.
func (t *MetaEvolutionTask) assembleOperatorUtilityEvidence() string {
	if t.Tracker == nil {
		return ""
	}
	u := t.Tracker.operatorUtilitySignals()
	if u.Adopted7d == 0 && u.Rejected7d == 0 && u.Reverted7d == 0 {
		return "" // no operator verdicts in the window — fresh install or quiet week
	}
	var b strings.Builder
	b.WriteString("\n## 운영자 피드카드 결정 (자문 — 게이트 아님, P5-5)\n")
	fmt.Fprintf(&b, "- 최근 7일: 채택 %d · 기각 %d · 되돌림 %d", u.Adopted7d, u.Rejected7d, u.Reverted7d)
	if verdicts := u.Adopted7d + u.Rejected7d; verdicts > 0 {
		fmt.Fprintf(&b, " (채택률 %.0f%%)", u.AdoptionRate*100)
	}
	b.WriteString("\n")
	b.WriteString("- 이는 운영자가 체감한 효용 자문 신호 — 개선 방향의 정성 참고. 채택률이 낮으면 제안이 운영자 기대에 못 미쳤다는 뜻이나, 게이트 통과 여부와 무관.\n")
	return b.String()
}

// assembleRevisionClassEvidence surfaces the structural/parametric balance of
// recent meta revisions to the PRODUCER epoch (L1.5-trap counter-pressure,
// Bilevel Autoresearch 2603.23420: parameter tweaks of a fixed mechanism gave
// no reliable gain; structural mechanism change carried the improvement). When
// adoptions run parametric for metaParametricStreakNudge consecutive windows,
// an explicit nudge asks the producer to weigh a structural candidate — while
// keeping the one-weakness-per-cycle discipline. ADVISORY prose only; the
// deterministic gates are untouched, so a timid-but-correct revision still
// adopts and a bold-but-regressive one is still rejected.
func (t *MetaEvolutionTask) assembleRevisionClassEvidence() string {
	if t.Tracker == nil {
		return ""
	}
	bal := t.Tracker.MetaRevisionClassBalance()
	if bal.Structural+bal.Parametric == 0 {
		return "" // pre-instrumentation history only — stay quiet
	}
	var b strings.Builder
	b.WriteString("\n## 개정 구조성 균형 (자문 — 게이트 아님)\n")
	fmt.Fprintf(&b, "- 최근 제안 분류: 구조형 %d · 파라미터형 %d", bal.Structural, bal.Parametric)
	if bal.Unclassified > 0 {
		fmt.Fprintf(&b, " · 미분류 %d", bal.Unclassified)
	}
	b.WriteString("\n")
	if bal.AdoptedParametricStreak >= metaParametricStreakNudge {
		fmt.Fprintf(&b, "- ⚠ 최근 채택 %d연속 파라미터형(수치·문구 손질). 증거가 구조적 약점을 가리킨다면 섹션/절차 수준의 메커니즘 개정 후보를 우선 검토하라 — 파라미터 손질만 반복하면 탐색 행동은 바뀌지 않는다 (Bilevel Autoresearch: 파라미터 조정은 유의미한 이득 없음, 구조 교체가 개선의 전부). 단 '한 사이클 한 약점' 규율과 출력 스키마 계약은 그대로 지켜라.\n",
			bal.AdoptedParametricStreak)
	}
	return b.String()
}

// assembleJudgeAccuracyEvidence closes the P3 loop: it surfaces the LIVE judge's
// recent labeled mistakes so an evaluator-epoch revision targets the judge's
// ACTUAL blind spots instead of revising blind. Three safety properties:
//   - No teaching-to-the-test: judgeMissExhibit carries only the miss CLASS and
//     skill, never the pair body, so the revision learns to catch a category of
//     defect — it cannot memorize the exact bench pairs. And the misses are
//     dominated by the SUBTLE classes (imperative-drop, safety-drop) while the
//     evaluator gate scores BLATANT pairs (buildJudgeDegradationPairs) — disjoint
//     corpora, so grounding cannot game the gate.
//   - Balanced pressure: misses (judge too lenient) AND suspected false-rejects
//     (judge too strict) are surfaced together with an explicit instruction to
//     tighten WITHOUT raising false rejects — the degradation bench rewards
//     rejecting defects and so cannot, alone, catch an over-strict judge.
//   - Scoped to the CURRENT judge version: older-version misses may already be
//     fixed. Returns "" when the incumbent judge has no recent misses,
//     false-rejects, or organic false-accepts (baseline-confirmed rollbacks of
//     evolves it accepted), so a clean judge leaves the evaluator epoch
//     unchanged.
func (t *MetaEvolutionTask) assembleJudgeAccuracyEvidence() string {
	if t.Tracker == nil || t.Meta == nil {
		return ""
	}
	judgeFallback := generation.DefaultMetaArtifacts()[generation.MetaSkillJudgeSystemPrompt]
	version := t.Meta.Version(generation.MetaSkillJudgeSystemPrompt, judgeFallback)
	ev := t.collectJudgeAccuracyEvidence(version)
	if ev.clean() {
		return "" // incumbent judge is clean — nothing to co-evolve on
	}
	return renderJudgeAccuracyEvidence(ev)
}

// judgeAccuracyEvidence is the incumbent-judge miss/label bundle feeding the
// evaluator-epoch revision prompt.
type judgeAccuracyEvidence struct {
	byClass           map[string][2]int // class -> [missed, total]
	byCategory        map[string][2]int // skill category -> [missed, total]
	falseRejects      int
	organic           []organicFalseAccept
	operatorConfirms  int
	operatorRollbacks int
}

func (ev judgeAccuracyEvidence) clean() bool {
	for _, ct := range ev.byClass {
		if ct[0] > 0 {
			return false
		}
	}
	return ev.falseRejects == 0 && len(ev.organic) == 0 &&
		ev.operatorConfirms == 0 && ev.operatorRollbacks == 0
}

// stormPoisonHealEligible reports whether a miss-rate-justified judge
// adoption should be undone. Unlike clean() (evaluator co-evolve fuel),
// this ignores falseRejects and operatorConfirms — those are over-strict /
// keep-soft signals, not reasons to retain a storm-poisoned tighten patch.
// Organic rollbacks and operator rollback labels still block the heal.
func (ev judgeAccuracyEvidence) stormPoisonHealEligible(minPairs int) (pairs int, ok bool) {
	for _, ct := range ev.byClass {
		if ct[0] > 0 {
			return 0, false
		}
		pairs += ct[1]
	}
	if pairs < minPairs || len(ev.organic) > 0 || ev.operatorRollbacks > 0 {
		return pairs, false
	}
	return pairs, true
}

// collectJudgeAccuracyEvidence gathers the incumbent judge's synthetic misses,
// organic false-accepts, and operator verdicts. Records from superseded judge
// versions are skipped — their mistakes may already be fixed.
func (t *MetaEvolutionTask) collectJudgeAccuracyEvidence(version string) judgeAccuracyEvidence {
	ev := judgeAccuracyEvidence{
		byClass:    map[string][2]int{},
		byCategory: map[string][2]int{},
	}
	if records, err := t.Tracker.recentJudgeAccuracy(judgeMissEvidenceRuns); err == nil {
		for _, rec := range records {
			if rec.JudgeVersion != version {
				continue // only the incumbent judge's own record is actionable
			}
			if !judgeAccuracyProbeUsable(rec) {
				continue // infra outage rows are not evaluator-epoch fuel
			}
			accumulateMissCounts(ev.byClass, rec.ByClass)
			accumulateMissCounts(ev.byCategory, rec.ByCategory)
			ev.falseRejects += len(rec.FalseRejects)
		}
	}
	// Organic labels — the REAL-usage half of the P3 food supply: baseline-
	// confirmed rollbacks whose accepting judge is the incumbent.
	for _, o := range t.Tracker.organicFalseAccepts(organicFalseAcceptWindow, judgeAccuracyMaxExhibits) {
		if o.JudgeVersion == version {
			ev.organic = append(ev.organic, o)
		}
	}
	for _, verdict := range t.Tracker.RecentOperatorJudgeVerdicts(organicFalseAcceptWindow, judgeAccuracyMaxExhibits*4) {
		if verdict.JudgeVersion != version {
			continue
		}
		if verdict.Verdict == OperatorJudgeVerdictRollback {
			ev.operatorRollbacks++
		} else if verdict.Verdict == OperatorJudgeVerdictConfirm {
			ev.operatorConfirms++
		}
	}
	return ev
}

// accumulateMissCounts folds one record's [correct, total] pairs into the
// running [missed, total] tallies.
func accumulateMissCounts(into map[string][2]int, counts map[string][2]int) {
	for key, ct := range counts {
		cur := into[key]
		cur[0] += ct[1] - ct[0] // missed = total - correct
		cur[1] += ct[1]
		into[key] = cur
	}
}

// renderJudgeAccuracyEvidence formats the collected evidence block. Wording is
// part of the meta-evolution prompt contract — change only with intent.
func renderJudgeAccuracyEvidence(ev judgeAccuracyEvidence) string {
	type classMiss struct {
		name          string
		missed, total int
	}
	var missed []classMiss
	for name, ct := range ev.byClass {
		if ct[0] > 0 {
			missed = append(missed, classMiss{name, ct[0], ct[1]})
		}
	}
	sort.Slice(missed, func(i, j int) bool {
		if missed[i].missed != missed[j].missed {
			return missed[i].missed > missed[j].missed // worst first
		}
		return missed[i].name < missed[j].name // deterministic tie-break
	})
	var b strings.Builder
	b.WriteString("\n## 판정자 최근 오판 (P3 라벨 — 실제 결함 감지력은 높이되 false-reject는 늘리지 말 것)\n")
	for _, m := range missed {
		fmt.Fprintf(&b, "- %s: 최근 %d/%d 건 놓침 (이 유형의 열화를 통과시킴)\n", m.name, m.missed, m.total)
	}
	if len(ev.organic) > 0 {
		names := make([]string, 0, len(ev.organic))
		for _, o := range ev.organic {
			names = append(names, o.Skill)
		}
		fmt.Fprintf(&b, "- 실전 false-accept %d건: %s — 판정자가 통과시킨 evolve가 실사용에서 롤백됨 (baseline-aware 확인, 최근 30일)\n",
			len(ev.organic), strings.Join(names, ", "))
	}
	if ev.operatorRollbacks > 0 {
		fmt.Fprintf(&b, "- 운영자 확인 false-accept %d건: 저신뢰 evolve를 사람이 직접 되돌림 — 판정 경계를 강화할 실제 라벨\n", ev.operatorRollbacks)
	}
	if ev.operatorConfirms > 0 {
		fmt.Fprintf(&b, "- 운영자 확인 true-accept %d건: 저신뢰 evolve를 사람이 유효하다고 확정 — 과잉 엄격화하지 말 것\n", ev.operatorConfirms)
	}
	// Category-local bias (evaluator preference collapse, 2606.16682): a
	// category whose misses concentrate must be named so the revision fixes
	// the category blind spot, not just the aggregate.
	var skewed []string
	for cat, ct := range ev.byCategory {
		if ct[0] > 0 {
			skewed = append(skewed, fmt.Sprintf("%s %d/%d", cat, ct[0], ct[1]))
		}
	}
	if len(skewed) > 0 {
		sort.Strings(skewed)
		fmt.Fprintf(&b, "- 카테고리별 놓침 분포: %s (한 카테고리 편중 = 국소 편향 신호)\n", strings.Join(skewed, " · "))
	}
	if ev.falseRejects > 0 {
		fmt.Fprintf(&b, "- 의심 false-reject: %d건 (기각했으나 실제로는 현재 본문보다 나았던 후보 — 과잉 엄격화 경계)\n", ev.falseRejects)
	}
	b.WriteString("위 유형의 실제 결함 감지력을 높이되, 정상 개선을 기각하지 않도록 판정 기준을 정밀화하라 (과잉 기각은 진화를 정지시킨다).\n")
	// Research-grounded direction hint (BINEVAL 2606.27226, soft path — RSI
	// 2026H2 addendum second pass #2): decomposing the judging RUBRIC into
	// atomic yes/no checks calibrates better than scalar scoring and makes
	// per-miss-class fixes targetable. ADVISORY prose only; the response JSON
	// schema is protected by the contract gate regardless.
	b.WriteString("개정 방향 후보 (자문): 판정 기준을 원자적 예/아니오 점검 항목 목록으로 분해해 서술하는 방식을 고려하라 — 스칼라 채점보다 캘리브레이션이 좋고 놓친 유형별 교정이 쉽다. 단 출력 JSON 스키마는 절대 불변.\n")
	return b.String()
}

// assembleGenesisEvidence grounds a genesis-epoch revision on what the
// genesis lane actually produced: 30d creation volume against catalog size.
// Compact on purpose — the fixtures and the admissibility gate carry the
// fitness signal; this block only tells the producer whether the lane is
// starving (few creations → extraction criteria may be too strict) or
// flooding (many → dedup/skip rules may be too loose).
func (t *MetaEvolutionTask) assembleGenesisEvidence() string {
	entries, err := t.Tracker.RecentLifecycleLog(400)
	if err != nil {
		return ""
	}
	cutoff := time.Now().Add(-30 * 24 * time.Hour).UnixMilli()
	created := 0
	for _, e := range entries {
		if e.Type == "genesis" && e.CreatedAt >= cutoff {
			created++
		}
	}
	catalog := len(t.Evolver.catalogEntries())
	var b strings.Builder
	b.WriteString("\n## 제네시스 레인 (30일)\n")
	fmt.Fprintf(&b, "- 신규 스킬 %d건 생성 · 현재 카탈로그 %d개\n", created, catalog)
	b.WriteString("- 거부 기준(skip 규칙)과 Hermes 선택 순서는 레인의 정직성 계약이다 — 완화가 아니라 정밀화 방향으로만 제안하라.\n")
	return b.String()
}

// metaLowConfidenceReason reports why a bench-cleared proposal is still not
// confident enough to auto-adopt (margin <= 0 on the epoch bench that ran),
// or "" when the evidence shows a measurable improvement. Pure — the
// deterministic half of the low-confidence routing decision.
func metaLowConfidenceReason(inc, prop *judgeBenchOutcome, shadow *producerBenchOutcome, gen *genesisBenchOutcome) string {
	if inc != nil && prop != nil && prop.rate() <= inc.rate() {
		return fmt.Sprintf("judge bench margin %.2f→%.2f (no measurable improvement)", inc.rate(), prop.rate())
	}
	if shadow != nil && shadow.Skills == 0 {
		// Nothing was scored: an unbenched proposal must not read as a measured
		// flat margin ("0.00→0.00").
		return "shadow bench scored no scenario (skips or unparsable outputs on both sides)"
	}
	if shadow != nil && shadow.ProposalScore <= shadow.IncumbentScore {
		return fmt.Sprintf("shadow bench margin %.2f→%.2f (no measurable improvement)", shadow.IncumbentScore, shadow.ProposalScore)
	}
	if gen != nil {
		if gen.Scenarios == 0 {
			return "genesis bench scored no scenario (skips or unparsable outputs on both sides)"
		}
		// Lower mean gate issues = better; equal (typically 0→0 on clean
		// fixtures) is cleared-but-unproven — exactly the operator-verdict case.
		if gen.ProposalIssues >= gen.IncumbentIssues {
			return fmt.Sprintf("genesis bench margin %.2f→%.2f issues (no measurable improvement)", gen.IncumbentIssues, gen.ProposalIssues)
		}
	}
	return ""
}

// annotateReason appends an annotation to a producer reason with a clean
// delimiter — an empty reason must not produce a leading " — " in verdict
// cards or the ledger.
func annotateReason(reason, note string) string {
	if strings.TrimSpace(reason) == "" {
		return note
	}
	return reason + " — " + note
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
