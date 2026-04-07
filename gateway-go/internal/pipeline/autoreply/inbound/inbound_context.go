package inbound

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"regexp"
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
func FinalizeInboundContextFull(ctx *types.MsgContext, opts FinalizeInboundContextOptions) {
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

	// 6. Media type alignment: ensure MediaType is set and MediaTypes
	// is padded to match MediaPaths/MediaUrls length.
	mediaCount := countMediaEntries(ctx)
	if mediaCount > 0 {
		mediaType := normalizeMediaType(ctx.MediaType)
		rawMediaTypes := ctx.MediaTypes

		var mediaTypesFinal []string
		if len(rawMediaTypes) > 0 {
			// Pad existing array to match media count.
			filled := make([]string, len(rawMediaTypes))
			copy(filled, rawMediaTypes)
			for len(filled) < mediaCount {
				filled = append(filled, "")
			}
			mediaTypesFinal = make([]string, len(filled))
			for i, entry := range filled {
				normalized := normalizeMediaType(entry)
				if normalized == "" {
					normalized = DefaultMediaType
				}
				mediaTypesFinal[i] = normalized
			}
		} else if mediaType != "" {
			// Broadcast single type across all entries.
			mediaTypesFinal = make([]string, mediaCount)
			mediaTypesFinal[0] = mediaType
			for i := 1; i < mediaCount; i++ {
				mediaTypesFinal[i] = DefaultMediaType
			}
		} else {
			// Fill with default.
			mediaTypesFinal = make([]string, mediaCount)
			for i := range mediaTypesFinal {
				mediaTypesFinal[i] = DefaultMediaType
			}
		}

		ctx.MediaTypes = mediaTypesFinal
		if mediaType == "" && len(mediaTypesFinal) > 0 {
			ctx.MediaType = mediaTypesFinal[0]
		} else if mediaType != "" {
			ctx.MediaType = mediaType
		} else {
			ctx.MediaType = DefaultMediaType
		}
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

// Regex patterns for system tag neutralization.
// Matches src/auto-reply/reply/inbound-text.ts sanitizeInboundSystemTags().
var (
	// Neutralizes bracketed system tags like [System Message], [Assistant], [Internal].
	bracketedSystemTagRe = regexp.MustCompile(`(?i)\[\s*(System\s*Message|System|Assistant|Internal)\s*\]`)
	// Neutralizes line-prefixed "System:" patterns.
	lineSystemPrefixRe = regexp.MustCompile(`(?mi)^(\s*)System:(?:\s|$)`)
	// Strips <system-reminder>...</system-reminder> blocks entirely.
	systemReminderTagRe = regexp.MustCompile(`(?is)<system-reminder>.*?</system-reminder>`)
	// Strips unclosed <system-reminder> tags.
	systemReminderOpenRe = regexp.MustCompile(`(?i)<system-reminder>[\s\S]*$`)
)

// stripSystemTags removes system-level injection vectors from user text.
// Three-pass sanitization:
// 1. Remove <system-reminder>...</system-reminder> blocks entirely
// 2. Replace bracketed tags [System Message] → (System Message)
// 3. Replace line-prefixed "System:" → "System (untrusted):"
func stripSystemTags(text string) string {
	// Pass 1: Remove complete system-reminder blocks.
	result := systemReminderTagRe.ReplaceAllString(text, "")
	// Pass 1b: Remove unclosed system-reminder tags.
	result = systemReminderOpenRe.ReplaceAllString(result, "")
	// Pass 2: Neutralize bracketed system tags → parenthesized.
	result = bracketedSystemTagRe.ReplaceAllStringFunc(result, func(match string) string {
		inner := bracketedSystemTagRe.FindStringSubmatch(match)
		if len(inner) > 1 {
			return "(" + inner[1] + ")"
		}
		return match
	})
	// Pass 3: Neutralize line-prefixed "System:".
	result = lineSystemPrefixRe.ReplaceAllString(result, "${1}System (untrusted):")
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
func countMediaEntries(ctx *types.MsgContext) int {
	pathCount := len(ctx.MediaPaths)
	urlCount := len(ctx.MediaUrls)
	single := 0
	if ctx.MediaPath != "" || ctx.MediaUrl != "" {
		single = 1
	}
	// Return the maximum across all sources.
	max := single
	if pathCount > max {
		max = pathCount
	}
	if urlCount > max {
		max = urlCount
	}
	return max
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
