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

// DraftDeleteFunc deletes a streaming draft message from the originating channel.
// Used to clean up the partial draft before the final reply is delivered.
type DraftDeleteFunc func(ctx context.Context, delivery *DeliveryContext, msgID string) error

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
	API     string `json:"api"` // "openai" (default)
}

// DeliveryContext carries channel routing information for a chat message.
type DeliveryContext struct {
	Channel   string `json:"channel,omitempty"`
	To        string `json:"to,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	ThreadID  string `json:"threadId,omitempty"`
	MessageID  string `json:"messageId,omitempty"`  // triggering message ID for reply threading
	DraftMsgID string `json:"draftMsgId,omitempty"` // draft streaming message ID for edit-in-place on completion
}

// ChatMessage represents a message in a session transcript.
// Content is json.RawMessage so it can hold either a plain JSON string
// (legacy text-only) or an array of ContentBlocks (rich format with
// tool_use, tool_result, thinking blocks). Use TextContent() to extract
// text, NewTextChatMessage() to construct text-only messages.
type ChatMessage struct {
	Role        string           `json:"role"`
	Content     json.RawMessage  `json:"content,omitempty"`
	Attachments []ChatAttachment `json:"attachments,omitempty"`
	Timestamp   int64            `json:"timestamp,omitempty"`
	ParentID    string           `json:"parentId,omitempty"`
	ID          string           `json:"id,omitempty"`
}

// NewTextChatMessage creates a ChatMessage with text-only content.
func NewTextChatMessage(role, text string, ts int64) ChatMessage {
	return ChatMessage{
		Role:      role,
		Content:   MarshalJSONString(text),
		Timestamp: ts,
	}
}

// TextContent extracts a plain text string from Content.
// If Content is a JSON string, returns the unquoted string.
// If Content is a ContentBlock array, joins all text block values.
// Returns "" if Content is nil or empty.
func (m *ChatMessage) TextContent() string {
	if len(m.Content) == 0 {
		return ""
	}
	// Try JSON string first (most common, legacy format).
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		return s
	}
	// Try ContentBlock array (rich format).
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}
	if err := json.Unmarshal(m.Content, &blocks); err == nil {
		var texts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				texts = append(texts, b.Text)
			}
		}
		if len(texts) > 0 {
			return joinTexts(texts)
		}
	}
	// Fallback: return raw content as string (shouldn't happen).
	return string(m.Content)
}

// HasContent returns true if Content is non-empty.
func (m *ChatMessage) HasContent() bool {
	if len(m.Content) == 0 {
		return false
	}
	// Check if it's an empty JSON string ("").
	return string(m.Content) != `""`
}

// MarshalJSONString returns s as a JSON-encoded string (with quotes).
func MarshalJSONString(s string) json.RawMessage {
	data, _ := json.Marshal(s)
	return data
}

// joinTexts joins text fragments with newlines.
func joinTexts(texts []string) string {
	if len(texts) == 1 {
		return texts[0]
	}
	result := texts[0]
	for _, t := range texts[1:] {
		result += "\n\n" + t
	}
	return result
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
	// CloneRecent copies the most recent `limit` messages from srcKey to dstKey.
	// Used for shadow sessions (KindShadow) that inherit conversation context.
	CloneRecent(srcKey, dstKey string, limit int) error
}
