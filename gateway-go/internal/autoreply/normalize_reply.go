package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"strings"
)

// NormalizeReplyPayload cleans up a reply payload before delivery:
// - Filters empty/silent/heartbeat replies
// - Strips silent tokens
// - Applies response prefix templates
func NormalizeReplyPayload(payload types.ReplyPayload, opts NormalizeOpts) (types.ReplyPayload, bool) {
	text := strings.TrimSpace(payload.Text)

	// Check for silent reply.
	if IsSilentReplyText(text, "") {
		return payload, false // skip delivery
	}

	// Strip trailing silent token from mixed content.
	text = StripSilentToken(text, "")

	// Handle heartbeat token in the text.
	if strings.Contains(text, HeartbeatToken) {
		result := StripHeartbeatToken(text, opts.HeartbeatMode, opts.HeartbeatAckMaxChars)
		if result.ShouldSkip {
			return payload, false
		}
		text = result.Text
	}

	// Apply response prefix template.
	if opts.ResponsePrefix != "" && text != "" {
		text = opts.ResponsePrefix + text
	}

	// Skip empty text replies with no media.
	if text == "" && payload.MediaURL == "" && len(payload.MediaURLs) == 0 {
		return payload, false
	}

	payload.Text = text
	return payload, true
}

// NormalizeOpts configures reply normalization.
type NormalizeOpts struct {
	ResponsePrefix       string
	HeartbeatMode        StripHeartbeatMode
	HeartbeatAckMaxChars int
}

// FilterReplyPayloads normalizes a slice of payloads, removing those that
// should be skipped.
func FilterReplyPayloads(payloads []types.ReplyPayload, opts NormalizeOpts) []types.ReplyPayload {
	var result []types.ReplyPayload
	for _, p := range payloads {
		normalized, ok := NormalizeReplyPayload(p, opts)
		if ok {
			result = append(result, normalized)
		}
	}
	return result
}

// DeduplicateReplyPayloads removes duplicate text and media from payloads.
func DeduplicateReplyPayloads(payloads []types.ReplyPayload) []types.ReplyPayload {
	seen := make(map[string]bool)
	var result []types.ReplyPayload
	for _, p := range payloads {
		key := p.Text
		if key == "" {
			key = p.MediaURL
		}
		if key != "" && seen[key] {
			continue
		}
		if key != "" {
			seen[key] = true
		}
		result = append(result, p)
	}
	return result
}
