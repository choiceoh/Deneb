// callbacks.go — Late-bind callback registry for channel integration.
//
// ChannelCallbacks stores all callback functions that integrate the chat
// handler with a specific channel (e.g., Telegram). Set during server
// initialization, read during request handling. Protected by an RWMutex.
package chat

import (
	"context"
	"fmt"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// ChannelCallbacks holds late-bind callback functions for channel integration.
// Thread-safe: all fields are protected by mu.
type ChannelCallbacks struct {
	mu sync.RWMutex

	replyFunc    ReplyFunc      // delivers response to originating channel
	mediaSendFn  MediaSendFunc  // delivers files to originating channel
	typingFn     TypingFunc     // sends typing indicator during agent run
	reactionFn   ReactionFunc   // sets emoji reaction on triggering message
	draftEditFn  DraftEditFunc  // sends/edits streaming draft messages
	deleteMsgFn  MessageDeleter // deletes a channel message (cancel-time draft cleanup)
	broadcastRaw streaming.BroadcastRawFunc

	// emitAgentFn sends agent lifecycle events to gateway event subscriptions.
	emitAgentFn func(kind, sessionKey, runID string, payload map[string]any)
	// emitTranscriptFn sends transcript updates to gateway event subscriptions.
	emitTranscriptFn func(sessionKey string, message any, messageID string)

	// uploadLimits maps channelID → max file upload size in bytes.
	uploadLimits map[string]int64

	// shutdownCtx is the server lifecycle context.
	shutdownCtx context.Context

	// defaultModel can be updated at runtime via SetDefaultModel.
	defaultModel string

	// runStateMachine tracks active agent runs for status broadcasting.
	runStateMachine *telegram.RunStateMachine

	// statusDepsFunc returns server-level status data for /status command.
	statusDepsFunc StatusDepsFunc

	// insightsProviderFunc generates a MarkdownV2 usage report for /insights.
	// Optional — nil disables the command and the dispatcher replies with a
	// "not available" notice.
	insightsProviderFunc InsightsProviderFunc
}

// NewChannelCallbacks creates a ChannelCallbacks with default model.
func NewChannelCallbacks(defaultModel string) *ChannelCallbacks {
	return &ChannelCallbacks{
		defaultModel: defaultModel,
		uploadLimits: make(map[string]int64),
	}
}

// Snapshot atomically reads all callback fields into a CallbackSnapshot.
// Used by buildRunDeps to capture stable references for the run goroutine.
func (cb *ChannelCallbacks) Snapshot() CallbackSnapshot {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return CallbackSnapshot{
		replyFunc:        cb.replyFunc,
		mediaSendFn:      cb.mediaSendFn,
		typingFn:         cb.typingFn,
		reactionFn:       cb.reactionFn,
		draftEditFn:      cb.draftEditFn,
		deleteMsgFn:      cb.deleteMsgFn,
		broadcastRaw:     cb.broadcastRaw,
		emitAgentFn:      cb.emitAgentFn,
		emitTranscriptFn: cb.emitTranscriptFn,
		shutdownCtx:      cb.shutdownCtx,
		defaultModel:     cb.defaultModel,
	}
}

// CallbackSnapshot is an immutable snapshot of callbacks for a single run.
type CallbackSnapshot struct {
	replyFunc        ReplyFunc
	mediaSendFn      MediaSendFunc
	typingFn         TypingFunc
	reactionFn       ReactionFunc
	draftEditFn      DraftEditFunc
	deleteMsgFn      MessageDeleter
	broadcastRaw     streaming.BroadcastRawFunc
	emitAgentFn      func(kind, sessionKey, runID string, payload map[string]any)
	emitTranscriptFn func(sessionKey string, message any, messageID string)
	shutdownCtx      context.Context
	defaultModel     string
}

// Validate reports whether required callbacks are wired. Call once after
// all SetX calls on a channel-facing Handler; it returns an error if the
// caller forgot to set replyFunc, which would otherwise cause every reply
// to drop silently at runtime. Optional callbacks (media, typing, reaction,
// draft, delete, broadcast, emit*) are not checked — their absence is
// handled at the callsite via nil guards and documented as optional.
func (cb *ChannelCallbacks) Validate() error {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	if cb.replyFunc == nil {
		return fmt.Errorf("ChannelCallbacks: replyFunc is required for channel-facing handlers " +
			"(without it every agent reply would drop silently); " +
			"call SetReplyFunc before the server starts accepting messages")
	}
	return nil
}

// --- Setters (called during server initialization) ---

func (cb *ChannelCallbacks) SetReplyFunc(fn ReplyFunc) {
	cb.mu.Lock()
	cb.replyFunc = fn
	cb.mu.Unlock()
}

func (cb *ChannelCallbacks) SetMediaSendFunc(fn MediaSendFunc) {
	cb.mu.Lock()
	cb.mediaSendFn = fn
	cb.mu.Unlock()
}

func (cb *ChannelCallbacks) SetTypingFunc(fn TypingFunc) {
	cb.mu.Lock()
	cb.typingFn = fn
	cb.mu.Unlock()
}

func (cb *ChannelCallbacks) SetReactionFunc(fn ReactionFunc) {
	cb.mu.Lock()
	cb.reactionFn = fn
	cb.mu.Unlock()
}

func (cb *ChannelCallbacks) SetDraftEditFunc(fn DraftEditFunc) {
	cb.mu.Lock()
	cb.draftEditFn = fn
	cb.mu.Unlock()
}

func (cb *ChannelCallbacks) SetMessageDeleter(fn MessageDeleter) {
	cb.mu.Lock()
	cb.deleteMsgFn = fn
	cb.mu.Unlock()
}

func (cb *ChannelCallbacks) SetChannelUploadLimit(channelID string, maxBytes int64) {
	cb.mu.Lock()
	cb.uploadLimits[channelID] = maxBytes
	cb.mu.Unlock()
}

func (cb *ChannelCallbacks) SetDefaultModel(model string) {
	cb.mu.Lock()
	cb.defaultModel = model
	cb.mu.Unlock()
}

func (cb *ChannelCallbacks) SetShutdownCtx(ctx context.Context) {
	cb.mu.Lock()
	cb.shutdownCtx = ctx
	cb.mu.Unlock()
}

func (cb *ChannelCallbacks) SetRunStateMachine(sm *telegram.RunStateMachine) {
	cb.mu.Lock()
	cb.runStateMachine = sm
	cb.mu.Unlock()
}

func (cb *ChannelCallbacks) SetStatusDepsFunc(fn StatusDepsFunc) {
	cb.mu.Lock()
	cb.statusDepsFunc = fn
	cb.mu.Unlock()
}

// SetInsightsProviderFunc installs the optional /insights report generator.
// Passing nil disables the command.
func (cb *ChannelCallbacks) SetInsightsProviderFunc(fn InsightsProviderFunc) {
	cb.mu.Lock()
	cb.insightsProviderFunc = fn
	cb.mu.Unlock()
}

// --- Getters ---

func (cb *ChannelCallbacks) ChannelUploadLimit(channelID string) int64 {
	cb.mu.RLock()
	n := cb.uploadLimits[channelID]
	cb.mu.RUnlock()
	return n
}

func (cb *ChannelCallbacks) ReplyFn() ReplyFunc {
	cb.mu.RLock()
	fn := cb.replyFunc
	cb.mu.RUnlock()
	return fn
}

func (cb *ChannelCallbacks) MediaSendFn() MediaSendFunc {
	cb.mu.RLock()
	fn := cb.mediaSendFn
	cb.mu.RUnlock()
	return fn
}

func (cb *ChannelCallbacks) TypingFn() TypingFunc {
	cb.mu.RLock()
	fn := cb.typingFn
	cb.mu.RUnlock()
	return fn
}

func (cb *ChannelCallbacks) ReactionFn() ReactionFunc {
	cb.mu.RLock()
	fn := cb.reactionFn
	cb.mu.RUnlock()
	return fn
}

func (cb *ChannelCallbacks) DefaultModel() string {
	cb.mu.RLock()
	m := cb.defaultModel
	cb.mu.RUnlock()
	return m
}

func (cb *ChannelCallbacks) RunStateMachine() *telegram.RunStateMachine {
	cb.mu.RLock()
	sm := cb.runStateMachine
	cb.mu.RUnlock()
	return sm
}

func (cb *ChannelCallbacks) StatusDeps() StatusDepsFunc {
	cb.mu.RLock()
	fn := cb.statusDepsFunc
	cb.mu.RUnlock()
	return fn
}

// InsightsProvider returns the currently installed /insights generator, or nil.
func (cb *ChannelCallbacks) InsightsProvider() InsightsProviderFunc {
	cb.mu.RLock()
	fn := cb.insightsProviderFunc
	cb.mu.RUnlock()
	return fn
}
