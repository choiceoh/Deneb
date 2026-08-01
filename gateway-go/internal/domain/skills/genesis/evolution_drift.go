package genesis

// Evolution-trajectory self-audit — the meta-monitor / self-brake.
//
// Auto-adoption (#3459) and the L4 coding loop run without a human in the
// accept path. The remaining safety gap (the external assessment's sharpest
// safety point): the ledgers accumulate but nothing READS them to ask "is my
// improvement process drifting toward reward-hacking — is the judge quietly
// going soft, are adoptions collapsing onto one narrow pattern, are accepted
// evolves regressing?" This audit reads the existing ledgers deterministically
// and, when a composite drift signal crosses threshold, FREEZES auto-adoption
// (falls back to propose-only, human decides) and ledgers the reason.
//
// Deterministic Go over data we already collect; no LLM. The brake is a
// persisted marker so it survives restarts and is auditable — a loop that has
// started to game its own metrics stops promoting itself automatically until
// an operator (or a recovered trajectory) clears it.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

const (
	// driftMinResolved gates every rate signal: below this the sample is too
	// thin to call drift (a 1/1 is not a trend).
	driftMinResolved = 4
	// driftFalseAcceptCeil — judge accepting this fraction of bad evolves
	// (rolled back / resolved) is a soft judge.
	driftFalseAcceptCeil = 0.50
	// driftMetaRevertCeil — this fraction of recent meta-adoptions getting
	// reverted means the slow loop is adopting regressions.
	driftMetaRevertCeil = 0.50
	// driftJudgeAccuracyFloor — a judge-accuracy run whose MUST-CATCH accuracy
	// (blatantJudgeDegradations classes only) falls below this means the
	// verifier itself is broken/soft. Scoped to the blatant classes on
	// purpose: the lane's probe curriculum ladder mixes harder tiers into the
	// same run, and their misses are P3 fuel, not breakage.
	driftJudgeAccuracyFloor = 0.50
	// driftMonotonyStreak — this many consecutive meta-adoptions targeting the
	// SAME artifact (no revert between) is a loop optimizing one narrow lever:
	// diversity collapse, the classic reward-hacking tell.
	driftMonotonyStreak = 3
)

// driftSignal is one tripped drift condition.
type driftSignal struct {
	Kind   string  `json:"kind"`
	Detail string  `json:"detail"`
	Value  float64 `json:"value,omitempty"`
}

// driftVerdict is the audit outcome. Frozen true means auto-adoption should
// fall back to propose-only until the trajectory recovers or an operator
// clears the marker.
type driftVerdict struct {
	CreatedAt int64         `json:"createdAt"`
	Frozen    bool          `json:"frozen"`
	Signals   []driftSignal `json:"signals,omitempty"`
	// Snapshot of the inputs, for the audit trail.
	FalseAcceptRate float64 `json:"falseAcceptRate"`
	ConfirmRate     float64 `json:"confirmRate"`
	Resolved        int     `json:"resolved"`
}

// autoAdoptFreezePath is the self-brake marker: present ⇒ auto-adopt frozen.
func (t *Tracker) autoAdoptFreezePath() string {
	return filepath.Join(filepath.Dir(t.logPath), "auto_adopt_freeze.json")
}

// AutoAdoptFrozen reports whether the self-brake is engaged. Fail CLOSED: the
// one component whose job is to halt the loop under adversarial drift must not
// default to "go" when it cannot read its own marker (RSI code eval H5). An
// unreadable marker, or a present-but-unparseable one, reads as frozen; only a
// genuinely ABSENT marker (fresh install, or a real clear transition) reads as
// not-frozen.
func (t *Tracker) AutoAdoptFrozen() bool {
	path := t.autoAdoptFreezePath()
	verdicts, err := jsonlstore.Load[driftVerdict](path)
	if err != nil {
		if t.logger != nil {
			t.logger.Warn("drift brake: freeze marker unreadable, failing closed (frozen)", "error", err)
		}
		return true
	}
	if len(verdicts) > 0 {
		return verdicts[len(verdicts)-1].Frozen
	}
	// Zero records with no error: absent file (allow) vs present-but-empty or
	// all-lines-corrupt (jsonlstore skips bad lines silently) → fail closed.
	if _, statErr := os.Stat(path); statErr == nil {
		if t.logger != nil {
			t.logger.Warn("drift brake: freeze marker present but held no verdict, failing closed (frozen)")
		}
		return true
	}
	return false
}

// auditEvolutionDrift computes the drift verdict from the ledgers. Pure read;
// the caller decides whether to persist/act. judgeRuns may be nil (judge
// accuracy is one of several signals, not required).
func (t *Tracker) auditEvolutionDrift() driftVerdict {
	// Fresh compute, not the 60s-cached EvolutionHealth(): the self-brake must
	// react to the current trajectory, never a stale snapshot.
	t.mu.Lock()
	h := t.computeEvolutionHealthLocked(time.Now())
	t.mu.Unlock()
	v := driftVerdict{
		FalseAcceptRate: h.FalseAcceptRate,
		ConfirmRate:     h.ConfirmRate,
		Resolved:        h.ResolvedEvolves7d,
	}

	// Signal 1: judge going soft (accepting bad evolves).
	if h.ResolvedEvolves7d >= driftMinResolved && h.FalseAcceptRate >= driftFalseAcceptCeil {
		v.Signals = append(v.Signals, driftSignal{
			Kind:   "judge_soft",
			Detail: fmt.Sprintf("falseAcceptRate %.2f over %d resolved evolves", h.FalseAcceptRate, h.ResolvedEvolves7d),
			Value:  h.FalseAcceptRate,
		})
	}

	// Signals 2 & 3 read the meta-evolution ledger.
	if revs, err := t.RecentMetaRevisions(20); err == nil {
		adopted, reverted, streak := driftMetaCounts(revs)
		if adopted >= 2 && float64(reverted)/float64(adopted) >= driftMetaRevertCeil {
			v.Signals = append(v.Signals, driftSignal{
				Kind:   "meta_revert_spike",
				Detail: fmt.Sprintf("%d of %d recent meta-adoptions reverted", reverted, adopted),
				Value:  float64(reverted) / float64(adopted),
			})
		}
		if streak >= driftMonotonyStreak {
			v.Signals = append(v.Signals, driftSignal{
				Kind:   "adoption_monotony",
				Detail: fmt.Sprintf("%d consecutive adoptions of the same artifact (diversity collapse)", streak),
				Value:  float64(streak),
			})
		}
	}

	// Signal 4: the verifier itself failing planted defects. Scored on the
	// must-catch blatant classes ONLY: those are the "a competent judge always
	// rejects these" contract, so missing half of them is real breakage.
	// Subtle/weaken-tier misses are the P3 label food the probe curriculum
	// ladder exists to produce — an aggregate rate would misread a
	// hard-probe-heavy run ("judge can't catch hard probes yet", normal) as
	// verifier breakage and freeze a healthy lane.
	// Prefer the newest usable probe run — an infra outage row (all verdict
	// errors) must not freeze auto-adopt as verifier_broken.
	if runs, err := t.recentJudgeAccuracy(8); err == nil {
		for _, run := range runs {
			if !judgeAccuracyProbeUsable(run) {
				continue
			}
			if correct, total := driftMustCatchCounts(run); total > 0 {
				rate := float64(correct) / float64(total)
				if rate < driftJudgeAccuracyFloor {
					v.Signals = append(v.Signals, driftSignal{
						Kind:   "verifier_broken",
						Detail: fmt.Sprintf("judge caught only %.0f%% of must-catch planted defects", rate*100),
						Value:  rate,
					})
				}
			}
			break
		}
	}

	v.Frozen = len(v.Signals) > 0
	return v
}

// driftMetaCounts walks the meta-revision ledger (newest first) and returns
// how many of the recent lifecycle records are adoptions, how many are
// reverts, and the current same-artifact adoption streak from the newest end.
//
// The streak measures DIVERSITY COLLAPSE — a loop that has stopped considering
// anything but one lever. So any cycle touching a different artifact ends it,
// including one that proposed nothing: a skip is still the loop looking at that
// artifact and deciding, honestly, that its scoreboard held no work.
//
// Counting adoptions alone misreads a healthy rotation. The meta loop cycles
// producer -> evaluator -> genesis every three days; when two of the three have
// no actionable rejection evidence they correctly skip, and only the third ever
// adopts. That is not collapse, but with skips invisible it looked identical
// and froze L2 on 2026-07-31 (three progressive versions of one prompt, zero
// corroborating quality signals — falseAccept/confirm/resolved all 0).
func driftMetaCounts(revs []MetaRevisionRecord) (adopted, reverted, streak int) {
	var streakArtifact string
	streakActive := true
	for _, r := range revs { // newest first
		switch r.Action {
		case "auto_adopted", "adopted":
			adopted++
			if streakActive {
				if streakArtifact == "" {
					streakArtifact = r.Artifact
					streak = 1
				} else if r.Artifact == streakArtifact {
					streak++
				} else {
					streakActive = false
				}
			}
		case "auto_reverted", "operator_reverted":
			reverted++
			// A revert interrupts the "unbroken adoption streak" reading.
			streakActive = false
		default:
			// A cycle that adopted nothing (skip, proposal awaiting review,
			// rejection). It still proves the loop considered this artifact, so
			// it breaks a streak held by a DIFFERENT one. Records for the streak
			// artifact itself are its own proposal trail and change nothing.
			if streakActive && streakArtifact != "" && r.Artifact != streakArtifact {
				streakActive = false
			}
		}
	}
	return adopted, reverted, streak
}

// driftMustCatchCounts returns a lane run's correct/total over the must-catch
// blatant degradation classes (blatantJudgeDegradations). A record predating
// the ByClass breakdown falls back to its aggregate counts — the only signal a
// legacy record carries. A run with no must-catch pairs yields total 0: no
// evidence either way, so the caller skips the signal.
func driftMustCatchCounts(rec judgeAccuracyRecord) (correct, total int) {
	if len(rec.ByClass) == 0 {
		return rec.Correct, rec.Pairs
	}
	for _, d := range blatantJudgeDegradations {
		ct := rec.ByClass[d.name]
		correct += ct[0]
		total += ct[1]
	}
	return correct, total
}

// runEvolutionDriftAudit computes the verdict, persists a freeze/clear
// transition to the marker + lifecycle ledger, and returns the verdict. It is
// idempotent: it only writes on a state CHANGE (clear→frozen or frozen→clear)
// so the log stays readable. onTransition, if set, fires once per change
// (feed-card surface).
func (t *Tracker) runEvolutionDriftAudit(onTransition func(frozen bool, reasons []string)) driftVerdict {
	v := t.auditEvolutionDrift()
	v.CreatedAt = time.Now().UnixMilli()
	was := t.AutoAdoptFrozen()
	if v.Frozen == was {
		return v // no transition — don't spam the marker log
	}
	if err := jsonlstore.Append(t.autoAdoptFreezePath(), v); err != nil {
		if t.logger != nil {
			t.logger.Warn("drift audit: marker write failed", "error", err)
		}
		return v
	}
	reasons := make([]string, 0, len(v.Signals))
	for _, s := range v.Signals {
		reasons = append(reasons, s.Kind+": "+s.Detail)
	}
	reason := "trajectory recovered — auto-adopt resumed"
	if v.Frozen {
		reason = "auto-adopt FROZEN (self-brake): " + strings.Join(reasons, "; ")
	}
	if err := jsonlstore.Append(t.logPath, evolveLogEntry{
		Type:      "auto_adopt_freeze_change",
		Reason:    reason,
		CreatedAt: v.CreatedAt,
	}); err != nil && t.logger != nil {
		t.logger.Warn("drift audit: lifecycle write failed", "error", err)
	}
	if t.logger != nil {
		if v.Frozen {
			t.logger.Warn("evolution drift audit: SELF-BRAKE engaged", "reasons", reasons)
		} else {
			t.logger.Info("evolution drift audit: trajectory recovered, auto-adopt resumed")
		}
	}
	if onTransition != nil {
		onTransition(v.Frozen, reasons)
	}
	return v
}
