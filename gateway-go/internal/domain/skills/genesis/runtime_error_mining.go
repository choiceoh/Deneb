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
)

const (
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
		if isExternalFault(ln) {
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

// Interval honors DENEB_RUNTIME_ERROR_MINING_INTERVAL_HOURS.
func (t *RuntimeErrorMiningTask) Interval() time.Duration {
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
	if err := saveRuntimeErrorState(path, state); err != nil {
		logger.Warn("runtime-error-mining: state save failed (window will re-accumulate)", "error", err)
	}
	if folded > 0 {
		logger.Debug("runtime-error-mining: folded ring lines into rolling window",
			"folded", folded, "signatures", len(state.Sigs))
	}

	// Deterministic order: most-recurring first, signature as tie-break.
	ranked := make([]*runtimeErrorAgg, 0, len(state.Sigs))
	for sig, e := range state.Sigs {
		if e.Count >= e.recurrenceFloor() {
			ranked = append(ranked, &runtimeErrorAgg{
				sig: sig, count: e.Count, lastAt: e.LastAt,
				exampleMsg: e.ExampleMsg, exampleErr: e.ExampleErr,
			})
		}
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
		evidence := buildRuntimeErrorEvidence(a)
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
	return nil
}

// buildRuntimeErrorEvidence renders the grounded evidence block: recurrence
// count, the example message, and the most useful attrs (error, runId).
func buildRuntimeErrorEvidence(a *runtimeErrorAgg) string {
	var b strings.Builder
	b.WriteString(strconv.Itoa(a.count))
	b.WriteString("× in the rolling 7d window (restart-surviving). example: ")
	b.WriteString(common.TruncateRunes(a.exampleMsg, 300))
	if e := strings.TrimSpace(a.exampleErr); e != "" {
		b.WriteString("\nerror=")
		b.WriteString(common.TruncateRunes(e, 300))
	}
	return b.String()
}
