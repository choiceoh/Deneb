package genesis

import (
	"sort"
	"strings"
)

// LeverYield is the effectiveness of one evolution "lever" — a (target failure
// signature × edited surface) combination — aggregated over the lifecycle log
// (#2, HarnessX Appendix D). It answers "do Procedure edits on timeout
// signatures hold up more often than Pitfalls edits?", which per-skill tracking
// could not. Committed counts shipped evolves on the lever; Confirmed counts
// those that proved out over the post-evolve window; Partial counts
// confirmed-but-target-recurred; RolledBack counts reverts; ConfirmRate is
// Confirmed/Committed.
type LeverYield struct {
	Signature   string  `json:"signature"`
	Surface     string  `json:"surface"`
	Committed   int     `json:"committed"`
	Confirmed   int     `json:"confirmed"`
	Partial     int     `json:"partial"`
	RolledBack  int     `json:"rolledBack"`
	ConfirmRate float64 `json:"confirmRate"`
}

type leverKey struct {
	sig     string
	surface string
}

func leverKeyFromAudit(audit *HarnessEditAudit) leverKey {
	if audit == nil {
		return leverKey{}
	}
	return leverKey{
		sig:     normalizedSelfHarnessSignature(audit.TargetSignature),
		surface: canonicalSkillSurface(strings.ToLower(strings.TrimSpace(audit.EditedSurface))),
	}
}

// LeverYields aggregates the lifecycle log into per-lever effectiveness. It pairs
// each shipped evolve (an "evolved" entry, which carries the Self-Harness audit)
// with its later outcome ("evolve_confirmed" / "evolve_rolled_back") for the same
// skill — rollback entries carry no audit, so the lever is recovered from that
// skill's most recent shipped evolve. limit bounds how much of the log is read.
func (t *Tracker) LeverYields(limit int) ([]LeverYield, error) {
	entries, err := t.RecentLifecycleLog(limit)
	if err != nil {
		return nil, err
	}
	agg := map[leverKey]*LeverYield{}
	lastLever := map[string]leverKey{} // skill -> lever of its last shipped evolve
	get := func(k leverKey) *LeverYield {
		y := agg[k]
		if y == nil {
			y = &LeverYield{Signature: k.sig, Surface: k.surface}
			agg[k] = y
		}
		return y
	}
	// RecentLifecycleLog returns newest-first; walk oldest-first so a skill's
	// shipped lever is known before its later confirm/rollback is attributed.
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		switch e.Type {
		case "evolved":
			k := leverKeyFromAudit(e.SelfHarnessAudit)
			lastLever[e.SkillName] = k
			get(k).Committed++
		case "evolve_confirmed":
			k := leverKeyFromAudit(e.SelfHarnessAudit)
			if k == (leverKey{}) {
				k = lastLever[e.SkillName]
			}
			y := get(k)
			y.Confirmed++
			if strings.HasPrefix(e.Reason, "partial") {
				y.Partial++
			}
		case "evolve_rolled_back":
			get(lastLever[e.SkillName]).RolledBack++
		}
	}
	out := make([]LeverYield, 0, len(agg))
	for _, y := range agg {
		if y.Committed == 0 {
			// Orphaned confirm/rollback whose 'evolved' entry fell outside the
			// limit window — no shipped lever to attribute it to; skip the phantom
			// (empty-signature, Committed==0) bucket rather than emit it.
			continue
		}
		y.ConfirmRate = float64(y.Confirmed) / float64(y.Committed)
		out = append(out, *y)
	}
	// Stable, deterministic order: most-shipped levers first.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Committed != out[j].Committed {
			return out[i].Committed > out[j].Committed
		}
		if out[i].Signature != out[j].Signature {
			return out[i].Signature < out[j].Signature
		}
		return out[i].Surface < out[j].Surface
	})
	return out, nil
}

// LowYieldLevers returns levers the evolver should stop proposing: a
// (signature×surface) pair whose RESOLVED evolves confirm at or below
// maxConfirmRate. It feeds the evolve prompt's avoid-directions (HarnessX
// Appendix D).
//
// Two refinements over a raw Committed-denominator point estimate — the data
// regime is sparse, so the rule must use every signal yet still decide early:
//
//   - Resolved denominator (Confirmed+RolledBack): pending, not-yet-confirmed
//     evolves are excluded, so a lever with confirms-plus-pending isn't falsely
//     avoided (raw Confirmed/Committed counts a pending evolve like a failure).
//     Reverts — the strongest negative signal, ignored by Confirmed/Committed —
//     count directly.
//   - Laplace smoothing (Beta(1,1) posterior mean): (Confirmed+1)/(resolved+2)
//     shrinks a 0/3 toward 0.2 and a 1/3 toward 0.4 so one or two resolved
//     outcomes can't slam the rate to 0/1. This is the decisive point-estimate
//     use of the posterior; a credible-interval variant was prototyped and
//     rejected (at single-user volumes it needs ~60 resolved to flag a
//     borderline lever, so it would never fire — uncertainty paralysis).
//
// minResolved gates on resolved outcomes (confirm+revert), not bare ships.
func (t *Tracker) LowYieldLevers(limit, minResolved int, maxConfirmRate float64) ([]LeverYield, error) {
	all, err := t.LeverYields(limit)
	if err != nil {
		return nil, err
	}
	return filterLowYieldLevers(all, minResolved, maxConfirmRate), nil
}

// filterLowYieldLevers is the pure avoid-decision (resolved denominator + Laplace
// smoothing), split out for testability.
func filterLowYieldLevers(levers []LeverYield, minResolved int, maxConfirmRate float64) []LeverYield {
	var low []LeverYield
	for _, y := range levers {
		resolved := y.Confirmed + y.RolledBack
		if resolved < minResolved {
			continue // not enough resolved evidence to avoid the direction yet
		}
		smoothed := float64(y.Confirmed+1) / float64(resolved+2)
		if smoothed <= maxConfirmRate {
			low = append(low, y)
		}
	}
	return low
}
