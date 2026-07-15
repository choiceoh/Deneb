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
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/genbind"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

const (
	runtimeErrorMiningDefaultInterval = 12 * time.Hour
	// runtimeErrorMinRecurrence: a signature below this is a one-off, not a
	// grounded, code-actionable defect.
	runtimeErrorMinRecurrence = 5
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
type RuntimeErrorMiningTask struct {
	ErrorLines func(limit int) []observe.LogLine
	Tracker    *Tracker
	Logger     *slog.Logger
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
	sig     string
	count   int
	lastAt  int64
	example observe.LogLine
}

// Run mines one snapshot of the error ring for recurring, code-actionable
// signatures and records the missing propose-only candidates.
func (t *RuntimeErrorMiningTask) Run(ctx context.Context) error {
	if t.ErrorLines == nil || t.Tracker == nil {
		return nil
	}
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}

	lines := t.ErrorLines(runtimeErrorRingScan)
	aggs := map[string]*runtimeErrorAgg{}
	for _, ln := range lines {
		if isExternalFault(ln) {
			continue
		}
		sig := normalizeErrorSignature(ln.Msg)
		if sig == "" {
			continue
		}
		a := aggs[sig]
		if a == nil {
			a = &runtimeErrorAgg{sig: sig, example: ln}
			aggs[sig] = a
		}
		a.count++
		if ln.Ts > a.lastAt {
			a.lastAt = ln.Ts
			a.example = ln
		}
	}

	// Deterministic order: most-recurring first, signature as tie-break.
	ranked := make([]*runtimeErrorAgg, 0, len(aggs))
	for _, a := range aggs {
		if a.count >= runtimeErrorMinRecurrence {
			ranked = append(ranked, a)
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
	now := time.Now()
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
			Title:     "recurring runtime error: " + genbind.TruncateRunes(a.sig, 80),
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
				"sig", genbind.TruncateRunes(a.sig, 60), "error", rerr)
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
	b.WriteString("× in the recent error ring. example: ")
	b.WriteString(genbind.TruncateRunes(a.example.Msg, 300))
	if e := strings.TrimSpace(a.example.Attrs["error"]); e != "" {
		b.WriteString("\nerror=")
		b.WriteString(genbind.TruncateRunes(e, 300))
	}
	if rid := strings.TrimSpace(a.example.RunID); rid != "" {
		b.WriteString("\nrunId=")
		b.WriteString(rid)
	}
	return b.String()
}
