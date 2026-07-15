// callbacks.go — Late-bind callback registry for channel integration.
//
// ChannelCallbacks stores all callback functions that integrate the chat
// handler with a specific channel (e.g., the native client). Set during server
// initialization, read during request handling. Protected by an RWMutex.
package chat

import (
	"context"
	"fmt"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
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
	emitAgentFn func(kind, sessionKey, runID string, payload jsonObject)
	// emitTranscriptFn sends transcript updates to gateway event subscriptions.
	emitTranscriptFn func(sessionKey string, message rawJSON, messageID string)

	// uploadLimits maps channelID → max file upload size in bytes.
	uploadLimits map[string]int64

	// shutdownCtx is the server lifecycle context.
	shutdownCtx context.Context

	// defaultModel can be updated at runtime via SetDefaultModel.
	defaultModel string

	// statusDepsFunc returns server-level status data for /status command.
	statusDepsFunc StatusDepsFunc
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
	emitAgentFn      func(kind, sessionKey, runID string, payload jsonObject)
	emitTranscriptFn func(sessionKey string, message rawJSON, messageID string)
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

// SetReplyFunc installs the channel reply delivery callback.
func (cb *ChannelCallbacks) SetReplyFunc(fn ReplyFunc) {
	cb.mu.Lock()
	cb.replyFunc = fn
	cb.mu.Unlock()
}

// SetMediaSendFunc installs the channel media delivery callback.
func (cb *ChannelCallbacks) SetMediaSendFunc(fn MediaSendFunc) {
	cb.mu.Lock()
	cb.mediaSendFn = fn
	cb.mu.Unlock()
}

// SetTypingFunc installs the typing-indicator callback.
func (cb *ChannelCallbacks) SetTypingFunc(fn TypingFunc) {
	cb.mu.Lock()
	cb.typingFn = fn
	cb.mu.Unlock()
}

// SetReactionFunc installs the message-reaction callback.
func (cb *ChannelCallbacks) SetReactionFunc(fn ReactionFunc) {
	cb.mu.Lock()
	cb.reactionFn = fn
	cb.mu.Unlock()
}

// SetDraftEditFunc installs the streaming draft editor.
func (cb *ChannelCallbacks) SetDraftEditFunc(fn DraftEditFunc) {
	cb.mu.Lock()
	cb.draftEditFn = fn
	cb.mu.Unlock()
}

// SetMessageDeleter installs the channel message deletion callback.
func (cb *ChannelCallbacks) SetMessageDeleter(fn MessageDeleter) {
	cb.mu.Lock()
	cb.deleteMsgFn = fn
	cb.mu.Unlock()
}

// SetChannelUploadLimit records the maximum media size for a channel.
func (cb *ChannelCallbacks) SetChannelUploadLimit(channelID string, maxBytes int64) {
	cb.mu.Lock()
	cb.uploadLimits[channelID] = maxBytes
	cb.mu.Unlock()
}

// SetDefaultModel updates the model reported to newly captured runs.
func (cb *ChannelCallbacks) SetDefaultModel(model string) {
	cb.mu.Lock()
	cb.defaultModel = model
	cb.mu.Unlock()
}

// SetShutdownCtx installs the server lifecycle context.
func (cb *ChannelCallbacks) SetShutdownCtx(ctx context.Context) {
	cb.mu.Lock()
	cb.shutdownCtx = ctx
	cb.mu.Unlock()
}

// SetStatusDepsFunc installs the lazy status dependency provider.
func (cb *ChannelCallbacks) SetStatusDepsFunc(fn StatusDepsFunc) {
	cb.mu.Lock()
	cb.statusDepsFunc = fn
	cb.mu.Unlock()
}

// --- Getters ---

// ChannelUploadLimit returns the configured media limit for channelID.
func (cb *ChannelCallbacks) ChannelUploadLimit(channelID string) int64 {
	cb.mu.RLock()
	n := cb.uploadLimits[channelID]
	cb.mu.RUnlock()
	return n
}

// ReplyFn returns the current reply delivery callback.
func (cb *ChannelCallbacks) ReplyFn() ReplyFunc {
	cb.mu.RLock()
	fn := cb.replyFunc
	cb.mu.RUnlock()
	return fn
}

// MediaSendFn returns the current media delivery callback.
func (cb *ChannelCallbacks) MediaSendFn() MediaSendFunc {
	cb.mu.RLock()
	fn := cb.mediaSendFn
	cb.mu.RUnlock()
	return fn
}

// TypingFn returns the current typing callback.
func (cb *ChannelCallbacks) TypingFn() TypingFunc {
	cb.mu.RLock()
	fn := cb.typingFn
	cb.mu.RUnlock()
	return fn
}

// ReactionFn returns the current reaction callback.
func (cb *ChannelCallbacks) ReactionFn() ReactionFunc {
	cb.mu.RLock()
	fn := cb.reactionFn
	cb.mu.RUnlock()
	return fn
}

// DefaultModel returns the model captured for new runs.
func (cb *ChannelCallbacks) DefaultModel() string {
	cb.mu.RLock()
	m := cb.defaultModel
	cb.mu.RUnlock()
	return m
}

// StatusDeps returns the lazy status dependency provider.
func (cb *ChannelCallbacks) StatusDeps() StatusDepsFunc {
	cb.mu.RLock()
	fn := cb.statusDepsFunc
	cb.mu.RUnlock()
	return fn
}
