// reply_payloads.go — Reply payload processing, dedup, threading, and delivery.
// Mirrors src/auto-reply/reply/reply-payloads.ts (274 LOC),
// reply-delivery.ts (134 LOC), route-reply.ts (225 LOC),
// session-delivery.ts (216 LOC).
package reply

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/tokens"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// FilterMessagingToolDuplicates removes payloads whose text was already sent
// by a messaging tool during the agent turn.
func FilterMessagingToolDuplicates(payloads []types.ReplyPayload, sentTexts []string) []types.ReplyPayload {
	if len(sentTexts) == 0 || len(payloads) == 0 {
		return payloads
	}

	sentSet := make(map[string]bool, len(sentTexts))
	for _, t := range sentTexts {
		trimmed := strings.TrimSpace(t)
		if trimmed != "" {
			sentSet[trimmed] = true
		}
	}

	var filtered []types.ReplyPayload
	for _, p := range payloads {
		text := strings.TrimSpace(p.Text)
		if text != "" && sentSet[text] {
			continue // already sent by messaging tool
		}
		filtered = append(filtered, p)
	}
	return filtered
}

// FilterMessagingToolMediaDuplicates removes media URLs already sent by messaging tools.
func FilterMessagingToolMediaDuplicates(payloads []types.ReplyPayload, sentMediaURLs []string) []types.ReplyPayload {
	if len(sentMediaURLs) == 0 || len(payloads) == 0 {
		return payloads
	}

	sentSet := make(map[string]bool, len(sentMediaURLs))
	for _, url := range sentMediaURLs {
		trimmed := strings.TrimSpace(url)
		if trimmed != "" {
			sentSet[trimmed] = true
		}
	}

	var filtered []types.ReplyPayload
	for _, p := range payloads {
		if p.MediaURL != "" && sentSet[strings.TrimSpace(p.MediaURL)] {
			// Remove the duplicate media but keep the payload if it has text.
			if p.Text != "" {
				p.MediaURL = ""
				p.MediaURLs = nil
				filtered = append(filtered, p)
			}
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered
}

// IsRenderablePayload returns true if the payload has content worth delivering.
func IsRenderablePayload(p types.ReplyPayload) bool {
	if strings.TrimSpace(p.Text) != "" {
		return true
	}
	if p.MediaURL != "" || len(p.MediaURLs) > 0 {
		return true
	}
	if len(p.ChannelData) > 0 {
		return true
	}
	return false
}

// ShouldSuppressMessagingToolReplies returns true if the messaging tool already
// delivered to the same target, so the final reply should be suppressed.
func ShouldSuppressMessagingToolReplies(messageProvider string, sentTargets []types.MessagingToolTarget, originTo, accountID string) bool {
	if len(sentTargets) == 0 || originTo == "" {
		return false
	}
	for _, target := range sentTargets {
		normalizedTo := strings.TrimSpace(target.To)
		normalizedOrigin := strings.TrimSpace(originTo)
		if normalizedTo == normalizedOrigin {
			if target.AccountID != "" && accountID != "" && target.AccountID != accountID {
				continue
			}
			return true
		}
	}
	return false
}

// FormatBtwTextForExternalDelivery wraps BTW (side question) text for delivery.
func FormatBtwTextForExternalDelivery(question, answer string) string {
	if answer == "" {
		return ""
	}
	if question != "" {
		return "💬 " + question + "\n\n" + answer
	}
	return answer
}

// NormalizeReplyPayloadDirectives processes [[tag]] directives in reply text.
func NormalizeReplyPayloadDirectives(payload types.ReplyPayload, currentMessageID, silentToken string) types.ReplyPayload {
	if payload.Text == "" {
		return payload
	}

	// Check for [[silent]] tag.
	if tokens.HasReplyTag(payload.Text, "silent") || tokens.HasReplyTag(payload.Text, "no_reply") {
		return types.ReplyPayload{} // suppress
	}

	// Strip all tags from output text.
	cleaned := tokens.StripReplyTags(payload.Text)

	// Handle reply threading tags.
	replyTo, replyToCurrent := tokens.ApplyReplyThreading(payload.Text, "")
	if replyToCurrent && currentMessageID != "" {
		payload.ReplyToID = currentMessageID
	} else if replyTo != "" {
		payload.ReplyToID = replyTo
	}

	payload.Text = cleaned
	return payload
}

// BuildReplyPayloads processes the raw payloads from an agent turn into
// deliverable reply payloads. Handles heartbeat stripping, dedup, threading,
// and messaging tool suppression.
func BuildReplyPayloads(params types.BuildReplyPayloadsParams) []types.ReplyPayload {
	payloads := params.Payloads

	// 1. Strip heartbeat tokens from non-heartbeat replies.
	if !params.IsHeartbeat {
		var sanitized []types.ReplyPayload
		for _, p := range payloads {
			if p.Text != "" && strings.Contains(p.Text, tokens.HeartbeatToken) {
				stripped := tokens.StripHeartbeatToken(p.Text, tokens.StripModeMessage, 0)
				if stripped.ShouldSkip && p.MediaURL == "" && len(p.MediaURLs) == 0 {
					continue
				}
				p.Text = stripped.Text
			}
			if tokens.IsSilentReplyText(p.Text, "") {
				continue
			}
			sanitized = append(sanitized, p)
		}
		payloads = sanitized
	}

	// 2. Apply reply threading.
	for i := range payloads {
		payloads[i].Text = StripLeakedToolCallMarkup(payloads[i].Text)
		payloads[i] = NormalizeReplyPayloadDirectives(payloads[i], params.CurrentMessageID, tokens.SilentReplyToken)
	}

	// 3. Filter non-renderable.
	var renderable []types.ReplyPayload
	for _, p := range payloads {
		if IsRenderablePayload(p) {
			renderable = append(renderable, p)
		}
	}
	payloads = renderable

	// 4. Dedup against messaging tool sends.
	if len(params.SentTexts) > 0 {
		payloads = FilterMessagingToolDuplicates(payloads, params.SentTexts)
	}
	if len(params.SentMediaURLs) > 0 {
		payloads = FilterMessagingToolMediaDuplicates(payloads, params.SentMediaURLs)
	}

	// 5. Suppress if messaging tool already delivered to same target.
	if ShouldSuppressMessagingToolReplies(params.MessageProvider, params.SentTargets, params.OriginTo, params.AccountID) {
		return nil
	}

	return payloads
}
