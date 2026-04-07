package llm

import "encoding/json"

// --- OpenAI request/response types ---

type openAIRequest struct {
	Model               string            `json:"model"`
	Messages            []openAIMessage   `json:"messages"`
	Stream              bool              `json:"stream"`
	StreamOptions       *openAIStreamOpts `json:"stream_options,omitempty"`
	MaxTokens           int               `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int              `json:"max_completion_tokens,omitempty"` // for reasoning models
	Temperature         *float64          `json:"temperature,omitempty"`
	TopP                *float64          `json:"top_p,omitempty"`
	FrequencyPenalty    *float64          `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64          `json:"presence_penalty,omitempty"`
	Stop                []string          `json:"stop,omitempty"`
	Tools               []openAITool      `json:"tools,omitempty"`
	ToolChoice          any               `json:"tool_choice,omitempty"`
	ResponseFormat      *ResponseFormat   `json:"response_format,omitempty"`
	ReasoningEffort     string            `json:"reasoning_effort,omitempty"` // "low", "medium", "high"
}

// openAIStreamOpts controls streaming behavior.
type openAIStreamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

// openAITool wraps a function definition in OpenAI's tool format.
type openAITool struct {
	Type     string         `json:"type"` // always "function"
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// openAIMessage represents a message in the OpenAI chat format.
// Content is any because it can be a string, []openAIContentPart (for vision),
// or nil (marshals to JSON null, required by OpenAI for tool-only assistant messages).
type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`                // string, []openAIContentPart, or nil (→ null)
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`   // assistant tool calls
	ToolCallID string           `json:"tool_call_id,omitempty"` // tool result reference
}

// openAIContentPart is a single part in a multipart content array (text or image_url).
type openAIContentPart struct {
	Type     string        `json:"type"`                // "text" or "image_url"
	Text     string        `json:"text,omitempty"`      // for type="text"
	ImageURL *openAIImgURL `json:"image_url,omitempty"` // for type="image_url"
}

// openAIImgURL holds the URL (or data URI) for an image content part.
type openAIImgURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "auto", "low", "high"
}

// openAIToolCall represents a tool call in an assistant message.
type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int         `json:"index"`
		Delta        openAIDelta `json:"delta"`
		FinishReason *string     `json:"finish_reason"` // pointer: null → nil, "stop" → &"stop"
	} `json:"choices"`
	Usage *openAIUsage `json:"usage,omitempty"`
}

type openAIDelta struct {
	Role             string                `json:"role,omitempty"`
	Content          string                `json:"content,omitempty"`
	ReasoningContent string                `json:"reasoning_content,omitempty"` // reasoning model thinking
	Refusal          string                `json:"refusal,omitempty"`           // model refusal
	ToolCalls        []openAIDeltaToolCall `json:"tool_calls,omitempty"`
}

// openAIDeltaToolCall is a streamed fragment of a tool call.
type openAIDeltaToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type openAIUsage struct {
	PromptTokens            int                      `json:"prompt_tokens"`
	CompletionTokens        int                      `json:"completion_tokens"`
	TotalTokens             int                      `json:"total_tokens"`
	CompletionTokensDetails *completionTokensDetails `json:"completion_tokens_details,omitempty"`
}

type completionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}
