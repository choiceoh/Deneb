package denebui

import (
	"log/slog"
	"strings"
)

const invalidCardFallback = "구조화된 카드 내용을 표시할 수 없어 평문으로 전환했습니다."

// Rejection describes one deneb-ui block that failed the delivery boundary.
// The reason travels back to the model on its next turn (chat's tail
// injection): a rejection today is invisible to the author — the card silently
// becomes plain text — so the same broken card is re-emitted turn after turn.
// Observed 2026-08-25 in puppet mode: an <input> without an id downgraded the
// whole card and nothing in the conversation said why.
type Rejection struct {
	Block  int    // 0-based fence index within the reply
	Reason string // schema_issues | unparseable | additional_block
	Issue  string // first validation issue, empty for additional_block
}

// NormalizeFinalReply is the last server-side boundary before an assistant
// reply is persisted or delivered. It preserves one valid deneb-ui block and
// converts invalid or additional blocks to readable plain text so raw broken
// markup never becomes the user's final answer.
func NormalizeFinalReply(text, sessionKey string, logger *slog.Logger) string {
	normalized, _ := NormalizeFinalReplyWithRejections(text, sessionKey, logger)
	return normalized
}

// NormalizeFinalReplyWithRejections is NormalizeFinalReply plus the rejections
// it had to make, so the caller can tell the model what went wrong.
func NormalizeFinalReplyWithRejections(text, sessionKey string, logger *slog.Logger) (string, []Rejection) {
	if text == "" {
		return text, nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	// deneb-html answers first: their bodies must never contain backticks per
	// contract, so running the (lenient) deneb-ui scan afterwards cannot split
	// a kept HTML document.
	text = normalizeHTMLAnswers(text, sessionKey, logger)
	if repairedText, glitched := RepairFenceGlitches(text); glitched {
		logger.Warn("deneb-ui fence glitch repaired", "session", sessionKey)
		text = repairedText
	}
	if !HasFence(text) {
		return text, nil
	}

	var rejections []Rejection
	block := 0
	normalized := ReplaceFences(text, func(body string) string {
		block++
		issues, err := Validate(body)
		if block == 1 && err == nil && allRecoverable(issues) {
			// Content-preserving issues (unknown tags the parser already
			// unwrapped) deliver as a card — every client parser unwraps them
			// identically, and a full plain-text downgrade would lose far more.
			// Logged so drift telemetry keeps seeing the invented tags.
			if len(issues) > 0 {
				logger.Info("deneb-ui card delivered with recoverable issues",
					"session", sessionKey, "block", block-1,
					"issueCount", len(issues), "firstIssue", issues[0].String())
			}
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
		rejected := Rejection{Block: block - 1, Reason: reason}
		if len(issues) > 0 {
			rejected.Issue = issues[0].String()
		} else if err != nil {
			rejected.Issue = err.Error()
		}
		rejections = append(rejections, rejected)

		if plain := strings.TrimSpace(PlainText(body)); plain != "" {
			return plain
		}
		return invalidCardFallback
	})
	return strings.TrimSpace(normalized), rejections
}

func allRecoverable(issues []Issue) bool {
	for _, is := range issues {
		if !is.Recoverable() {
			return false
		}
	}
	return true
}
