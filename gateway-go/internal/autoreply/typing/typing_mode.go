package typing

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/tokens"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// TypingMode controls when typing indicators are shown.
type TypingMode string

const (
	TypingModeInstant  TypingMode = "instant"
	TypingModeMessage  TypingMode = "message"
	TypingModeThinking TypingMode = "thinking"
	TypingModeNever    TypingMode = "never"
)

// DefaultGroupTypingMode is the typing mode used for unmentioned group messages.
const DefaultGroupTypingMode TypingMode = TypingModeMessage

// TypingModeContext provides context for resolving the typing mode.
type TypingModeContext struct {
	Configured     TypingMode
	IsGroupChat    bool
	WasMentioned   bool
	IsHeartbeat    bool
	TypingPolicy   types.TypingPolicy
	SuppressTyping bool
}

// ResolveTypingMode determines the typing indicator behavior based on context.
//
// Mirrors src/auto-reply/reply/typing-mode.ts resolveTypingMode().
func ResolveTypingMode(ctx TypingModeContext) TypingMode {
	// Suppress typing for heartbeats and system events.
	if ctx.IsHeartbeat ||
		ctx.TypingPolicy == types.TypingPolicyHeartbeat ||
		ctx.TypingPolicy == types.TypingPolicySystemEvent ||
		ctx.SuppressTyping {
		return TypingModeNever
	}
	if ctx.Configured != "" {
		return ctx.Configured
	}
	if !ctx.IsGroupChat || ctx.WasMentioned {
		return TypingModeInstant
	}
	return DefaultGroupTypingMode
}

// FullTypingSignaler provides phase-aware typing signal dispatch.
// It wraps a TypingController and starts typing at appropriate phases
// depending on the resolved TypingMode.
//
// Mirrors src/auto-reply/reply/typing-mode.ts createTypingSignaler().
type FullTypingSignaler struct {
	controller *TypingController
	Mode       TypingMode

	ShouldStartImmediately    bool
	ShouldStartOnMessageStart bool
	ShouldStartOnText         bool
	ShouldStartOnReasoning    bool

	disabled          bool
	hasRenderableText bool
}

// NewFullTypingSignaler creates a phase-aware typing signaler.
func NewFullTypingSignaler(controller *TypingController, mode TypingMode, isHeartbeat bool) *FullTypingSignaler {
	return &FullTypingSignaler{
		controller:                controller,
		Mode:                      mode,
		ShouldStartImmediately:    mode == TypingModeInstant,
		ShouldStartOnMessageStart: mode == TypingModeMessage,
		ShouldStartOnText:         mode == TypingModeMessage || mode == TypingModeInstant,
		ShouldStartOnReasoning:    mode == TypingModeThinking,
		disabled:                  isHeartbeat || mode == TypingModeNever,
	}
}

// SignalRunStart signals that an agent run has started.
func (s *FullTypingSignaler) SignalRunStart() {
	if s.disabled || !s.ShouldStartImmediately || s.controller == nil {
		return
	}
	s.controller.Start()
}

// SignalMessageStart signals that a message has started streaming.
func (s *FullTypingSignaler) SignalMessageStart() {
	if s.disabled || !s.ShouldStartOnMessageStart || s.controller == nil {
		return
	}
	if !s.hasRenderableText {
		return
	}
	s.controller.Start()
}

// SignalTextDelta signals a text delta was received, starting typing if appropriate.
// Filters out silent reply tokens (NO_REPLY).
//
// For "message"/"instant" modes: uses startTypingOnText (filters silent tokens).
// For "thinking" mode: keeps typing alive via refreshTypingTtl if already active.
func (s *FullTypingSignaler) SignalTextDelta(text string) {
	if s.disabled || s.controller == nil {
		return
	}
	trimmed := trimWhitespace(text)
	if trimmed == "" {
		return
	}
	renderable := !tokens.IsSilentReplyText(trimmed, tokens.SilentReplyToken)
	if renderable {
		s.hasRenderableText = true
	} else {
		// Non-renderable text (silent token) — skip typing.
		return
	}
	if s.ShouldStartOnText {
		s.controller.StartTypingOnText(text)
		return
	}
	if s.ShouldStartOnReasoning {
		// In thinking mode, keep alive via TTL refresh.
		if !s.controller.IsActive() {
			s.controller.StartTypingLoop()
		}
		s.controller.RefreshTypingTtl()
	}
}

// SignalReasoningDelta signals that reasoning/thinking output was received.
func (s *FullTypingSignaler) SignalReasoningDelta() {
	if s.disabled || !s.ShouldStartOnReasoning || s.controller == nil {
		return
	}
	if !s.hasRenderableText {
		return
	}
	s.controller.StartTypingLoop()
	s.controller.RefreshTypingTtl()
}

// SignalToolStart signals that a tool invocation has started.
// Starts typing immediately when tools begin, refreshes TTL if already active.
func (s *FullTypingSignaler) SignalToolStart() {
	if s.disabled || s.controller == nil {
		return
	}
	if !s.controller.IsActive() {
		s.controller.StartTypingLoop()
		s.controller.RefreshTypingTtl()
		return
	}
	s.controller.RefreshTypingTtl()
}

// Stop stops the underlying typing controller.
func (s *FullTypingSignaler) Stop() {
	if s.controller != nil {
		s.controller.Stop()
	}
}

func trimWhitespace(s string) string {
	// Inline trim to avoid importing strings for a single use.
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
