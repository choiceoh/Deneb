// Package llm provides an HTTP client for LLM provider APIs with SSE streaming.
//
// This package is provider-agnostic at the type level. Provider-specific
// adapters (e.g., Anthropic) build requests and map streaming events
// using the shared types defined here.
package llm

import (
	"encoding/json"
	"strings"
)

// ChatRequest represents a streaming chat completion request.
// The System field accepts both a plain string and an array of ContentBlocks
// (for Anthropic cache_control annotations and extended thinking).
type ChatRequest struct {
	Model            string          `json:"model"`
	Messages         []Message       `json:"messages"`
	System           json.RawMessage `json:"system,omitempty"` // string or []ContentBlock
	MaxTokens        int             `json:"max_tokens"`
	Tools            []Tool          `json:"tools,omitempty"`
	Stream           bool            `json:"stream"`
	Temperature      *float64        `json:"temperature,omitempty"`
	TopP             *float64        `json:"top_p,omitempty"`
	TopK             *int            `json:"top_k,omitempty"`
	StopSequences    []string        `json:"stop_sequences,omitempty"`
	FrequencyPenalty *float64        `json:"-"` // OpenAI only; excluded from Anthropic JSON
	PresencePenalty  *float64        `json:"-"` // OpenAI only; excluded from Anthropic JSON

	// Anthropic extended thinking support.
	Thinking *ThinkingConfig `json:"thinking,omitempty"`
}

// SystemString is a convenience for setting a plain string system prompt.
func SystemString(s string) json.RawMessage {
	if s == "" {
		return nil
	}
	raw, _ := json.Marshal(s)
	return raw
}

// SystemBlocks is a convenience for setting an array-of-blocks system prompt
// (used for Anthropic cache_control annotations).
func SystemBlocks(blocks []ContentBlock) json.RawMessage {
	if len(blocks) == 0 {
		return nil
	}
	raw, _ := json.Marshal(blocks)
	return raw
}

// ExtractSystemText extracts a plain text string from the System field,
// whether it's stored as a JSON string or an array of content blocks.
func ExtractSystemText(system json.RawMessage) string {
	if len(system) == 0 {
		return ""
	}
	// Try plain string first.
	var s string
	if json.Unmarshal(system, &s) == nil {
		return s
	}
	// Try array of content blocks.
	var blocks []ContentBlock
	if json.Unmarshal(system, &blocks) == nil {
		var sb strings.Builder
		for _, b := range blocks {
			if b.Type == "text" {
				sb.WriteString(b.Text)
			}
		}
		return sb.String()
	}
	return ""
}

// ThinkingConfig controls Anthropic's extended thinking feature.
type ThinkingConfig struct {
	Type         string `json:"type"`          // "enabled" or "disabled"
	BudgetTokens int    `json:"budget_tokens"` // max tokens for thinking
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
	Type string `json:"type"` // "text", "tool_use", "tool_result", "image", "thinking"

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

	// image block (Anthropic: type="image", source.type/media_type/data)
	Source *ImageSource `json:"source,omitempty"`

	// image_url block (OpenAI: type="image_url", image_url.url)
	ImageURL *ImageURL `json:"image_url,omitempty"`

	// thinking block (Anthropic extended thinking)
	Thinking string `json:"thinking,omitempty"`

	// Cache control (Anthropic prompt caching + OpenAI-compatible).
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// ImageSource is an Anthropic-style inline image (base64).
type ImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png", "image/jpeg", etc.
	Data      string `json:"data"`       // base64-encoded image data
}

// ImageURL is an OpenAI-style image reference (URL or data URI).
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "auto", "low", "high"
}

// CacheControl marks a content block for prompt caching.
type CacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// Tool describes a tool available to the model.
type Tool struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"input_schema"`
	CacheControl *CacheControl  `json:"cache_control,omitempty"` // Anthropic prompt caching
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
