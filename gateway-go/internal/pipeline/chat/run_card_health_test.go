package chat

import (
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
)

func TestNormalizeRunCardRepliesCoversEveryFinalTextField(t *testing.T) {
	t.Parallel()
	result := &agent.AgentResult{
		Text:            "text",
		AllText:         "text",
		DeliverableText: "deliverable",
	}
	calls := 0
	deps := runDeps{normalizeCardReply: func(text, sessionKey string, _ *slog.Logger) string {
		calls++
		return sessionKey + ":" + text
	}}

	normalizeRunCardReplies(result, RunParams{SessionKey: "client:main"}, deps, slog.Default())

	if result.Text != "client:main:text" || result.AllText != "client:main:text" || result.DeliverableText != "client:main:deliverable" {
		t.Fatalf("not every final field was normalized: %+v", result)
	}
	if calls != 2 {
		t.Fatalf("normalizer calls = %d, want one per unique text", calls)
	}
}
