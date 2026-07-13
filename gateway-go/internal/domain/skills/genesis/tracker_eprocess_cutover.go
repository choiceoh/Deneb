package genesis

// e-process cutover mechanism (RSI graduation ladder, "e-process observation
// mode" row). Observation mode (#3439) accumulates RollbackBaselineTest
// disagreement labels on every resolving watch; this file adds the two halves
// the cutover itself was missing:
//
//  1. eProcessCutoverReadiness — the deterministic evidence summary (label
//     count, legacy-agreement rate) against the ladder thresholds, surfaced on
//     miniapp.rsi.status so the operator SEES when evidence justifies the flip
//     instead of re-mining JSONL by hand.
//  2. DENEB_EPROCESS_OWNS_ROLLBACK=1 — the operator lever that hands rollback
//     firing to the anytime-valid test (maybeFireRollbackLocked). Off by
//     default. Flipping it is the graduation ACTION, taken on track record,
//     never on calendar — the readiness struct is the track record. After
//     cutover both verdicts keep being recorded on every resolving entry
//     (stashBaselineTestLocked compares threshold vs e-process regardless of
//     owner), so the decision stays auditable and reversible.

import (
	"os"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// Ladder thresholds (roadmap graduation ladder): observation labels justify
// cutover at n>=20 with legacy agreement >=90%. Deterministic Go by the P1
// invariant — the acceptance circuit is never self-editable.
const (
	eProcessCutoverMinLabels    = 20
	eProcessCutoverMinAgreement = 0.90
)

// eProcessOwnsRollback reports whether rollback firing belongs to the
// anytime-valid e-process: the operator env knob (always wins, "0" forces
// legacy ownership even past an auto-graduation), or an executed
// graduation-ladder unlock (operator directive 2026-07-14 — the loop may
// flip its own pre-declared, evidence-met locks).
func eProcessOwnsRollback() bool {
	switch os.Getenv("DENEB_EPROCESS_OWNS_ROLLBACK") {
	case "1":
		return true
	case "0":
		return false
	}
	return graduationUnlocked(graduationEProcess)
}

// eProcessCutoverReadiness summarizes the observation-mode label evidence
// against the graduation thresholds. Labels accumulate over the loop's whole
// history — a windowed count would starve the very evidence the ladder gates
// on at organic (~3 evolves/week) cadence.
type eProcessCutoverReadiness struct {
	Labels        int     `json:"labels"`
	Disagreements int     `json:"disagreements"`
	AgreementRate float64 `json:"agreementRate"`
	Ready         bool    `json:"ready"`
	// EProcessOwner mirrors the DENEB_EPROCESS_OWNS_ROLLBACK knob so status
	// surfaces can distinguish "ready, awaiting the flip" from "flipped".
	EProcessOwner bool `json:"eProcessOwner"`
}

// eProcessCutoverReadiness scans the lifecycle ledger for entries carrying a
// baseline-test verdict and scores them against the ladder thresholds. Lock-
// free like the other ledger readers (RecentJudgeAccuracy, RecentMetaRevisions):
// jsonlstore.Load tolerates a concurrent append's partial tail line.
func (t *Tracker) eProcessCutoverReadiness() eProcessCutoverReadiness {
	out := eProcessCutoverReadiness{EProcessOwner: eProcessOwnsRollback()}
	entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.BaselineTest == nil {
			continue
		}
		out.Labels++
		if e.BaselineTest.Disagreement {
			out.Disagreements++
		}
	}
	if out.Labels > 0 {
		out.AgreementRate = float64(out.Labels-out.Disagreements) / float64(out.Labels)
	}
	out.Ready = out.Labels >= eProcessCutoverMinLabels && out.AgreementRate >= eProcessCutoverMinAgreement
	return out
}
