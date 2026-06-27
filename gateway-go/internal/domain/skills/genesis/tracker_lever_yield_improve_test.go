package genesis

import "testing"

// oldRuleFlags replicates the previous raw Committed/ConfirmRate gate, so the
// test can show exactly where the refined rule diverges.
func oldRuleFlags(y LeverYield, minShips int, maxRate float64) bool {
	rate := 0.0
	if y.Committed > 0 {
		rate = float64(y.Confirmed) / float64(y.Committed)
	}
	return y.Committed >= minShips && rate <= maxRate
}

func TestFilterLowYieldLevers_ResolvedAndSmoothed(t *testing.T) {
	levers := []LeverYield{
		// Good lever whose evolves are mostly still PENDING. Raw rate 3/8=0.375
		// wrongly flags it; the resolved view (3 confirmed, 0 reverts) keeps it.
		{Signature: "pending-good", Committed: 8, Confirmed: 3, RolledBack: 0},
		// Genuinely bad: resolved 5, four reverts. Both rules flag.
		{Signature: "reverted-bad", Committed: 6, Confirmed: 1, RolledBack: 4},
		// Only one resolved outcome (a single revert) amid pending — too little
		// evidence to avoid yet; the refined rule waits.
		{Signature: "thin-evidence", Committed: 5, Confirmed: 0, RolledBack: 1},
		// Strong lever: 8 confirmed, 1 revert. Neither rule flags.
		{Signature: "good", Committed: 10, Confirmed: 8, RolledBack: 1},
	}

	flagged := map[string]bool{}
	for _, y := range filterLowYieldLevers(levers, 3, 0.4) {
		flagged[y.Signature] = true
	}

	// Refined rule flags only the resolved-bad one.
	if got := keysOf(flagged); len(got) != 1 || !flagged["reverted-bad"] {
		t.Fatalf("refined rule should flag only {reverted-bad}, got %v", got)
	}

	// The pending-good lever is the headline fix: the OLD rule avoided it.
	if !oldRuleFlags(levers[0], 3, 0.4) {
		t.Fatal("precondition: old rule should have flagged pending-good (3/8=0.375)")
	}
	if flagged["pending-good"] {
		t.Error("refined rule must NOT avoid a lever that is 3/3 on resolved outcomes")
	}

	// thin-evidence: one revert, four pending → not enough resolved to avoid.
	if flagged["thin-evidence"] {
		t.Error("refined rule should wait on a single resolved outcome")
	}

	t.Logf("old-rule flags:     pending-good=%v reverted-bad=%v thin=%v good=%v",
		oldRuleFlags(levers[0], 3, 0.4), oldRuleFlags(levers[1], 3, 0.4),
		oldRuleFlags(levers[2], 3, 0.4), oldRuleFlags(levers[3], 3, 0.4))
	t.Logf("refined-rule flags: %v", keysOf(flagged))
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	return out
}
