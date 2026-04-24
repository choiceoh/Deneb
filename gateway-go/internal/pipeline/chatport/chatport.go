// Package chatport defines shared interfaces and types for the chat ↔ autoreply
// boundary. Both packages depend on chatport (a leaf package with zero intra-project
// imports), preventing compile-time circular dependencies.
package chatport

// TypingSignaler abstracts phase-aware typing indicator dispatch.
// Concrete implementation: autoreply/typing.FullTypingSignaler.
type TypingSignaler interface {
	SignalRunStart()
	SignalTextDelta(text string)
	SignalReasoningDelta()
	SignalToolStart()
	Stop()
}

// ReplyDirectives holds the result of parsing reply directives from raw agent
// output text. Extracts MEDIA: tokens, threading tags, silent tokens, and
// leaked tool-call markup.
type ReplyDirectives struct {
	Text           string
	MediaURLs      []string
	MediaURL       string
	ReplyToID      string
	ReplyToCurrent bool
	ReplyToTag     bool
	AudioAsVoice   bool
	IsSilent       bool
}

// ParseReplyDirectivesFunc parses reply directives from raw agent output text.
type ParseReplyDirectivesFunc func(raw, currentMessageID, silentToken string) ReplyDirectives

// DraftSanitizerFunc cleans draft streaming text (strips leaked tool-call
// markup, fenced code blocks, etc.).
type DraftSanitizerFunc func(text string) string
