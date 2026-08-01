package genesis

import (
	"strings"
	"testing"
)

// A gate that always answers the same way has stopped measuring. Three of the
// 2026-08-01 findings were that in different clothes — always-pass probes,
// an always-fail bar, always-agree impact verdicts — and none were noticed
// until someone asked why the loop was quiet.

func repeated(label string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = label
	}
	return out
}

func TestUnanimousGateReadsSaturated(t *testing.T) {
	// The judge corpus: 11 days, every run 100%.
	got := MeasureGateDiscrimination(repeated("all-caught", 20))
	if !got.Saturated {
		t.Error("a gate with one answer over a large sample has stopped measuring")
	}
	if got.MinorityShare != 0 {
		t.Errorf("unanimity is zero separation, got %v", got.MinorityShare)
	}
}

func TestAlwaysFailingGateIsAlsoSaturated(t *testing.T) {
	// The unreachable fan-out bar carries exactly as much information as the
	// always-passing probe corpus: none.
	if !MeasureGateDiscrimination(repeated("fail", 20)).Saturated {
		t.Error("always-fail must read saturated too — the direction is irrelevant")
	}
}

func TestSmallUnanimousSampleIsNotSaturation(t *testing.T) {
	// Four identical outcomes is unremarkable; calling it saturation would cry
	// wolf on every freshly-deployed gate.
	if MeasureGateDiscrimination(repeated("pass", 4)).Saturated {
		t.Error("a thin sample must not be reported as saturation")
	}
}

func TestSeparationIsScoredByMinorityShare(t *testing.T) {
	outcomes := append(repeated("pass", 15), repeated("fail", 5)...)
	got := MeasureGateDiscrimination(outcomes)
	if got.Saturated {
		t.Error("a gate that separates must not read saturated")
	}
	if got.MinorityShare < 0.24 || got.MinorityShare > 0.26 {
		t.Errorf("5 of 20 is a 25%% minority, got %v", got.MinorityShare)
	}
	if got.Distinct != 2 {
		t.Errorf("distinct = %d, want 2", got.Distinct)
	}
}

func TestEmptyOutcomesAreNotSaturation(t *testing.T) {
	// No runs is "no data", which is a different problem from a dead gate.
	got := MeasureGateDiscrimination(nil)
	if got.Saturated || got.Samples != 0 {
		t.Errorf("no samples must not read saturated: %+v", got)
	}
}

func TestSaturationIsNamedNotScoredInTheDashboard(t *testing.T) {
	// Rendering saturation as a high number would read as a good result; it is
	// the absence of a result.
	value := rsiDiscriminationValue(MeasureGateDiscrimination(repeated("all-caught", 20)))
	if !strings.Contains(value, "포화") || !strings.Contains(value, "계측 정지") {
		t.Errorf("saturation must be called out, got %q", value)
	}
}
