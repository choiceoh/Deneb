package genesis

// RuntimeErrorMiningTask — a second, always-available grounded source of
// scope=code self-correction candidates (RSI L4 fuel).
//
// evolver_tool_gap.go only emits a code candidate when a SKILL evolve declares a
// grounded tool gap — genuine but rare, so the L4 coding lane sat starved. This
// lane reads the gateway's own recent error log (the in-process observe ring)
// and, for an error SIGNATURE that both recurs at/above a threshold AND is a
// code-fixable class (not an external/transient fault), records ONE propose-only
// scope=code candidate.
//
// Safety (SkillSmith/tool-gap spirit — this lane PROPOSES, it never edits):
//   - Grounding: a one-off never promotes; only a signature recurring >= a
//     threshold within the observed error window.
//   - Class filter: external/transient faults (429, EOF, connection refused,
//     context canceled, deadline exceeded, "unavailable", TLS, i/o timeout) are
//     EXCLUDED — a 429 is not a bug to fix in our source (mirrors runtime-
//     health's fault-vs-latency accounting).
//   - Forbidden surface: RecordSelfCorrectionCandidate rejects any TargetFiles
//     pointing at acceptance-machinery or a security path at record time, and
//     the coding-dispatch inherits the CLAUDE.md rules-gate on top.
//   - Dedup + reopen: one open candidate per SIGNATURE; an APPLIED fix whose
//     signature recurs after the cooldown re-opens ("the fix did not stick").
//   - Propose-only + auto-dispatch: source namespace is on rsiDispatchSources
//     (graduated 2026-07-15 — first-batch human review dropped). Miners still
//     only PROPOSE; coding-dispatch + deploy-watch remain the landing gates.
//   - Production-gated at registration (reads the live ring, writes shared state).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"
	rsilifecycle "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/lifecycle"
)

const (
	// runtimeErrorFoldDefaultInterval is the task cadence: how often the
	// in-memory observe ring is folded into the persisted rolling window.
	// This must be much shorter than the mining cadence because the ring is
	// wiped by every hot-swap: with 12h folds, a night-long error burst
	// followed by a morning deploy evaporated entirely (live 2026-07-19: a
	// ~120-line embed-failure burst — 10× the warn floor — left zero trace
	// in the rolling state). Hourly folds bound the loss to ≤1h per swap.
	runtimeErrorFoldDefaultInterval = 1 * time.Hour
	// runtimeErrorMiningDefaultInterval throttles candidate AUTHORING — the
	// expensive/queue-visible half stays at the original cadence while folds
	// run hourly underneath.
	runtimeErrorMiningDefaultInterval = 12 * time.Hour
	// runtimeErrorMinRecurrence: a signature below this is a one-off, not a
	// grounded, code-actionable defect.
	runtimeErrorMinRecurrence = 5
	// runtimeErrorWarnMinRecurrence is the stricter bar for WARN-level
	// signatures. Warns are admitted at all because graceful degradation
	// DOWNGRADES real defects: a fallback-rescued model failure logs Warn
	// ("model failed, trying fallback"), so the better the fallbacks, the less
	// an Error-only miner sees (live 2026-07-18: a week of recurring kimi 400s
	// produced zero candidates while an operator fixed them by hand). Warns
	// are also noisier, hence the higher floor.
	runtimeErrorWarnMinRecurrence = 12
	// runtimeErrorStateTTL bounds the rolling signature window. Old
	// occurrences age out so a long-fixed defect cannot keep a signature
	// above threshold forever.
	runtimeErrorStateTTL = 7 * 24 * time.Hour
	// runtimeErrorMaxCandidatesPerRun bounds queue growth per run.
	runtimeErrorMaxCandidatesPerRun = 2
	// runtimeErrorFreshnessWindow keeps self-resolved bursts out of dispatch:
	// a signature whose LAST occurrence is older than this is history, not a
	// live defect. Live 2026-07-20: a wormhole/kimi outage burst from the
	// 07-18/19 window — already fixed operator-side — burned 2 of the day's 4
	// dispatch slots on candidates codex could only decline. The 7d TTL still
	// keeps such signatures as evidence; they just cannot author dispatches
	// until they fire again.
	runtimeErrorFreshnessWindow = 24 * time.Hour
	// runtimeErrorImpactQuietTargetHours defines "fixed" for the impact
	// contract issued with each candidate: the signature staying quiet this
	// long after the fix survived the rollback watch verifies usefulness.
	runtimeErrorImpactQuietTargetHours = 48
	// runtimeErrorRingScan is how many recent error lines to pull from the ring.
	runtimeErrorRingScan = 3000
	// runtimeErrorSourcePrefix is the candidate Source namespace. On the
	// compiled dispatch allowlist as of 2026-07-15 (first-batch human
	// review dropped); miners still only propose — landing stays behind
	// coding-dispatch + deploy-watch.
	runtimeErrorSourcePrefix = "runtime-error"
	// runtimeErrorMiningSkill labels these non-skill, runtime-level candidates.
	runtimeErrorMiningSkill = "gateway-runtime"
)

// runtimeErrorImpactObservationWindow delays impact measurement until the
// quiet-hours metric can actually reach its target. Var, not const: tests
// shrink it to measure immediately.
var runtimeErrorImpactObservationWindow = runtimeErrorImpactQuietTargetHours * time.Hour

// externalFaultPattern matches transient/external faults that are NOT source
// defects — they must never become code candidates.
var externalFaultPattern = regexp.MustCompile(`(?i)\b(429|rate.?limit|EOF|connection refused|connection reset|broken pipe|context canceled|context deadline|deadline exceeded|timed? ?out|timeout|unavailable|no such host|i/o timeout|TLS|temporarily|throttl)\b`)

// Signature normalization: collapse the variable parts of an error message so
// the same defect firing with different ids/numbers/paths folds into one key.
var (
	reQuoted = regexp.MustCompile(`"[^"]*"|'[^']*'`)
	reHex    = regexp.MustCompile(`\b(?:0x)?[0-9a-fA-F]{8,}\b`)
	reNum    = regexp.MustCompile(`\b\d+\b`)
	reWS     = regexp.MustCompile(`\s+`)
)

func normalizeErrorSignature(msg string) string {
	s := reQuoted.ReplaceAllString(msg, "…")
	s = reHex.ReplaceAllString(s, "…")
	s = reNum.ReplaceAllString(s, "N")
	s = reWS.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// observeOnlyPattern matches an EXPLICIT authorial marker that a line reports
// an observation rather than a fault. Such a line has no fix — "silencing" it
// would mean deleting the observation the author wanted — so a candidate built
// from one can only be declined or land with no effect, burning a scarce
// dispatch slot either way. Live 2026-07-25: `regression-watch: regression
// detected (observe-only)` did exactly that, landing with a no_effect impact
// verdict (23.9h quiet vs the 48h target) because there was nothing to fix.
//
// Deliberately narrow: it respects a marker the AUTHOR wrote, and does not try
// to infer "this mechanism is working as designed". That inference would
// collide head-on with why WARN lines are admitted at all — graceful
// degradation DOWNGRADES real defects, so "model failed, trying fallback" is a
// working fallback AND a real upstream defect signal (see
// runtimeErrorWarnMinRecurrence). Only the explicit marker is safe to trust.
var observeOnlyPattern = regexp.MustCompile(`(?i)\(observe.?only\)|\bobserve.?only\b|\badvisory\b|\bdry.?run\b|\bwill recover\b|\bno-?op\b`)

// isObserveOnlySignal reports an intentionally advisory line — see
// observeOnlyPattern. Checked on the message only: `error` attrs carry the
// underlying failure text, where these words would be coincidental.
func isObserveOnlySignal(line observe.LogLine) bool {
	return observeOnlyPattern.MatchString(line.Msg)
}

func isExternalFault(line observe.LogLine) bool {
	if externalFaultPattern.MatchString(line.Msg) {
		return true
	}
	return externalFaultPattern.MatchString(line.Attrs["error"])
}

func runtimeErrorSignatureHash(sig string) string {
	sum := sha256.Sum256([]byte(sig))
	return hex.EncodeToString(sum[:])[:12]
}

// RuntimeErrorMiningTask is the standing lane. ErrorLines is injected (a thin
// closure over the observe ring) so scoring is testable without a live ring.
//
// The lane aggregates into a PERSISTED rolling window (StatePath), not a
// single ring snapshot: the observe ring is in-memory and every hot-swap
// wipes it, so on active development days — exactly when defects are most
// plentiful — a snapshot miner scans a near-empty ring every run and nothing
// ever reaches the recurrence floor (live 2026-07-18: 7+ restarts in a
// morning, zero candidates all week). Each run folds only lines newer than
// the stored watermark, so a line is counted once across restarts.
type RuntimeErrorMiningTask struct {
	ErrorLines func(limit int) []observe.LogLine
	Tracker    *Tracker
	Logger     *slog.Logger
	// StatePath overrides the rolling-state location (tests). Empty resolves
	// to ~/.deneb/data/runtime_error_signature_state.json — the same
	// homeDir-anchored convention as the Tracker ledgers.
	StatePath string
	// MiningInterval overrides the candidate-authoring throttle (tests).
	// Zero resolves via env/default.
	MiningInterval time.Duration
}

// runtimeErrorSigEntry is one signature's rolling aggregate.
type runtimeErrorSigEntry struct {
	Count      int    `json:"count"`
	FirstAt    int64  `json:"firstAtMs"`
	LastAt     int64  `json:"lastAtMs"`
	ExampleMsg string `json:"exampleMsg"`
	ExampleErr string `json:"exampleErr,omitempty"`
	// SeenError records whether the signature ever fired at ERROR level —
	// error signatures use the lower recurrence floor, warn-only ones the
	// stricter floor.
	SeenError bool `json:"seenError,omitempty"`
}

// runtimeErrorState is the persisted rolling aggregation.
type runtimeErrorState struct {
	// WatermarkMs is the newest line timestamp already folded — the
	// cross-restart dedup cursor.
	WatermarkMs int64                            `json:"watermarkMs"`
	Sigs        map[string]*runtimeErrorSigEntry `json:"sigs"`
	// LastMinedAtMs throttles candidate authoring to the mining cadence
	// while the task itself runs at the (much faster) fold cadence.
	LastMinedAtMs int64 `json:"lastMinedAtMs,omitempty"`
}

func (t *RuntimeErrorMiningTask) statePath() string {
	if strings.TrimSpace(t.StatePath) != "" {
		return t.StatePath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".deneb", "data", "runtime_error_signature_state.json")
}

// loadRuntimeErrorState is fail-open: a missing or corrupt file starts fresh —
// losing the window costs one accumulation cycle, never the lane.
func loadRuntimeErrorState(path string) *runtimeErrorState {
	st := &runtimeErrorState{Sigs: map[string]*runtimeErrorSigEntry{}}
	if path == "" {
		return st
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	var loaded runtimeErrorState
	if json.Unmarshal(raw, &loaded) != nil || loaded.Sigs == nil {
		return st
	}
	return &loaded
}

func saveRuntimeErrorState(path string, st *runtimeErrorState) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// fold merges ring lines newer than the watermark into the rolling state and
// prunes aged-out signatures. Returns the number of lines folded.
func (st *runtimeErrorState) fold(lines []observe.LogLine, now time.Time) int {
	folded := 0
	maxTs := st.WatermarkMs
	for _, ln := range lines {
		if ln.Ts <= st.WatermarkMs {
			continue
		}
		if ln.Ts > maxTs {
			maxTs = ln.Ts
		}
		if isExternalFault(ln) || isObserveOnlySignal(ln) {
			continue
		}
		sig := normalizeErrorSignature(ln.Msg)
		if sig == "" {
			continue
		}
		e := st.Sigs[sig]
		if e == nil {
			e = &runtimeErrorSigEntry{FirstAt: ln.Ts}
			st.Sigs[sig] = e
		}
		e.Count++
		folded++
		if ln.Ts >= e.LastAt {
			e.LastAt = ln.Ts
			e.ExampleMsg = ln.Msg
			e.ExampleErr = ln.Attrs["error"]
		}
		if strings.EqualFold(ln.Level, "ERROR") {
			e.SeenError = true
		}
	}
	st.WatermarkMs = maxTs
	cutoff := now.Add(-runtimeErrorStateTTL).UnixMilli()
	for sig, e := range st.Sigs {
		if e.LastAt < cutoff {
			delete(st.Sigs, sig)
		}
	}
	return folded
}

// recurrenceFloor is per-signature: ERROR-touched signatures promote at the
// base floor; warn-only ones need the stricter floor.
func (e *runtimeErrorSigEntry) recurrenceFloor() int {
	if e.SeenError {
		return runtimeErrorMinRecurrence
	}
	return runtimeErrorWarnMinRecurrence
}

// Name identifies the task in the autonomous scheduler.
func (t *RuntimeErrorMiningTask) Name() string { return "runtime-error-mining" }

// Interval is the FOLD cadence (ring → persisted window); candidate authoring
// is throttled separately by miningInterval. Honors
// DENEB_RUNTIME_ERROR_FOLD_INTERVAL_HOURS.
func (t *RuntimeErrorMiningTask) Interval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("DENEB_RUNTIME_ERROR_FOLD_INTERVAL_HOURS")); v != "" {
		if hours, err := strconv.Atoi(v); err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return runtimeErrorFoldDefaultInterval
}

// miningInterval throttles candidate authoring. Honors
// DENEB_RUNTIME_ERROR_MINING_INTERVAL_HOURS (its pre-split meaning).
func (t *RuntimeErrorMiningTask) miningInterval() time.Duration {
	if t.MiningInterval > 0 {
		return t.MiningInterval
	}
	if v := strings.TrimSpace(os.Getenv("DENEB_RUNTIME_ERROR_MINING_INTERVAL_HOURS")); v != "" {
		if hours, err := strconv.Atoi(v); err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return runtimeErrorMiningDefaultInterval
}

type runtimeErrorAgg struct {
	sig        string
	count      int
	lastAt     int64
	exampleMsg string
	exampleErr string
}

// Run folds the current ring into the persisted rolling window and records
// propose-only candidates for signatures over their recurrence floor.
func (t *RuntimeErrorMiningTask) Run(ctx context.Context) error {
	if t.ErrorLines == nil || t.Tracker == nil {
		return nil
	}
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}

	now := time.Now()
	path := t.statePath()
	state := loadRuntimeErrorState(path)
	folded := state.fold(t.ErrorLines(runtimeErrorRingScan), now)
	if folded > 0 {
		logger.Debug("runtime-error-mining: folded ring lines into rolling window",
			"folded", folded, "signatures", len(state.Sigs))
	}

	// Authoring runs at the slower mining cadence; every other run is a pure
	// fold that just keeps the rolling window fed between hot-swaps.
	mine := now.UnixMilli()-state.LastMinedAtMs >= t.miningInterval().Milliseconds()
	if mine {
		state.LastMinedAtMs = now.UnixMilli()
	}
	if err := saveRuntimeErrorState(path, state); err != nil {
		logger.Warn("runtime-error-mining: state save failed (window will re-accumulate)", "error", err)
	}
	if !mine {
		return nil
	}

	// Deterministic order: most-recurring first, signature as tie-break.
	// Freshness gate: recurrence alone is not enough — the signature must
	// still be firing, or the "defect" is a self-resolved burst that would
	// only burn a dispatch slot on an honest decline.
	freshCutoff := now.Add(-runtimeErrorFreshnessWindow).UnixMilli()
	staleHeld := 0
	ranked := make([]*runtimeErrorAgg, 0, len(state.Sigs))
	for sig, e := range state.Sigs {
		if e.Count < e.recurrenceFloor() {
			continue
		}
		if e.LastAt < freshCutoff {
			staleHeld++
			continue
		}
		ranked = append(ranked, &runtimeErrorAgg{
			sig: sig, count: e.Count, lastAt: e.LastAt,
			exampleMsg: e.ExampleMsg, exampleErr: e.ExampleErr,
		})
	}
	if staleHeld > 0 {
		logger.Debug("runtime-error-mining: recurring-but-quiet signatures held out of authoring",
			"held", staleHeld, "freshnessWindow", runtimeErrorFreshnessWindow)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return ranked[i].sig < ranked[j].sig
	})

	existing, err := t.Tracker.RecentSelfCorrectionCandidates("", "", 300)
	if err != nil {
		logger.Warn("runtime-error-mining: candidate scan failed", "error", err)
		return nil
	}

	authored := 0
	for _, a := range ranked {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if authored >= runtimeErrorMaxCandidatesPerRun {
			break
		}
		source := runtimeErrorSourcePrefix + ":" + runtimeErrorSignatureHash(a.sig)
		if selfCorrectionReopenBlocked(existing, source, a.lastAt, now) {
			continue
		}
		evidence := buildRuntimeErrorEvidence(a, now)
		if _, rerr := t.Tracker.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
			Scope:     "code",
			SkillName: runtimeErrorMiningSkill,
			Title:     "recurring runtime error: " + common.TruncateRunes(a.sig, 80),
			Candidate: a.sig,
			Evidence:  evidence,
			Reason:    "recurring gateway error signature (grounded, non-external)",
			ProposedChange: "Locate the source path emitting this error and fix the root cause. " +
				"If the condition is expected/external rather than a defect, downgrade the log level instead of leaving it at error.",
			Risk: "Diagnose from the evidence before editing. If the root cause is not clearly in our source " +
				"(external dependency, user input, transient), land nothing and record why.",
			Source: source,
			// The usefulness contract this miner can measure itself: the
			// signature staying quiet after the fix. measurePendingImpacts
			// closes the loop from the same rolling window.
			ImpactContract: &rsilifecycle.ImpactContract{
				Metric:              "quiet-hours since last occurrence of runtime-error signature " + runtimeErrorSignatureHash(a.sig),
				Direction:           selfCorrectionImpactDirectionIncrease,
				Baseline:            0,
				Target:              runtimeErrorImpactQuietTargetHours,
				MinSamples:          1,
				ObservationWindowMs: runtimeErrorImpactObservationWindow.Milliseconds(),
			},
		}); rerr != nil {
			// Forbidden-surface rejections are expected and healthy (errors born
			// in acceptance-machinery/security code must not become auto-fix
			// candidates) — debug, not warn.
			logger.Debug("runtime-error-mining: candidate rejected",
				"sig", common.TruncateRunes(a.sig, 60), "error", rerr)
			continue
		}
		authored++
	}
	if authored > 0 {
		logger.Info("runtime-error-mining: authored code candidates for recurring errors (propose-only)",
			"count", authored, "distinctSignatures", len(ranked))
	}
	t.measurePendingImpacts(existing, state, now, logger)
	return nil
}

// measurePendingImpacts closes the usefulness loop on this miner's landed
// candidates: a watch_passed dispatch with an unmeasured impact contract is
// judged against the live rolling window. The metric is quiet-hours since the
// signature last fired — a fixed defect verifies deterministically, a
// persisting one records no_effect (before this, 6 of 7 landed L4 candidates
// had no effect measurement at all: 확인 0 · 대기 1 on 2026-07-20). Errors are
// expected while the observation window is still open; those stay at Debug
// and retry on the next mining run.
func (t *RuntimeErrorMiningTask) measurePendingImpacts(existing []SelfCorrectionCandidateRecord, state *runtimeErrorState, now time.Time, logger *slog.Logger) {
	for i := range existing {
		cand := existing[i]
		if !strings.HasPrefix(cand.Source, runtimeErrorSourcePrefix+":") {
			continue
		}
		if cand.DispatchPhase != selfCorrectionDispatchWatchPassed || cand.ImpactContract == nil {
			continue
		}
		// An inconclusive verdict is a record that we looked, not a terminal
		// answer — re-measure it when the stream is alive again. Any other
		// recorded result is final.
		if cand.ImpactResult != nil && cand.ImpactResult.Status != selfCorrectionImpactInconclusive {
			continue
		}
		// A signature absent from the rolling state aged out entirely — quiet
		// for at least the window TTL.
		quietHours := runtimeErrorStateTTL.Hours()
		if e, ok := state.Sigs[cand.Candidate]; ok {
			quietHours = now.Sub(time.UnixMilli(e.LastAt)).Hours()
		}
		if quietHours < 0 {
			quietHours = 0
		}
		// Attribution control. Quiet is evidence the fix worked only if the error
		// stream was ALIVE while THIS signature stayed silent. When every other
		// tracked signature went quiet too, the gateway — or whatever exercises
		// the path — simply went idle, and crediting the fix would be measuring
		// our own downtime. Ledger audit 2026-08-01: all 8 "verified" runtime
		// impacts were bare hours-since-last-fire thresholds (52–68h) with no
		// such control, which is why the loop could not tell a repair from a
		// quiet weekend.
		//
		// No control available means no verdict yet, NOT no effect: leave the
		// candidate pending so a later run with a live stream can judge it. A
		// permanently idle signature staying pending is the honest reading.
		quietSince := now.Add(-time.Duration(quietHours * float64(time.Hour)))
		liveSiblings := activeSiblingSignatures(state, cand.Candidate, quietSince)
		samples := 1
		note := fmt.Sprintf("runtime-error miner: quiet-hours from the rolling signature window; "+
			"%d sibling signature(s) fired in the same window (stream live, quiet is attributable)",
			liveSiblings)
		if liveSiblings == 0 {
			// Samples 0 drives the inconclusive verdict: the measurement ran and
			// carried no evidence. Recording it beats skipping — the ledger now
			// shows WHY this candidate has no usefulness answer, and the guard
			// above re-measures it once the stream is live.
			samples = 0
			note = "runtime-error miner: whole signature stream quiet in the same window — " +
				"quiet is not attributable to the fix (no control), re-measured when the stream is live"
		}
		rec, err := t.Tracker.RecordSelfCorrectionDispatch(SelfCorrectionCandidateRecord{
			ID:        cand.ID,
			AttemptID: cand.AttemptID,
			ImpactResult: &rsilifecycle.ImpactResult{
				Observed: quietHours,
				Samples:  samples,
				Note:     note,
			},
		})
		if err != nil {
			logger.Debug("runtime-error-mining: impact measurement deferred",
				"id", cand.ID, "error", err)
			continue
		}
		status := ""
		if rec.ImpactResult != nil {
			status = rec.ImpactResult.Status
		}
		logger.Info("runtime-error-mining: impact verdict recorded",
			"id", cand.ID, "status", status, "quietHours", int(quietHours))
	}
}

// buildRuntimeErrorEvidence renders the grounded evidence block: recurrence
// count, recency, the example message, and the most useful attrs (error, runId).
func buildRuntimeErrorEvidence(a *runtimeErrorAgg, now time.Time) string {
	var b strings.Builder
	b.WriteString(strconv.Itoa(a.count))
	b.WriteString("× in the rolling 7d window (restart-surviving); last seen ")
	b.WriteString(now.Sub(time.UnixMilli(a.lastAt)).Round(time.Minute).String())
	b.WriteString(" ago. example: ")
	b.WriteString(common.TruncateRunes(a.exampleMsg, 300))
	if e := strings.TrimSpace(a.exampleErr); e != "" {
		b.WriteString("\nerror=")
		b.WriteString(common.TruncateRunes(e, 300))
	}
	return b.String()
}

// activeSiblingSignatures counts tracked signatures OTHER than selfSig that
// fired after `since` — the control group for an impact measurement. They come
// from the same rolling state the miner already maintains, so this costs
// nothing extra to collect and cannot drift away from what produced the
// finding.
func activeSiblingSignatures(state *runtimeErrorState, selfSig string, since time.Time) int {
	if state == nil {
		return 0
	}
	live := 0
	for sig, entry := range state.Sigs {
		if sig == selfSig || entry == nil {
			continue
		}
		if time.UnixMilli(entry.LastAt).After(since) {
			live++
		}
	}
	return live
}
