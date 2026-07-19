// Package toolport provides stable shared types and context helpers used across
// the chat tool subsystem (tools/, toolwire/, chat/).
//
// This is a leaf package with zero intra-chat imports, enabling clean
// dependency flow: tools/ -> toolport/, toolwire/ -> toolport/, chat/ -> toolport/.
// Volatile domain/platform dependency bags live in sibling package tooldeps.
package toolport

import (
	"context"
	"encoding/json"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

// ToolFunc is an adapter to use ordinary functions as tool executors.
type ToolFunc func(ctx context.Context, input rawJSON) (string, error)

// ToolDef describes a tool with its schema, description, and executor function.
type ToolDef struct {
	Name        string
	Description string
	InputSchema jsonObject
	Fn          ToolFunc
	Hidden      bool   // if true, excluded from LLMTools() but still callable via Execute
	Deferred    bool   // if true, excluded from initial LLMTools() but activatable via fetch_tools
	Profile     string // optional: "coding" = coding-only, "" = available in all profiles
	MaxOutput   int    // max chars for tool result in LLM context; 0 = agent.DefaultMaxOutput
}

// DeferredToolSummary is a minimal view of a deferred tool for system prompt assembly.
type DeferredToolSummary struct {
	Name        string
	Description string
}

// ToolRegistrar accepts tool registrations. Implemented by chat.ToolRegistry.
type ToolRegistrar interface {
	RegisterTool(def ToolDef)
}

// ToolExecutor executes a named tool with JSON input and returns the result.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, input rawJSON) (string, error)
}

// BroadcastFunc sends an event to all matching subscribers.
type BroadcastFunc func(event string, payload rawJSON) (int, []error)

// ReplyFunc delivers the assistant response back to the originating channel.
type ReplyFunc func(ctx context.Context, delivery *DeliveryContext, text string) error

// TypingFunc signals a typing indicator to the originating channel.
type TypingFunc func(ctx context.Context, delivery *DeliveryContext) error

// ReactionFunc sets/removes an emoji reaction on the triggering message.
type ReactionFunc func(ctx context.Context, delivery *DeliveryContext, emoji string) error

// MessageDeleter removes a previously-sent message from the originating
// channel. Used to clean up orphan streaming drafts left behind when a
// run is cancelled mid-stream (e.g. via the chat-merge window).
type MessageDeleter func(ctx context.Context, delivery *DeliveryContext, msgID string) error

// DraftEditFunc sends or edits a streaming draft message on the originating channel.
// Returns the message ID of the sent/edited message and an error.
// On the first call (msgID == ""), it sends a new message and returns its ID.
// On subsequent calls, it edits the message with the given ID.
type DraftEditFunc func(ctx context.Context, delivery *DeliveryContext, msgID string, text string) (newMsgID string, err error)

// ProviderConfig is the stable provider configuration owned by chatport.
type ProviderConfig = chatport.ProviderConfig

// RoutingConfig is the stable routing override owned by chatport.
type RoutingConfig = chatport.RoutingConfig

// DeliveryContext is the stable channel-routing contract owned by chatport.
type DeliveryContext = chatport.DeliveryContext

// Transcript wire types are owned by chatport. Aliases keep the established
// toolport API source- and JSON-compatible while Polaris and compaction depend
// on the neutral contract directly.
type (
	ChatMessage    = chatport.ChatMessage
	ChatAttachment = chatport.ChatAttachment
)

// NewTextChatMessage preserves the legacy toolport constructor surface.
func NewTextChatMessage(role, text string, ts int64) ChatMessage {
	return chatport.NewTextChatMessage(role, text, ts)
}

// MarshalJSONString preserves the legacy toolport helper surface.
func MarshalJSONString(s string) json.RawMessage {
	return rawJSON(chatport.MarshalJSONString(s))
}

// AbortEntry tracks an active abort controller for a running chat session.
// CancelFn is a context.CancelCauseFunc so callers (e.g. the Send merge
// path) can attach a sentinel error like ErrMergedIntoNewRun, letting the
// run goroutine distinguish a clean merge cancel from a generic kill and
// perform channel-side cleanup accordingly.
type AbortEntry struct {
	SessionKey string
	ClientRun  string
	CancelFn   context.CancelCauseFunc
	ExpiresAt  time.Time
	// Automation marks an autonomous relay run (cron, heartbeat sweep,
	// mailpoll, goal, event-ingest) that merely RIDES a session key the user
	// also chats on. Auto-steer must never fold a user's message into such a
	// run: the user isn't watching that turn, and their message would surface
	// as a stray note inside an automation transcript (live 2026-07-18 —
	// "채팅 안 하고 있는데 진행 중인 답변에 반영하겠다더라").
	Automation bool
}

// MediaSendFunc delivers a file to the originating channel.
// mediaType is one of: photo, document, video, audio, voice (empty = auto-detect).
type MediaSendFunc func(ctx context.Context, delivery *DeliveryContext, filePath, mediaType, caption string, silent bool) error

type (
	SearchResult    = chatport.SearchResult
	MatchedMsg      = chatport.MatchedMsg
	TranscriptStore = chatport.TranscriptStore
)
