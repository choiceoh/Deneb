// streaming.go — Streaming directives and post-compaction context.
// Mirrors src/auto-reply/reply/streaming-directives.ts (137 LOC),
// post-compaction-context.ts (233 LOC), untrusted-context.ts (16 LOC),
// message-preprocess-hooks.ts (50 LOC).
package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// StreamingDirective represents a directive detected during streaming.
type StreamingDirective struct {
	Type  string // "think", "verbose", "model", etc.
	Value string
}

// DetectStreamingDirective checks if streamed text contains an inline directive.
func DetectStreamingDirective(text string) *StreamingDirective {
	// During streaming, we watch for [[directives]] in the output.
	tags := ExtractReplyTags(text)
	for _, tag := range tags {
		switch tag.Name {
		case "think":
			return &StreamingDirective{Type: "think", Value: tag.Value}
		case "verbose":
			return &StreamingDirective{Type: "verbose", Value: tag.Value}
		case "model":
			return &StreamingDirective{Type: "model", Value: tag.Value}
		}
	}
	return nil
}

// PostCompactionContext manages context after history compaction.
type PostCompactionContext struct {
	CompactedAt    int64
	PreservedCount int
	TotalRemoved   int
	SummaryText    string
}

// BuildPostCompactionHint creates a system hint about compaction.
func BuildPostCompactionHint(ctx PostCompactionContext) string {
	if ctx.CompactedAt == 0 {
		return ""
	}
	return "[Context was compacted. Earlier messages were summarized to fit within the token budget.]"
}

// UntrustedContentPolicy controls how untrusted content is handled.
type UntrustedContentPolicy struct {
	AllowExternalContent bool
	StripSystemTags      bool
	MaxContentLength     int
}

// DefaultUntrustedContentPolicy returns the safe default policy.
func DefaultUntrustedContentPolicy() UntrustedContentPolicy {
	return UntrustedContentPolicy{
		AllowExternalContent: false,
		StripSystemTags:      true,
		MaxContentLength:     100000,
	}
}

// SanitizeUntrustedContent applies the untrusted content policy.
func SanitizeUntrustedContent(text string, policy UntrustedContentPolicy) string {
	if text == "" {
		return text
	}

	result := text

	// Truncate if too long.
	if policy.MaxContentLength > 0 && len(result) > policy.MaxContentLength {
		result = result[:policy.MaxContentLength] + "…[truncated]"
	}

	// Strip system tags if enabled.
	if policy.StripSystemTags {
		result = StripReplyTags(result)
	}

	return result
}

// MessagePreprocessHook is called before agent processing to modify the message.
type MessagePreprocessHook func(msg *types.MsgContext) error

// RunPreprocessHooks executes all preprocess hooks on a message.
func RunPreprocessHooks(msg *types.MsgContext, hooks []MessagePreprocessHook) error {
	for _, hook := range hooks {
		if err := hook(msg); err != nil {
			return err
		}
	}
	return nil
}
