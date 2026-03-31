// Package toolctx provides shared types, context helpers, and dependency
// definitions used across the chat tool subsystem (tools/, toolreg/, chat/).
//
// This is a leaf package with zero intra-chat imports, enabling clean
// dependency flow: tools/ -> toolctx/, toolreg/ -> toolctx/, chat/ -> toolctx/.
package toolctx

import (
	"context"
	"encoding/json"
	"time"
)

// ToolFunc is an adapter to use ordinary functions as tool executors.
type ToolFunc func(ctx context.Context, input json.RawMessage) (string, error)

// ToolDef describes a tool with its schema, description, and executor function.
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any
	Fn          ToolFunc
	Hidden      bool   // if true, excluded from LLMTools() but still callable via Execute
	Profile     string // optional: "coding" = coding-only, "" = available in all profiles
}

// ToolRegistrar accepts tool registrations. Implemented by chat.ToolRegistry.
type ToolRegistrar interface {
	RegisterTool(def ToolDef)
}

// ToolExecutor executes a named tool with JSON input and returns the result.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, input json.RawMessage) (string, error)
}

// BroadcastFunc sends an event to all matching subscribers.
type BroadcastFunc func(event string, payload any) (int, []error)

// ReplyFunc delivers the assistant response back to the originating channel.
type ReplyFunc func(ctx context.Context, delivery *DeliveryContext, text string) error

// TypingFunc signals a typing indicator to the originating channel.
type TypingFunc func(ctx context.Context, delivery *DeliveryContext) error

// ReactionFunc sets/removes an emoji reaction on the triggering message.
type ReactionFunc func(ctx context.Context, delivery *DeliveryContext, emoji string) error

// DraftEditFunc sends or edits a streaming draft message on the originating channel.
// Returns the message ID of the sent/edited message and an error.
// On the first call (msgID == ""), it sends a new message and returns its ID.
// On subsequent calls, it edits the message with the given ID.
type DraftEditFunc func(ctx context.Context, delivery *DeliveryContext, msgID string, text string) (newMsgID string, err error)

// ToolProgressFunc is called during agent execution to report tool execution events.
type ToolProgressFunc func(ctx context.Context, delivery *DeliveryContext, event ToolProgressEvent)

// ToolProgressEvent describes a tool execution lifecycle event.
type ToolProgressEvent struct {
	Type    string // "start", "complete"
	Name    string // tool name
	Reason  string // raw thinking block text for summarization (only for "start"; may be empty)
	IsError bool   // true if tool execution failed (only for "complete")
	Result  string // truncated error output for display (only for "complete" when IsError; may be empty)
}

// ProviderConfig holds credentials and endpoint for an LLM provider.
type ProviderConfig struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseUrl"`
	API     string `json:"api"` // "openai" (default) or "anthropic"
}

// DeliveryContext carries channel routing information for a chat message.
type DeliveryContext struct {
	Channel   string `json:"channel,omitempty"`
	To        string `json:"to,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	ThreadID  string `json:"threadId,omitempty"`
	MessageID string `json:"messageId,omitempty"` // triggering message ID for reply threading
}

// ChatMessage represents a message in a session transcript.
type ChatMessage struct {
	Role        string           `json:"role"`
	Content     string           `json:"content,omitempty"`
	Attachments []ChatAttachment `json:"attachments,omitempty"`
	Timestamp   int64            `json:"timestamp,omitempty"`
	ParentID    string           `json:"parentId,omitempty"`
	ID          string           `json:"id,omitempty"`
}

// ChatAttachment represents an attachment on a chat message.
type ChatAttachment struct {
	Type     string `json:"type"` // "image", "file", "audio", "video"
	MimeType string `json:"mimeType,omitempty"`
	URL      string `json:"url,omitempty"`
	Data     string `json:"data,omitempty"` // base64-encoded content
	Name     string `json:"name,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// AbortEntry tracks an active abort controller for a running chat session.
type AbortEntry struct {
	SessionKey string
	ClientRun  string
	CancelFn   context.CancelFunc
	ExpiresAt  time.Time
}

// MediaSendFunc delivers a file to the originating channel.
// mediaType is one of: photo, document, video, audio, voice (empty = auto-detect).
type MediaSendFunc func(ctx context.Context, delivery *DeliveryContext, filePath, mediaType, caption string, silent bool) error

// SearchResult holds matching messages from a single session.
type SearchResult struct {
	SessionKey string       `json:"sessionKey"`
	Matches    []MatchedMsg `json:"matches"`
}

// MatchedMsg is a single matching message with surrounding context.
type MatchedMsg struct {
	Index   int           `json:"index"`   // position in transcript
	Message ChatMessage   `json:"message"` // the matched message
	Context []ChatMessage `json:"context"` // surrounding messages (+-1)
}

// TranscriptStore loads and persists session transcripts.
type TranscriptStore interface {
	Load(sessionKey string, limit int) ([]ChatMessage, int, error)
	Append(sessionKey string, msg ChatMessage) error
	Delete(sessionKey string) error
	ListKeys() ([]string, error)
	Search(query string, maxResults int) ([]SearchResult, error)
}
