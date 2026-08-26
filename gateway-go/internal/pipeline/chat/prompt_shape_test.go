package chat

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestFinalizePromptSeparatesAbsentFromDiscarded is the whole point of the
// outcome type: a turn with no memory block and a turn whose memory block was
// built and then thrown away produce identical prompts, and must not produce
// identical readings.
func TestFinalizePromptSeparatesAbsentFromDiscarded(t *testing.T) {
	base := json.RawMessage(`"` + strings.Repeat("가", 4000) + `"`)

	// No tier-1 was ever built — nothing to report.
	_, absent := finalizePrompt(base, "", "", ContextConfig{SystemPromptBudget: 100}, "", "")
	if absent.tier1Dropped() || absent.tier1Shrunk() {
		t.Errorf("absent tier-1 must not read as a loss: %+v", absent)
	}

	// Tier-1 was built, but the static prompt already exhausted the budget.
	_, discarded := finalizePrompt(base, "", "기억 블록", ContextConfig{SystemPromptBudget: 100}, "", "")
	if !discarded.tier1Dropped() {
		t.Fatalf("built-then-discarded tier-1 must read as dropped: %+v", discarded)
	}
	if !discarded.starvedByStaticPrompt() {
		t.Errorf("budget exhausted by the static prompt must name that cause: %+v", discarded)
	}
	if discarded.Tier1RequestedTokens == 0 {
		t.Error("a dropped block must still report what was asked for — otherwise the loss has no size")
	}
}

// TestFinalizePromptReportsIntactAdmission: an addition that fits reports no
// loss, so the Warn path stays quiet on the healthy majority of turns.
func TestFinalizePromptReportsIntactAdmission(t *testing.T) {
	prompt, outcome := finalizePrompt(json.RawMessage(`"짧은 프롬프트"`), "", "기억 블록",
		ContextConfig{SystemPromptBudget: 100_000}, "", "")
	if outcome.tier1Dropped() || outcome.tier1Shrunk() {
		t.Errorf("an addition that fits must report no loss: %+v", outcome)
	}
	if outcome.Tier1AdmittedTokens == 0 {
		t.Error("an admitted block must report a non-zero admitted size")
	}
	if !strings.Contains(string(prompt), "기억 블록") {
		t.Error("the admitted block must actually be in the prompt")
	}
}

// TestPromptShapeVerdictsAreMutuallyExclusive pins that no single outcome reads
// as both dropped and shrunk — the two verdicts route to different fixes.
func TestPromptShapeVerdictsAreMutuallyExclusive(t *testing.T) {
	cases := []promptBudgetOutcome{
		{},
		{Tier1RequestedTokens: 10},
		{Tier1RequestedTokens: 10, Tier1AdmittedTokens: 5},
		{Tier1RequestedTokens: 10, Tier1AdmittedTokens: 10},
	}
	for _, o := range cases {
		if o.tier1Dropped() && o.tier1Shrunk() {
			t.Errorf("outcome %+v reads as both dropped and shrunk", o)
		}
	}
}
