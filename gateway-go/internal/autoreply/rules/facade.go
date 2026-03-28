package rules

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/directives"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/typing"
)

// InlineDirectives aliases parsed inline directive metadata.
type InlineDirectives = directives.InlineDirectives

// DirectiveParseOptions aliases inline directive parse options.
type DirectiveParseOptions = directives.DirectiveParseOptions

// TypingMode aliases resolved typing mode.
type TypingMode = typing.TypingMode

// TypingModeContext aliases typing mode resolution inputs.
type TypingModeContext = typing.TypingModeContext


const (
	TypingModeInstant  = typing.TypingModeInstant
	TypingModeMessage  = typing.TypingModeMessage
	TypingModeThinking = typing.TypingModeThinking
	TypingModeNever    = typing.TypingModeNever
)

// ParseInlineDirectives parses slash-style inline directives.
func ParseInlineDirectives(body string, opts *DirectiveParseOptions) InlineDirectives {
	return directives.ParseInlineDirectives(body, opts)
}

// IsDirectiveOnly returns true when message body contains directives only.
func IsDirectiveOnly(parsed InlineDirectives) bool {
	return directives.IsDirectiveOnly(parsed)
}

// ResolveTypingMode resolves how typing indicators should behave.
func ResolveTypingMode(ctx TypingModeContext) TypingMode {
	return typing.ResolveTypingMode(ctx)
}
