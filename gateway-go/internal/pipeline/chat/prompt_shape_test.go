package chat

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

// TestFinalizePromptSeparatesAbsentFromDiscarded is the whole point of the
// outcome type: a turn with no memory block and a turn whose memory block was
// built and then thrown away produce identical prompts, and must not produce
// identical readings.
func TestFinalizePromptSeparatesAbsentFromDiscarded(t *testing.T) {
	base := json.RawMessage(`"` + strings.Repeat("가", 4000) + `"`)

	// No tier-1 was ever built — nothing to report.
	_, absent := finalizePrompt(base, "", "", ContextConfig{SystemPromptBudget: 100}, "", "", engineRoute{}, nil)
	if absent.tier1Dropped() || absent.tier1Shrunk() {
		t.Errorf("absent tier-1 must not read as a loss: %+v", absent)
	}

	// Tier-1 was built, but the static prompt already exhausted the budget.
	_, discarded := finalizePrompt(base, "", "기억 블록", ContextConfig{SystemPromptBudget: 100}, "", "", engineRoute{}, nil)
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
		ContextConfig{SystemPromptBudget: 100_000}, "", "", engineRoute{}, nil)
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

// TestLogPromptShapeMarksOnlyTheShrinkAdvisory: the two verdicts route to
// different readers. A bounded shrink is the budget guard working, so it
// carries the author marker that keeps genesis' runtime-error miner from
// filing it as a code defect; a full drop is a degraded answer and must stay
// mineable. Asserted through observe.IsAdvisory — the same predicate the miner
// runs — so rewording the line without the marker fails here.
func TestLogPromptShapeMarksOnlyTheShrinkAdvisory(t *testing.T) {
	capture := func(o promptBudgetOutcome) string {
		var buf bytes.Buffer
		logPromptShape(slog.New(slog.NewTextHandler(&buf, nil)), o, "s1")
		return buf.String()
	}

	shrunk := capture(promptBudgetOutcome{Tier1RequestedTokens: 10, Tier1AdmittedTokens: 5})
	if !strings.Contains(shrunk, "축소") {
		t.Fatalf("shrink must be logged: %q", shrunk)
	}
	if !observe.IsAdvisory(observe.LogLine{Msg: shrunk}) {
		t.Errorf("a bounded shrink must carry the advisory marker: %q", shrunk)
	}

	dropped := capture(promptBudgetOutcome{Tier1RequestedTokens: 10})
	if !strings.Contains(dropped, "누락") {
		t.Fatalf("drop must be logged: %q", dropped)
	}
	if observe.IsAdvisory(observe.LogLine{Msg: dropped}) {
		t.Errorf("a dropped block is a real loss and must stay mineable: %q", dropped)
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
