package main

import "testing"

// The backtest must mirror the live deciders exactly: legacy fires on the
// threshold regardless of baseline; the e-process demands baseline-relative
// evidence — their disagreement on noisy-baseline skills is the report's point.
func TestBacktest(t *testing.T) {
	mkUsage := func(skill string, at int64, ok bool) usageRec {
		return usageRec{SkillName: skill, UsedAt: at, Success: ok}
	}
	evolve := lifecycleRec{Type: "evolved", SkillName: "sk", CreatedAt: 100}

	t.Run("noisy baseline overfire disagreement", func(t *testing.T) {
		var usage []usageRec
		for i := int64(0); i < 10; i++ { // 50% baseline fail rate
			usage = append(usage, mkUsage("sk", i, i%2 == 0))
		}
		for i := int64(101); i < 104; i++ { // 3 straight post-evolve fails
			usage = append(usage, mkUsage("sk", i, false))
		}
		got := backtest(usage, []lifecycleRec{evolve}, 3, 0.05)
		if len(got) != 1 || !got[0].LegacyFired || got[0].EPReject {
			t.Fatalf("want legacy fired + ep quiet (overfire label): %+v", got)
		}
	})

	t.Run("healthy baseline hard regression agreement", func(t *testing.T) {
		var usage []usageRec
		for i := int64(0); i < 10; i++ {
			usage = append(usage, mkUsage("sk", i, true)) // clean baseline → p0 floor
		}
		for i := int64(101); i < 109; i++ {
			usage = append(usage, mkUsage("sk", i, false))
		}
		// Threshold 8 so both deciders resolve on the same evidence (1.475^8≈22≥20).
		got := backtest(usage, []lifecycleRec{evolve}, 8, 0.05)
		if len(got) != 1 || !got[0].LegacyFired || !got[0].EPReject {
			t.Fatalf("want both deciders firing: %+v", got)
		}
	})

	t.Run("horizon truncates at the next evolve of the same skill", func(t *testing.T) {
		usage := []usageRec{mkUsage("sk", 150, false), mkUsage("sk", 250, false)}
		evolves := []lifecycleRec{evolve, {Type: "evolved", SkillName: "sk", CreatedAt: 200}}
		got := backtest(usage, evolves, 3, 0.05)
		if len(got) != 2 || got[0].PostUses != 1 || got[1].PostUses != 1 {
			t.Fatalf("horizon not applied: %+v", got)
		}
	})

	t.Run("synthetic usage sources are excluded", func(t *testing.T) {
		usage := []usageRec{{SkillName: "sk", UsedAt: 150, Success: false, Source: "workout"}}
		if got := backtest(usage, []lifecycleRec{evolve}, 3, 0.05); len(got) != 0 {
			t.Fatalf("synthetic usage leaked into the backtest: %+v", got)
		}
	})
}
