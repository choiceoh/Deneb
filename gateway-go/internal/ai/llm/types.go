// Package llm provides an HTTP client for OpenAI-compatible LLM provider APIs
// with SSE streaming.
package llm

import (
	"encoding/json"
	"strings"
)

// ChatRequest represents a streaming chat completion request.
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
	FrequencyPenalty *float64        `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64        `json:"presence_penalty,omitempty"`

	// ResponseFormat requests structured output.
	// Use &ResponseFormat{Type: "json_object"} for JSON mode.
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`

	// ToolChoice controls tool selection behavior.
	// Values: "auto", "none", "required", or {"type":"function","function":{"name":"..."}}.
	ToolChoice any `json:"tool_choice,omitempty"`

	// Thinking configures extended thinking (mapped to reasoning_effort for OpenAI).
	Thinking *ThinkingConfig `json:"thinking,omitempty"`

	// ExtraBody holds additional top-level fields merged into the OpenAI-format
	// request body. Used for provider-specific parameters (e.g., timeout,
	// logit_bias for CJK blocking).
	ExtraBody map[string]any `json:"-"`
}

// ResponseFormat controls the output format for OpenAI-compatible endpoints.
type ResponseFormat struct {
	Type       string          `json:"type"`                  // "json_object", "json_schema", or "text"
	JSONSchema json.RawMessage `json:"json_schema,omitempty"` // schema definition when Type="json_schema"
}

// hexChars is used by appendJSONString to encode control characters as \uXXXX.
const hexChars = "0123456789abcdef"

// appendJSONString encodes s as a JSON string and appends it to dst.
// It is equivalent to json.Marshal(s) but avoids the reflection path and
// html-safe escaping of <, >, & that json.Marshal performs by default.
// Valid UTF-8 multi-byte sequences are passed through unchanged (JSON allows UTF-8).
func appendJSONString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := range len(s) {
		c := s[i]
		switch {
		case c == '"':
			dst = append(dst, '\\', '"')
		case c == '\\':
			dst = append(dst, '\\', '\\')
		case c < 0x20:
			// Control character — use short form where available, \uXXXX otherwise.
			switch c {
			case '\t':
				dst = append(dst, '\\', 't')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			default:
				dst = append(dst, '\\', 'u', '0', '0', hexChars[c>>4], hexChars[c&0x0f])
			}
		default:
			dst = append(dst, c)
		}
	}
	dst = append(dst, '"')
	return dst
}

// SystemString is a convenience for setting a plain string system prompt.
func SystemString(s string) json.RawMessage {
	if s == "" {
		return nil
	}
	raw, _ := json.Marshal(s)
	return raw
}

// SystemBlocks is a convenience for setting an array-of-blocks system prompt.
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

// AppendSystemText appends additional text to the system prompt.
// Handles both JSON string and []ContentBlock formats.
func AppendSystemText(system json.RawMessage, addition string) json.RawMessage {
	if addition == "" {
		return system
	}
	if len(system) == 0 {
		return SystemString(addition)
	}
	// Try plain string first.
	var s string
	if json.Unmarshal(system, &s) == nil {
		return SystemString(s + "\n\n" + addition)
	}
	// Try array of content blocks — append as new text block.
	var blocks []ContentBlock
	if json.Unmarshal(system, &blocks) == nil {
		blocks = append(blocks, ContentBlock{Type: "text", Text: "\n\n" + addition})
		return SystemBlocks(blocks)
	}
	return system
}

// AppendSystemTexts appends multiple text additions to the system prompt in a single
// unmarshal/marshal cycle. Empty additions are ignored. This is more efficient than
// calling AppendSystemText repeatedly when multiple additions are known upfront.
func AppendSystemTexts(system json.RawMessage, additions ...string) json.RawMessage {
	// Collect non-empty additions.
	filtered := additions[:0:0]
	for _, a := range additions {
		if a != "" {
			filtered = append(filtered, a)
		}
	}
	if len(filtered) == 0 {
		return system
	}
	if len(system) == 0 {
		var sb strings.Builder
		for i, a := range filtered {
			if i > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(a)
		}
		return SystemString(sb.String())
	}
	// Try plain string — unmarshal once, build combined string, marshal once.
	var s string
	if json.Unmarshal(system, &s) == nil {
		var sb strings.Builder
		sb.WriteString(s)
		for _, a := range filtered {
			sb.WriteString("\n\n")
			sb.WriteString(a)
		}
		return SystemString(sb.String())
	}
	// Try array of content blocks — unmarshal once, append blocks, marshal once.
	var blocks []ContentBlock
	if json.Unmarshal(system, &blocks) == nil {
		for _, a := range filtered {
			blocks = append(blocks, ContentBlock{Type: "text", Text: "\n\n" + a})
		}
		return SystemBlocks(blocks)
	}
	return system
}

// ThinkingConfig controls extended thinking (mapped to reasoning_effort for OpenAI-compatible APIs).
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
// Uses appendJSONString to avoid json.Marshal's reflection path and
// html-safe escaping, and to pre-size the allocation from string length.
func NewTextMessage(role, text string) Message {
	return Message{Role: role, Content: appendJSONString(make([]byte, 0, len(text)+2), text)}
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
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`

	// image block (base64 inline)
	Source *ImageSource `json:"source,omitempty"`

	// image_url block (URL reference)
	ImageURL *ImageURL `json:"image_url,omitempty"`

	// thinking block (extended thinking / reasoning content)
	Thinking string `json:"thinking,omitempty"`

	// Cache control for prompt caching.
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// ImageSource is an inline image (base64).
type ImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png", "image/jpeg", etc.
	Data      string `json:"data"`       // base64-encoded image data
}

// ImageURL is an image reference (URL or data URI).
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "auto", "low", "high"
}

// CacheControl marks a content block or tool for prompt caching.
type CacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// Tool describes a tool available to the model.
//
// InputSchema holds the schema as a Go map for programmatic access.
// RawInputSchema holds the pre-serialized JSON bytes used during API
// request marshaling — this avoids re-serializing the deeply nested
// map[string]any via reflection on every LLM call (~40 tools × multiple
// turns). Call PreSerialize() or set RawInputSchema directly.
type Tool struct {
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	InputSchema    map[string]any  `json:"-"`                       // programmatic access; excluded from JSON
	RawInputSchema json.RawMessage `json:"input_schema"`            // pre-serialized; used in API requests
	CacheControl   *CacheControl   `json:"cache_control,omitempty"` // prompt caching
}

// PreSerialize computes RawInputSchema from InputSchema if not already set.
// This is called automatically by ToolRegistry.buildLLMToolsLocked but can
// also be called manually for tools constructed outside the registry.
func (t *Tool) PreSerialize() {
	if t.InputSchema != nil && t.RawInputSchema == nil {
		t.RawInputSchema, _ = json.Marshal(t.InputSchema) // best-effort: marshal of known-good schema cannot fail
	}
}

// StreamEvent represents a single server-sent event from the LLM API.
type StreamEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// --- Streaming event payload types ---

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
		Type        string `json:"type"` // "text_delta" or "input_json_delta"
		Text        string `json:"text,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
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
