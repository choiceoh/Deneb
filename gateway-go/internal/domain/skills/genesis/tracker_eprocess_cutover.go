package genesis

// e-process cutover mechanism (RSI graduation ladder, "e-process observation
// mode" row). Observation mode (#3439) accumulates RollbackBaselineTest
// disagreement labels on every resolving watch; this file adds the two halves
// the cutover itself was missing:
//
//  1. EProcessCutoverReadiness — the deterministic evidence summary (label
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

// eProcessOwnsRollback reports whether the operator flipped rollback firing
// over to the anytime-valid e-process (DENEB_EPROCESS_OWNS_ROLLBACK=1).
func eProcessOwnsRollback() bool {
	return os.Getenv("DENEB_EPROCESS_OWNS_ROLLBACK") == "1"
}

// EProcessCutoverReadiness summarizes the observation-mode label evidence
// against the graduation thresholds. Labels accumulate over the loop's whole
// history — a windowed count would starve the very evidence the ladder gates
// on at organic (~3 evolves/week) cadence.
type EProcessCutoverReadiness struct {
	Labels        int     `json:"labels"`
	Disagreements int     `json:"disagreements"`
	AgreementRate float64 `json:"agreementRate"`
	Ready         bool    `json:"ready"`
	// EProcessOwner mirrors the DENEB_EPROCESS_OWNS_ROLLBACK knob so status
	// surfaces can distinguish "ready, awaiting the flip" from "flipped".
	EProcessOwner bool `json:"eProcessOwner"`
}

// EProcessCutoverReadiness scans the lifecycle ledger for entries carrying a
// baseline-test verdict and scores them against the ladder thresholds. Lock-
// free like the other ledger readers (RecentJudgeAccuracy, RecentMetaRevisions):
// jsonlstore.Load tolerates a concurrent append's partial tail line.
func (t *Tracker) EProcessCutoverReadiness() EProcessCutoverReadiness {
	out := EProcessCutoverReadiness{EProcessOwner: eProcessOwnsRollback()}
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
