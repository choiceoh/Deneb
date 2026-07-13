package proactive

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// A mail-report body that ends with a ```choices fence (the decision affordance the
// mail-analysis contract now teaches) yields the card body with the fence stripped,
// plus one ActionAnswer chip per option — the plumbing the feed renders as tappable
// answer chips (native WorkFeedAnswerBlock + the desktop parity from #3564).
func TestSplitChoicesFence_MailDecisionCard(t *testing.T) {
	body := "```deneb-ui\n<column><card><text>견적 회신 필요</text></card></column>\n```\n\n" +
		"```choices\n회신 초안 작성\n나중에 알림\n무시\n```"

	cleaned, choices := splitChoicesFence(body)
	if len(choices) != 3 {
		t.Fatalf("expected 3 choices, got %d (%v)", len(choices), choices)
	}
	if choices[0] != "회신 초안 작성" || choices[2] != "무시" {
		t.Fatalf("unexpected choices: %v", choices)
	}
	// The deneb-ui card survives; the raw ```choices fence is stripped from the
	// shown body (it becomes chips, not literal code text).
	if !strings.Contains(cleaned, "견적 회신 필요") {
		t.Fatalf("card body dropped: %q", cleaned)
	}
	if strings.Contains(cleaned, "```choices") || strings.Contains(cleaned, "회신 초안 작성") {
		t.Fatalf("choices fence not stripped from body: %q", cleaned)
	}

	actions := choiceAnswerActions(choices)
	if len(actions) != 3 {
		t.Fatalf("expected 3 answer actions, got %d", len(actions))
	}
	for i, a := range actions {
		if a.Kind != workfeed.ActionAnswer {
			t.Fatalf("action %d kind = %q, want ActionAnswer", i, a.Kind)
		}
		if a.Label != choices[i] || a.Prompt != choices[i] {
			t.Fatalf("action %d label/prompt = %q/%q, want %q", i, a.Label, a.Prompt, choices[i])
		}
	}
}
