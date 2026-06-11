// reply_payloads.go — Reply payload utilities.
package reply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
)

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
