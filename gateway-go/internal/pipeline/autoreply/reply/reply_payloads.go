// reply_payloads.go — Reply payload utilities.
package reply

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
)

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

// DeduplicateReplyPayloads removes duplicate text and media from payloads.
func DeduplicateReplyPayloads(payloads []types.ReplyPayload) []types.ReplyPayload {
	seen := make(map[string]struct{})
	var result []types.ReplyPayload
	for _, p := range payloads {
		key := p.Text
		if key == "" {
			key = p.MediaURL
		}
		if _, ok := seen[key]; key != "" && ok {
			continue
		}
		if key != "" {
			seen[key] = struct{}{}
		}
		result = append(result, p)
	}
	return result
}
