package router

import "testing"

// TestEffortNudge_DirectionAndGuards covers the policy's two firing directions
// and every no-op guard: the bounded recalibration must move the gate only on a
// trusted signal, in the right direction, by exactly one step.
func TestEffortNudge_DirectionAndGuards(t *testing.T) {
	cases := []struct {
		name        string
		current     int
		sig         EffortSignal
		wantNext    int
		wantChanged bool
	}{
		{
			name:    "high escalation steps gate down (stricter)",
			current: 140,
			// 4/20 routed runs escalated = 0.20 > 0.15, over the routed-run floor.
			sig:         EffortSignal{Sample: 50, RoutedRuns: 20, EscalationRate: 0.20, RoutedShare: 0.40},
			wantNext:    140 - EffortNudgeStep,
			wantChanged: true,
		},
		{
			name:    "near-zero routed share with healthy escalation steps gate up (looser)",
			current: 140,
			// router fired on 2/100 runs (0.02) and none escalated → too strict.
			sig:         EffortSignal{Sample: 100, RoutedRuns: 2, EscalationRate: 0, RoutedShare: 0.02},
			wantNext:    140 + EffortNudgeStep,
			wantChanged: true,
		},
		{
			name:        "sample below minimum: no-op even with extreme rates",
			current:     140,
			sig:         EffortSignal{Sample: EffortNudgeMinSample - 1, RoutedRuns: 5, EscalationRate: 1, RoutedShare: 0},
			wantNext:    140,
			wantChanged: false,
		},
		{
			name:    "high escalation but too few routed runs: signal untrusted, no-op",
			current: 140,
			// 3/3 escalated looks catastrophic but RoutedRuns < EffortNudgeMinRouted.
			sig:         EffortSignal{Sample: 40, RoutedRuns: 3, EscalationRate: 1, RoutedShare: 0.075},
			wantNext:    140,
			wantChanged: false,
		},
		{
			name:    "low routed share but unhealthy escalation: do NOT widen a wrong router",
			current: 140,
			// RoutedShare 0.05 would invite a widen, but escalation 0.5 > healthy guard.
			sig:         EffortSignal{Sample: 60, RoutedRuns: 3, EscalationRate: 0.5, RoutedShare: 0.05},
			wantNext:    140,
			wantChanged: false,
		},
		{
			name:        "healthy middle: neither condition holds, no-op",
			current:     140,
			sig:         EffortSignal{Sample: 80, RoutedRuns: 30, EscalationRate: 0.10, RoutedShare: 0.375},
			wantNext:    140,
			wantChanged: false,
		},
		{
			name:        "uninitialized gate (<=0) is left for the caller to seed",
			current:     0,
			sig:         EffortSignal{Sample: 100, RoutedRuns: 50, EscalationRate: 0.5, RoutedShare: 0.5},
			wantNext:    0,
			wantChanged: false,
		},
		{
			name:        "escalation exactly at threshold does not fire (strict >)",
			current:     140,
			sig:         EffortSignal{Sample: 60, RoutedRuns: 20, EscalationRate: EffortNudgeHighEscalation, RoutedShare: 0.33},
			wantNext:    140,
			wantChanged: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next, changed := EffortNudge(tc.current, tc.sig)
			if next != tc.wantNext || changed != tc.wantChanged {
				t.Fatalf("EffortNudge(%d, %+v) = (%d, %v), want (%d, %v)",
					tc.current, tc.sig, next, changed, tc.wantNext, tc.wantChanged)
			}
		})
	}
}

// TestEffortNudge_ClampBand pins the [min,max] band so the loop can never run
// away: a down-nudge at the floor and an up-nudge at the ceiling must report no
// change, and a value already outside the band is pulled back inside.
func TestEffortNudge_ClampBand(t *testing.T) {
	downSig := EffortSignal{Sample: 60, RoutedRuns: 20, EscalationRate: 0.5, RoutedShare: 0.40}
	upSig := EffortSignal{Sample: 100, RoutedRuns: 2, EscalationRate: 0, RoutedShare: 0.02}

	t.Run("down-nudge at floor stays put", func(t *testing.T) {
		next, changed := EffortNudge(EffortNudgeMin, downSig)
		if next != EffortNudgeMin || changed {
			t.Fatalf("at floor: got (%d, %v), want (%d, false)", next, changed, EffortNudgeMin)
		}
	})

	t.Run("up-nudge at ceiling stays put", func(t *testing.T) {
		next, changed := EffortNudge(EffortNudgeMax, upSig)
		if next != EffortNudgeMax || changed {
			t.Fatalf("at ceiling: got (%d, %v), want (%d, false)", next, changed, EffortNudgeMax)
		}
	})

	t.Run("down-nudge one step above floor lands exactly on floor", func(t *testing.T) {
		next, changed := EffortNudge(EffortNudgeMin+EffortNudgeStep, downSig)
		if next != EffortNudgeMin || !changed {
			t.Fatalf("one step above floor: got (%d, %v), want (%d, true)", next, changed, EffortNudgeMin)
		}
	})

	t.Run("down-nudge within a partial step of the floor clamps to floor", func(t *testing.T) {
		start := EffortNudgeMin + EffortNudgeStep - 1 // a full step would undershoot
		next, changed := EffortNudge(start, downSig)
		if next != EffortNudgeMin || !changed {
			t.Fatalf("partial step above floor: got (%d, %v), want (%d, true)", next, changed, EffortNudgeMin)
		}
		if next < EffortNudgeMin {
			t.Fatalf("clamp breached floor: %d < %d", next, EffortNudgeMin)
		}
	})

	t.Run("value above band is pulled back down by an up-nudge clamp", func(t *testing.T) {
		next, changed := EffortNudge(EffortNudgeMax+50, upSig)
		if next != EffortNudgeMax || !changed {
			t.Fatalf("above band: got (%d, %v), want (%d, true)", next, changed, EffortNudgeMax)
		}
	})

	t.Run("repeated nudges converge and stay in band (no runaway)", func(t *testing.T) {
		v := 140
		for i := 0; i < 100; i++ {
			v, _ = EffortNudge(v, downSig)
			if v < EffortNudgeMin || v > EffortNudgeMax {
				t.Fatalf("iteration %d left band: %d not in [%d,%d]", i, v, EffortNudgeMin, EffortNudgeMax)
			}
		}
		if v != EffortNudgeMin {
			t.Fatalf("after 100 down-nudges want floor %d, got %d", EffortNudgeMin, v)
		}
	})
}

// TestEffortNudgeBandSanity guards the band invariants the policy relies on:
// the shipped default sits strictly inside the band and a single step never
// jumps the whole band.
func TestEffortNudgeBandSanity(t *testing.T) {
	def := DefaultProfile().MaxSimpleRunes
	if def <= EffortNudgeMin || def >= EffortNudgeMax {
		t.Fatalf("default gate %d must sit strictly inside [%d,%d]", def, EffortNudgeMin, EffortNudgeMax)
	}
	if EffortNudgeStep <= 0 || EffortNudgeStep >= EffortNudgeMax-EffortNudgeMin {
		t.Fatalf("step %d must be positive and smaller than the band width %d",
			EffortNudgeStep, EffortNudgeMax-EffortNudgeMin)
	}
}
