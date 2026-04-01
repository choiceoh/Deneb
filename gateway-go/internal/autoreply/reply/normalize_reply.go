package reply

import (
	"regexp"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/tokens"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// toolCallTagRe matches <tool_call>...</tool_call> blocks (some models emit
// this variant without the <function=...> wrapper).
var toolCallTagRe = regexp.MustCompile(`(?s)<tool_call>.*?</tool_call>`)

// jsonToolCallRe matches JSON-style tool call blocks that some models emit as
// text: {"name": "tool_name", "arguments": {...}} or {"type": "function", ...}.
var jsonToolCallRe = regexp.MustCompile(`(?s)\{"(?:name|type)":\s*"(?:function|tool_use|[a-z_]+)"[^}]*"(?:arguments|input|parameters)":\s*\{[^}]*\}\s*\}`)

// pipeFunctionRe matches model-specific function call tokens like
// <|python_tag|>..., <|function|>..., or similar special tokens.
var pipeFunctionRe = regexp.MustCompile(`<\|(?:python_tag|function|tool_call)\|>[^\n]*(?:\n|$)`)

// StripLeakedToolCallMarkup removes leaked tool-call envelope text that should
// stay internal. Handles multiple model-specific formats:
//   - Llama-style: <function=name>...<arg_key>...<arg_value>...</tool_call>
//   - XML-style: <tool_call>...</tool_call>
//   - JSON-style: {"name": "tool_name", "arguments": {...}}
//   - Special tokens: <|python_tag|>, <|function|>, <|tool_call|>
func StripLeakedToolCallMarkup(text string) string {
	trimmed := text

	// Strip Llama-style <function=name>...</tool_call> blocks.
	for {
		start := strings.Index(trimmed, "<function=")
		if start < 0 {
			break
		}
		end := strings.Index(trimmed[start:], "</tool_call>")
		if end < 0 {
			break
		}
		end += start + len("</tool_call>")
		trimmed = strings.TrimSpace(trimmed[:start] + "\n" + trimmed[end:])
	}

	// Strip <tool_call>...</tool_call> blocks.
	trimmed = toolCallTagRe.ReplaceAllString(trimmed, "")

	// Strip JSON-style tool call blocks.
	trimmed = jsonToolCallRe.ReplaceAllString(trimmed, "")

	// Strip model-specific special tokens.
	trimmed = pipeFunctionRe.ReplaceAllString(trimmed, "")

	return strings.TrimSpace(trimmed)
}

// NormalizeReplyPayload cleans up a reply payload before delivery:
// - Filters empty/silent/heartbeat replies
// - Strips silent tokens
// - Applies response prefix templates
func NormalizeReplyPayload(payload types.ReplyPayload, opts NormalizeOpts) (types.ReplyPayload, bool) {
	text := strings.TrimSpace(payload.Text)
	text = StripLeakedToolCallMarkup(text)

	// Check for silent reply.
	if tokens.IsSilentReplyText(text, "") {
		return payload, false // skip delivery
	}

	// Strip trailing silent token from mixed content.
	text = tokens.StripSilentToken(text, "")

	// Handle heartbeat token in the text.
	if strings.Contains(text, tokens.HeartbeatToken) {
		result := tokens.StripHeartbeatToken(text, opts.HeartbeatMode, opts.HeartbeatAckMaxChars)
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
	HeartbeatMode        tokens.StripHeartbeatMode
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
