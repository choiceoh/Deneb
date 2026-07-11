package autonomous

import "testing"

// TestSignalConfigForThreshold verifies the operator cadence dial: a positive
// value overrides only EscalateThreshold, and an unset/non-positive value keeps
// the calibrated default byte-for-byte.
func TestSignalConfigForThreshold(t *testing.T) {
	def := DefaultSignalConfig()

	// Unset / non-positive keeps the calibrated default.
	for _, n := range []int{0, -5} {
		got := SignalConfigForThreshold(n)
		if got.EscalateThreshold != def.EscalateThreshold {
			t.Errorf("threshold %d: EscalateThreshold = %d, want default %d", n, got.EscalateThreshold, def.EscalateThreshold)
		}
	}

	// Positive override is applied.
	got := SignalConfigForThreshold(75)
	if got.EscalateThreshold != 75 {
		t.Errorf("EscalateThreshold = %d, want 75", got.EscalateThreshold)
	}

	// Only the threshold changes; every other dial stays calibrated.
	if got.VIPMailWeight != def.VIPMailWeight ||
		got.StaleMailWeight != def.StaleMailWeight ||
		got.ConflictWeight != def.ConflictWeight ||
		got.ImminentWeight != def.ImminentWeight ||
		got.DeadlineWeight != def.DeadlineWeight ||
		got.MaxReasonsPerKind != def.MaxReasonsPerKind ||
		got.StaleMailAge != def.StaleMailAge ||
		got.ImminentEventWindow != def.ImminentEventWindow ||
		got.DeadlineWindow != def.DeadlineWindow {
		t.Errorf("non-threshold fields drifted: got %+v vs default %+v", got, def)
	}
}
