// dreamer_quality.go — a per-cycle quality score for the dream itself.
//
// The workfeed card and DreamReport already count pages created/updated, but
// volume is not quality: a cycle can create ten pages nobody ever reads. This
// scores the OUTPUT along three axes and, crucially, closes the loop with the
// recall-utility ledger (recall_hits.go) so "did my earlier writes get used?"
// becomes a measured number the operator — and a future slow-loop tuner — can
// trend. Honest by construction (the runtime-health discipline): only axes with
// real evidence this cycle contribute, and the weights renormalize over them, so
// an idle or first-ever cycle is not punished for signals it cannot have.
package wiki

import (
	"time"
)

// utilityGrace is how long a freshly written page is exempt from the utility
// denominator: a page created yesterday has not had a fair chance to be
// recalled, so counting it as "unused" would just punish recency.
const utilityGrace = 48 * time.Hour

// dreamQuality is the decomposed score. Score is 0–100; the sub-axes are 0–1.
type dreamQuality struct {
	Score         float64
	Precision     float64 // applied / proposed — synthesis surviving guards
	Confidence    float64 // mean confidence of applied updates
	Utility       float64 // recalled fraction of prior-cycle pages past the grace window
	RecalledPages int     // distinct prior-cycle pages recalled in the score window
	Signals       int     // how many axes contributed (0 → not scored)
}

// confidenceWeight maps a page's confidence label to a 0–1 weight. Empty and
// unknown default to medium — the synthesis prompt's baseline.
func confidenceWeight(label string) float64 {
	switch label {
	case "high":
		return 1.0
	case "low":
		return 0.3
	default: // "medium" or unset
		return 0.6
	}
}

// dreamQualityInputs are the cycle facts the scorer reads, gathered by the phase
// method so the computation itself stays pure and unit-testable.
type dreamQualityInputs struct {
	proposed   int
	applied    int
	updates    []wikiUpdate            // to average applied confidence
	priorPaths []processedDiaryCapsule // capsule history (prior cycles only)
	recalls    map[string]RecallUsage  // page → kind-split ledger usage in the score window
	now        time.Time
}

// computeDreamQuality grades one cycle. Axis weights: precision 0.4, confidence
// 0.2, utility 0.4 — utility is weighted as heavily as precision because a
// precise page nobody reads is still waste. Missing axes drop out and the rest
// renormalize.
func computeDreamQuality(in dreamQualityInputs) dreamQuality {
	var q dreamQuality

	type axis struct {
		value  float64
		weight float64
		ok     bool
	}
	axes := make([]axis, 0, 3)

	// Precision: only meaningful when the cycle actually proposed updates.
	if in.proposed > 0 {
		p := float64(in.applied) / float64(in.proposed)
		if p > 1 {
			p = 1
		}
		q.Precision = p
		axes = append(axes, axis{value: p, weight: 0.4, ok: true})
	}

	// Confidence: mean over this cycle's updates (proxy for applied — dropped
	// items are a minority and the guards do not discriminate by confidence).
	if len(in.updates) > 0 {
		var sum float64
		for _, u := range in.updates {
			sum += confidenceWeight(string(u.Confidence))
		}
		c := sum / float64(len(in.updates))
		q.Confidence = c
		axes = append(axes, axis{value: c, weight: 0.2, ok: true})
	}

	// Utility: of pages written by PRIOR cycles that are past the grace window,
	// how much use they earned. Observed use (the model opened the page or the
	// answer referenced it) earns full credit; injection alone earns half —
	// exposure does not predict use (bridge-evidence adoption), so "was pulled
	// into context once" must not saturate the axis like real engagement does.
	seen := make(map[string]struct{})
	denom, num := 0, 0.0
	for _, cap := range in.priorPaths {
		at, err := time.Parse(time.RFC3339, cap.At)
		if err != nil || in.now.Sub(at) < utilityGrace {
			continue // too fresh (or unstamped) to fairly judge
		}
		for _, p := range cap.Paths {
			if p == "" {
				continue
			}
			if _, dup := seen[p]; dup {
				continue
			}
			seen[p] = struct{}{}
			denom++
			usage := in.recalls[p]
			switch {
			case usage.Used():
				num += 1.0
				q.RecalledPages++
			case usage.Injects > 0:
				num += 0.5
				q.RecalledPages++
			}
		}
	}
	if denom > 0 {
		u := num / float64(denom)
		q.Utility = u
		axes = append(axes, axis{value: u, weight: 0.4, ok: true})
	}

	// Weighted average over the axes that had evidence.
	var wsum, vsum float64
	for _, a := range axes {
		if !a.ok {
			continue
		}
		wsum += a.weight
		vsum += a.value * a.weight
		q.Signals++
	}
	if wsum > 0 {
		q.Score = (vsum / wsum) * 100
	}
	return q
}
