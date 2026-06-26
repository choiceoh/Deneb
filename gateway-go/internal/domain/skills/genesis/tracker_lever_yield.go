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
		if y.Committed > 0 {
			y.ConfirmRate = float64(y.Confirmed) / float64(y.Committed)
		}
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

// LowYieldLevers returns levers that shipped at least minShips times yet confirm
// at or below maxConfirmRate — combinations the evolver should stop proposing.
// Intended to feed the evolve prompt's avoid-directions so a signature×surface
// pair that historically does not hold up is deprioritized (HarnessX Appendix D).
func (t *Tracker) LowYieldLevers(limit, minShips int, maxConfirmRate float64) ([]LeverYield, error) {
	all, err := t.LeverYields(limit)
	if err != nil {
		return nil, err
	}
	var low []LeverYield
	for _, y := range all {
		if y.Committed >= minShips && y.ConfirmRate <= maxConfirmRate {
			low = append(low, y)
		}
	}
	return low, nil
}
