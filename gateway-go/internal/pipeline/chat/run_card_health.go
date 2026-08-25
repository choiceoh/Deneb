package chat

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
)

// Card finalization is injected via HandlerConfig.NormalizeCardReply (server
// wires denebui.NormalizeFinalReply). Keeping the callback here avoids a
// chat-parent import of denebui while enforcing one boundary for every entry
// path before persistence or delivery.
func normalizeRunCardReplies(result *agent.AgentResult, params RunParams, deps runDeps, logger *slog.Logger) {
	if result == nil || deps.normalizeCardReply == nil {
		return
	}
	// AgentResult fields often share the same final text. Normalize each unique
	// value once so validation and rejection logs are not duplicated.
	normalized := make(map[string]string, 3)
	normalize := func(text string) string {
		if value, ok := normalized[text]; ok {
			return value
		}
		value, rejections := deps.normalizeCardReply(text, params.SessionKey, logger)
		normalized[text] = value
		// One correction hint per turn: the first rejection is the one the
		// author has to fix before the rest can even be judged.
		if len(rejections) > 0 {
			recordCardRejection(params.SessionKey, "schema_issues", rejections[0])
		}
		return value
	}
	result.Text = normalize(result.Text)
	result.AllText = normalize(result.AllText)
	result.DeliverableText = normalize(result.DeliverableText)
}
