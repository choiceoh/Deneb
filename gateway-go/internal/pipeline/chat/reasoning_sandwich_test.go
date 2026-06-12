package chat

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestBoostThinkingBudget(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 1024},      // below ladder → first tier
		{1024, 4096},   // minimal → low
		{4096, 10240},  // low → medium
		{10240, 32768}, // medium → high
		{16384, 32768}, // adaptive → high
		{32768, 65536}, // high → xhigh
		{65536, 65536}, // xhigh → capped
		{99999, 99999}, // above top → unchanged
	}
	for _, c := range cases {
		if got := boostThinkingBudget(c.in); got != c.want {
			t.Errorf("boostThinkingBudget(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestPlanningSandwichThinking(t *testing.T) {
	const bigMaxTokens = 200000 // plenty of headroom: boost always applies

	// nil base → nil selector (thinking disabled, leave as-is).
	if planningSandwichThinking(nil, bigMaxTokens) != nil {
		t.Fatal("planningSandwichThinking(nil) should be nil")
	}

	base := &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 10240, Interleaved: true}
	sel := planningSandwichThinking(base, bigMaxTokens)
	if sel == nil {
		t.Fatal("selector should be non-nil for enabled thinking")
	}

	// Turn 0 (planning) is boosted one tier; later turns use the baseline.
	turn0 := sel(0, nil)
	if turn0.BudgetTokens != 32768 {
		t.Errorf("turn 0 budget = %d, want 32768 (boosted from 10240)", turn0.BudgetTokens)
	}
	if !turn0.Interleaved || turn0.Type != "enabled" {
		t.Errorf("turn 0 should preserve Type/Interleaved, got %+v", turn0)
	}
	for _, turn := range []int{1, 2, 5} {
		if got := sel(turn, nil); got.BudgetTokens != base.BudgetTokens {
			t.Errorf("turn %d budget = %d, want baseline %d", turn, got.BudgetTokens, base.BudgetTokens)
		}
	}

	// The baseline must not be mutated by the boost.
	if base.BudgetTokens != 10240 {
		t.Errorf("base budget mutated to %d, want 10240", base.BudgetTokens)
	}
}

// TestPlanningSandwichThinking_ClampsToMaxTokens verifies the planning boost is
// dropped when the larger budget would not leave response headroom under
// max_tokens (Anthropic requires budget_tokens < max_tokens) — preventing a
// rejected first turn.
func TestPlanningSandwichThinking_ClampsToMaxTokens(t *testing.T) {
	base := &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 10240}

	// boost target is 32768; with maxTokens 32768 there is no headroom
	// (32768 > 32768-4096), so turn 0 must fall back to the baseline.
	sel := planningSandwichThinking(base, 32768)
	if got := sel(0, nil).BudgetTokens; got != 10240 {
		t.Errorf("turn 0 budget = %d, want 10240 (boost dropped under tight max_tokens)", got)
	}

	// With ample max_tokens the boost applies.
	sel = planningSandwichThinking(base, 65536)
	if got := sel(0, nil).BudgetTokens; got != 32768 {
		t.Errorf("turn 0 budget = %d, want 32768 (boost should apply with headroom)", got)
	}

	// maxTokens <= 0 means unknown → keep the boost.
	sel = planningSandwichThinking(base, 0)
	if got := sel(0, nil).BudgetTokens; got != 32768 {
		t.Errorf("turn 0 budget = %d, want 32768 (unknown max_tokens keeps boost)", got)
	}
}
