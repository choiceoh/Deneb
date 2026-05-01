package llm

import "encoding/json"

// --- Anthropic Messages API request types ---
//
// Mirrors the Anthropic Messages API JSON schema. The internal Message and
// ContentBlock types already match Anthropic semantics, so most of the
// request body is built from the input ChatRequest with minor adjustments
// (system as []block, tool_choice shape, thinking config shape).

type anthropicRequest struct {
	Model         string             `json:"model"`
	Messages      []anthropicMessage `json:"messages"`
	MaxTokens     int                `json:"max_tokens"`
	Stream        bool               `json:"stream"`
	System        json.RawMessage    `json:"system,omitempty"`         // string or []ContentBlock
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	TopK          *int               `json:"top_k,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Tools         []anthropicTool    `json:"tools,omitempty"`
	ToolChoice    any                `json:"tool_choice,omitempty"`
	Thinking      *anthropicThinking `json:"thinking,omitempty"`
}

// anthropicMessage is the wire form of one entry in the messages array.
// Content is json.RawMessage so callers may pass either a plain string or
// a pre-serialized []ContentBlock without an extra unmarshal/marshal cycle.
type anthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// anthropicTool wraps a tool definition in Anthropic's expected shape.
type anthropicTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *CacheControl   `json:"cache_control,omitempty"`
}

// anthropicThinking is the per-request extended thinking config.
// Anthropic accepts {"type":"enabled","budget_tokens":N} or
// {"type":"disabled"}. Interleaved is a separate beta header, not a body field.
type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}
