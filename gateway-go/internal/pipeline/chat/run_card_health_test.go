package chat

import (
	"log/slog"
	"strings"
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
	deps := runDeps{normalizeCardReply: func(text, sessionKey string, _ *slog.Logger) (string, []string) {
		calls++
		return sessionKey + ":" + text, nil
	}}

	normalizeRunCardReplies(result, RunParams{SessionKey: "client:main"}, deps, slog.Default())

	if result.Text != "client:main:text" || result.AllText != "client:main:text" || result.DeliverableText != "client:main:deliverable" {
		t.Fatalf("not every final field was normalized: %+v", result)
	}
	if calls != 2 {
		t.Fatalf("normalizer calls = %d, want one per unique text", calls)
	}
}

// A rejected card must leave a one-shot correction hint for the next turn,
// otherwise the model re-emits the same broken card (2026-08-25 puppet run).
func TestRejectedCardLeavesASingleCorrectionNotice(t *testing.T) {
	const session = "client:card-reject-test"
	result := &agent.AgentResult{Text: "flattened"}
	deps := runDeps{normalizeCardReply: func(text, sessionKey string, _ *slog.Logger) (string, []string) {
		return text, []string{`$.children[0]: node "text_input" requires a non-empty "id"`}
	}}

	normalizeRunCardReplies(result, RunParams{SessionKey: session}, deps, slog.Default())

	notice := takeCardRejectionNotice(session)
	if !strings.Contains(notice, "text_input") {
		t.Fatalf("notice must carry the reason, got %q", notice)
	}
	if again := takeCardRejectionNotice(session); again != "" {
		t.Fatalf("notice must be consumed on read, got %q", again)
	}
}
