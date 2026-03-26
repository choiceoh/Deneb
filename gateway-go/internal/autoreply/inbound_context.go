package autoreply

import (
	"strings"
)

// FinalizeInboundContextOptions controls which fields are force-overridden
// during inbound context finalization.
type FinalizeInboundContextOptions struct {
	ForceBodyForAgent      bool
	ForceBodyForCommands   bool
	ForceChatType          bool
	ForceConversationLabel bool
}

// DefaultMediaType is used when no media type is provided.
const DefaultMediaType = "application/octet-stream"

// FinalizeInboundContextFull normalizes and validates all inbound context fields
// with the full priority chain logic from the TypeScript codebase.
//
// Mirrors src/auto-reply/reply/inbound-context.ts finalizeInboundContext().
func FinalizeInboundContextFull(ctx *MsgContext, opts FinalizeInboundContextOptions) {
	if ctx == nil {
		return
	}

	// 1. Text normalization: sanitize system tags and normalize newlines.
	ctx.Body = sanitizeInboundText(ctx.Body)
	ctx.RawBody = normalizeOptionalTextField(ctx.RawBody)
	ctx.CommandBody = normalizeOptionalTextField(ctx.CommandBody)

	// 2. Chat type normalization.
	chatType := normalizeChatType(ctx.ChatType)
	if chatType != "" && (opts.ForceChatType || ctx.ChatType != chatType) {
		ctx.ChatType = chatType
	}
	if ctx.ChatType == "" {
		if ctx.IsGroup {
			ctx.ChatType = "group"
		} else {
			ctx.ChatType = "direct"
		}
	}

	// 3. BodyForAgent priority chain.
	if opts.ForceBodyForAgent {
		ctx.BodyForAgent = sanitizeInboundText(ctx.Body)
	} else {
		source := firstNonEmpty(ctx.BodyForAgent, ctx.CommandBody, ctx.RawBody, ctx.Body)
		ctx.BodyForAgent = sanitizeInboundText(source)
	}

	// 4. BodyForCommands priority chain.
	if opts.ForceBodyForCommands {
		source := firstNonEmpty(ctx.CommandBody, ctx.RawBody, ctx.Body)
		ctx.BodyForCommands = sanitizeInboundText(source)
	} else {
		source := firstNonEmpty(ctx.BodyForCommands, ctx.CommandBody, ctx.RawBody, ctx.Body)
		ctx.BodyForCommands = sanitizeInboundText(source)
	}

	// 5. Command authorization: default-deny when upstream forgets.
	// ctx.CommandAuthorized stays as-is (Go zero value is false = deny).

	// 6. Media type alignment.
	mediaCount := countMediaEntries(ctx)
	if mediaCount > 0 {
		mediaType := normalizeMediaType(ctx.MediaType)
		if mediaType == "" {
			mediaType = DefaultMediaType
		}
		ctx.MediaType = mediaType
	}
}

// normalizeOptionalTextField sanitizes text if non-empty.
func normalizeOptionalTextField(value string) string {
	if value == "" {
		return ""
	}
	return sanitizeInboundText(value)
}

// sanitizeInboundText normalizes newlines and strips system tags from text.
func sanitizeInboundText(text string) string {
	if text == "" {
		return ""
	}
	// Normalize Windows line endings.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	// Strip system-reminder tags that could be injected.
	text = stripSystemTags(text)
	return text
}

// stripSystemTags removes <system-reminder> tags from text to prevent injection.
func stripSystemTags(text string) string {
	// Remove opening and closing system-reminder tags.
	result := text
	for {
		lower := strings.ToLower(result)
		start := strings.Index(lower, "<system-reminder>")
		if start == -1 {
			break
		}
		end := strings.Index(lower[start:], "</system-reminder>")
		if end == -1 {
			// Remove unclosed tag.
			result = result[:start]
			break
		}
		result = result[:start] + result[start+end+len("</system-reminder>"):]
	}
	return result
}

// normalizeChatType normalizes a chat type string.
func normalizeChatType(chatType string) string {
	switch strings.ToLower(strings.TrimSpace(chatType)) {
	case "direct", "dm", "private":
		return "direct"
	case "group":
		return "group"
	case "supergroup":
		return "supergroup"
	case "channel":
		return "channel"
	default:
		return ""
	}
}

// normalizeMediaType trims and validates a media type string.
func normalizeMediaType(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return trimmed
}

// countMediaEntries counts the total media entries across paths and URLs.
func countMediaEntries(ctx *MsgContext) int {
	count := 0
	if ctx.MediaPath != "" {
		count = 1
	}
	return count
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
