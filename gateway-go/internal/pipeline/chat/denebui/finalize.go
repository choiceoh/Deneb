package denebui

import (
	"log/slog"
	"strings"
)

const invalidCardFallback = "구조화된 카드 내용을 표시할 수 없어 평문으로 전환했습니다."

// NormalizeFinalReply is the last server-side boundary before an assistant
// reply is persisted or delivered. It preserves one valid deneb-ui block and
// converts invalid or additional blocks to readable plain text so raw broken
// markup never becomes the user's final answer.
func NormalizeFinalReply(text, sessionKey string, logger *slog.Logger) string {
	if text == "" || !HasFence(text) {
		return text
	}
	if logger == nil {
		logger = slog.Default()
	}

	block := 0
	normalized := ReplaceFences(text, func(body string) string {
		block++
		issues, err := Validate(body)
		if block == 1 && err == nil && len(issues) == 0 {
			return "```" + FenceInfo + "\n" + strings.TrimSpace(body) + "\n```"
		}

		reason := "schema_issues"
		attrs := []any{"session", sessionKey, "block", block - 1}
		switch {
		case block > 1:
			reason = "additional_block"
		case err != nil:
			reason = "unparseable"
			attrs = append(attrs, "error", err)
		case len(issues) > 0:
			attrs = append(attrs, "issueCount", len(issues), "firstIssue", issues[0].String())
		}
		attrs = append(attrs, "reason", reason)
		logger.Warn("deneb-ui card rejected before delivery", attrs...)

		if plain := strings.TrimSpace(PlainText(body)); plain != "" {
			return plain
		}
		return invalidCardFallback
	})
	return strings.TrimSpace(normalized)
}
