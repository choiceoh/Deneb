package regressionwatch

import "math"

// Thresholds decides when a signal's drift counts as a regression. A drift must
// clear BOTH the relative and absolute floor so a tiny baseline doesn't trip on
// noise (1% → 2% is +100% relative but trivial in absolute terms) and a large
// baseline doesn't trip on a small relative wiggle.
type Thresholds struct {
	RelPct        float64 // relative change vs baseline, e.g. 0.30 = +30% (rate/scalar)
	AbsNoiseFloor float64 // min absolute move for a RATE's relative trip (noise cut)
	AbsHard       float64 // absolute move that trips a RATE on its own, e.g. 0.10 = 10 points
	CountHard     float64 // default absolute rise that trips a KindCount with no HardFloor
	MinSample     int     // ignore RATE/SCALAR signals with fewer samples (count is exempt)
}

// DefaultThresholds are deliberately conservative for the observe-only stage —
// better to under-report while we learn the real noise floor than to cry wolf.
func DefaultThresholds() Thresholds {
	return Thresholds{RelPct: 0.30, AbsNoiseFloor: 0.02, AbsHard: 0.10, CountHard: 5, MinSample: 20}
}

// BaselineEntry is the stable value for one signal key.
type BaselineEntry struct {
	Value       float64 `json:"value"`
	Sample      int     `json:"sample"`
	HigherWorse bool    `json:"higherWorse"`
	UpdatedAtMs int64   `json:"updatedAtMs"`
}

// Baseline is the persisted rolling snapshot, keyed by Signal.FullKey().
type Baseline struct {
	GeneratedAtMs int64                    `json:"generatedAtMs"`
	Entries       map[string]BaselineEntry `json:"entries"`
}

// Regression is one detected deterioration of a signal versus its baseline.
type Regression struct {
	Key         string
	Value       float64
	Baseline    float64
	DeltaPct    float64
	HigherWorse bool
}

// detect compares current signals against the baseline and returns the ones
// that regressed past the thresholds. Signals with no baseline yet, or below
// MinSample, are skipped — the cycle folds them into the baseline instead.
func detect(base Baseline, current []Signal, th Thresholds) []Regression {
	var out []Regression
	for _, s := range current {
		// Count signals carry no sample size (a delivery failure either happened
		// or it didn't), so only rate/scalar signals face the noise gate.
		if s.Kind != KindCount && s.Sample < th.MinSample {
			continue
		}
		prev, ok := base.Entries[s.FullKey()]
		if !ok {
			continue
		}
		if regressed(prev.Value, s.Value, s.HigherWorse, s.Kind, s.HardFloor, th) {
			out = append(out, Regression{
				Key:         s.FullKey(),
				Value:       s.Value,
				Baseline:    prev.Value,
				DeltaPct:    relDelta(prev.Value, s.Value),
				HigherWorse: s.HigherWorse,
			})
		}
	}
	return out
}

// regressed reports whether current deteriorated past the thresholds, in the
// direction HigherWorse specifies. The test differs by kind:
//   - KindRate: a big-enough relative move above the noise floor OR a hard
//     absolute move — so a 0.90→0.70 cache-hit drop trips on absolute size
//     (0.20) even though it is only 22% relative.
//   - KindScalar: relative change alone (an absolute "points" floor is
//     meaningless for ms).
//   - KindCount: an absolute rise past HardFloor (a relative test can't fire
//     from a ~0 baseline).
func regressed(base, cur float64, higherWorse bool, kind SignalKind, hardFloor float64, th Thresholds) bool {
	delta := cur - base
	if !higherWorse {
		delta = -delta // for higher-is-better signals, a fall is the bad direction
	}
	if delta <= 0 {
		return false // moved in the good direction (or flat)
	}
	relTrip := math.Abs(base) > 1e-9 && delta/math.Abs(base) >= th.RelPct
	switch kind {
	case KindScalar:
		return relTrip
	case KindCount:
		floor := hardFloor
		if floor <= 0 {
			floor = th.CountHard
		}
		return delta >= floor
	default: // KindRate
		if relTrip && delta >= th.AbsNoiseFloor {
			return true
		}
		return delta >= th.AbsHard
	}
}

func relDelta(base, cur float64) float64 {
	if math.Abs(base) < 1e-9 {
		return 0
	}
	return (cur - base) / math.Abs(base)
}

// updateBaseline folds the current cycle into the baseline with an EMA so it
// tracks slow drift — EXCEPT keys that just regressed, which keep their old
// baseline so a sustained regression keeps tripping instead of being absorbed
// into "normal". New keys are seeded at their current value.
func updateBaseline(base Baseline, current []Signal, regressed []Regression, nowMs int64) Baseline {
	const emaAlpha = 0.3
	regKeys := make(map[string]bool, len(regressed))
	for _, r := range regressed {
		regKeys[r.Key] = true
	}
	// Copy into a fresh map so the input baseline is never mutated — callers may
	// reuse it, and a map is shared by reference otherwise.
	out := Baseline{
		GeneratedAtMs: nowMs,
		Entries:       make(map[string]BaselineEntry, len(base.Entries)+len(current)),
	}
	for k, v := range base.Entries {
		out.Entries[k] = v
	}
	for _, s := range current {
		if s.Sample == 0 {
			continue
		}
		k := s.FullKey()
		if regKeys[k] {
			continue // do not absorb a live regression into the baseline
		}
		v := s.Value
		if prev, ok := out.Entries[k]; ok {
			v = emaAlpha*s.Value + (1-emaAlpha)*prev.Value
		}
		out.Entries[k] = BaselineEntry{
			Value:       v,
			Sample:      s.Sample,
			HigherWorse: s.HigherWorse,
			UpdatedAtMs: nowMs,
		}
	}
	return out
}
