// Package llm provides an HTTP client for LLM provider APIs with SSE streaming.
//
// This package is provider-agnostic at the type level. Provider-specific
// adapters (e.g., Anthropic) build requests and map streaming events
// using the shared types defined here.
package llm

import "encoding/json"

// ChatRequest represents a streaming chat completion request.
type ChatRequest struct {
	Model       string   `json:"model"`
	Messages    []Message `json:"messages"`
	System      string   `json:"system,omitempty"`
	MaxTokens   int      `json:"max_tokens"`
	Tools       []Tool   `json:"tools,omitempty"`
	Stream      bool     `json:"stream"`
	Temperature *float64 `json:"temperature,omitempty"`
}

// Message represents a single message in a conversation.
type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string or []ContentBlock
}

// NewTextMessage creates a message with a plain text content string.
func NewTextMessage(role, text string) Message {
	raw, _ := json.Marshal(text)
	return Message{Role: role, Content: raw}
}

// NewBlockMessage creates a message with structured content blocks.
func NewBlockMessage(role string, blocks []ContentBlock) Message {
	raw, _ := json.Marshal(blocks)
	return Message{Role: role, Content: raw}
}

// ContentBlock represents a single content block in a message.
type ContentBlock struct {
	Type string `json:"type"` // "text", "tool_use", "tool_result"

	// text block
	Text string `json:"text,omitempty"`

	// tool_use block
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result block
	ToolUseID  string `json:"tool_use_id,omitempty"`
	Content    string `json:"content,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`
}

// Tool describes a tool available to the model.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// StreamEvent represents a single server-sent event from the LLM API.
type StreamEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// --- Anthropic streaming event payload types ---

// MessageStart is the payload for "message_start" events.
type MessageStart struct {
	Message struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// ContentBlockStart is the payload for "content_block_start" events.
type ContentBlockStart struct {
	Index        int          `json:"index"`
	ContentBlock ContentBlock `json:"content_block"`
}

// ContentBlockDelta is the payload for "content_block_delta" events.
type ContentBlockDelta struct {
	Index int `json:"index"`
	Delta struct {
		Type         string `json:"type"` // "text_delta" or "input_json_delta"
		Text         string `json:"text,omitempty"`
		PartialJSON  string `json:"partial_json,omitempty"`
	} `json:"delta"`
}

// ContentBlockStop is the payload for "content_block_stop" events.
type ContentBlockStop struct {
	Index int `json:"index"`
}

// MessageDelta is the payload for "message_delta" events.
type MessageDelta struct {
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// TokenUsage tracks token consumption for a request.
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
