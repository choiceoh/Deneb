package genesis

// Discrimination — how much a gate actually SEPARATED lately.
//
// Three of the 2026-08-01 findings were the same measurement failure wearing
// different clothes:
//
//   - the judge probe corpus returned 100% on all nine classes for 11 days
//   - the server fan-out bar sat below a target no refactor could reach
//   - impact verdicts came back "verified" from a metric that could not fail
//
// One always passes, one always fails, one always agrees — and all three carry
// the same information: none. A gate whose outcome never varies has stopped
// measuring, and today nothing notices until a human asks why the loop is
// quiet. Saturation is not a score; it is the absence of a score.
//
// So score the SPREAD, not the value. This is deliberately not entropy or
// variance: outcomes here are small categorical sets, usually two, and a single
// number that says "how far from unanimous" reads directly and cannot be
// gamed by adding rarely-taken branches.

// GateDiscrimination summarizes one gate's recent outcomes.
type GateDiscrimination struct {
	// Samples is how many outcomes were scored.
	Samples int `json:"samples"`
	// Distinct is how many different outcomes appeared.
	Distinct int `json:"distinct"`
	// MinorityShare is the fraction NOT taken by the most common outcome:
	// 0.0 = unanimous (no information), 0.5 = an even split (maximum
	// separation for two outcomes).
	MinorityShare float64 `json:"minorityShare"`
	// Saturated is true when the gate returned one answer for every scored
	// outcome over a sample large enough to mean something.
	Saturated bool `json:"saturated"`
}

// gateDiscriminationMinSamples is the floor below which unanimity is just a
// small sample. Four consecutive identical outcomes is unremarkable; a dozen is
// a gate that has stopped asking a question.
const gateDiscriminationMinSamples = 12

// MeasureGateDiscrimination scores a gate from its recent outcome labels,
// newest-first or oldest-first alike — order does not matter, only the spread.
// Callers pass whatever label the gate emits (pass/fail, verdict names, class
// names); the function makes no assumption about their meaning.
func MeasureGateDiscrimination(outcomes []string) GateDiscrimination {
	out := GateDiscrimination{Samples: len(outcomes)}
	if len(outcomes) == 0 {
		return out
	}
	counts := map[string]int{}
	top := 0
	for _, o := range outcomes {
		counts[o]++
		if counts[o] > top {
			top = counts[o]
		}
	}
	out.Distinct = len(counts)
	out.MinorityShare = float64(len(outcomes)-top) / float64(len(outcomes))
	out.Saturated = out.Distinct == 1 && out.Samples >= gateDiscriminationMinSamples
	return out
}
