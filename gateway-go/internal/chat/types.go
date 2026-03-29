package chat

import (
	"context"
	"time"
)

// BroadcastFunc sends an event to all matching subscribers.
type BroadcastFunc func(event string, payload any) (int, []error)

// ReplyFunc delivers the assistant response back to the originating channel.
// Called with the delivery context (channel + recipient) and the response text.
type ReplyFunc func(ctx context.Context, delivery *DeliveryContext, text string) error

// TypingFunc signals a typing indicator to the originating channel.
// Called periodically during an agent run to show "typing..." status.
type TypingFunc func(ctx context.Context, delivery *DeliveryContext) error

// ReactionFunc sets/removes an emoji reaction on the triggering message.
// Pass an empty emoji to remove reactions.
type ReactionFunc func(ctx context.Context, delivery *DeliveryContext, emoji string) error

// ToolProgressFunc is called during agent execution to report tool execution events.
// Used by Discord to update progress embeds in real-time.
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
	API     string `json:"api"` // "openai" (default) or "anthropic" (inferred from provider ID)
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
	Data     string `json:"data,omitempty"` // base64-encoded content (used for inline media)
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
