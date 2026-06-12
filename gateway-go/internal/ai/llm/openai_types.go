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
	TopK                *int              `json:"top_k,omitempty"` // vLLM/OpenRouter extension; only sent when a profile/config sets it
	FrequencyPenalty    *float64          `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64          `json:"presence_penalty,omitempty"`
	Stop                []string          `json:"stop,omitempty"`
	Tools               []openAITool      `json:"tools,omitempty"`
	ToolChoice          any               `json:"tool_choice,omitempty"`
	ResponseFormat      *ResponseFormat   `json:"response_format,omitempty"`
	ReasoningEffort     string            `json:"reasoning_effort,omitempty"` // "low", "medium", "high"
	// ChatTemplateKwargs forwards per-request chat-template variables to a
	// vLLM server (e.g. {"thinking": false} to disable a dual-mode model's
	// thinking phase). Only set when ThinkingConfig.TemplateKwarg names the
	// model's toggle; other OpenAI-compatible servers never see the field.
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
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

	// ReasoningContent echoes prior assistant reasoning back to the API.
	// Used for interleaved thinking on OpenRouter / Z.AI / Anthropic-compatible
	// proxies that accept the symmetric form of the streamed reasoning_content
	// delta. Empty string is omitted from the wire payload.
	ReasoningContent string `json:"reasoning_content,omitempty"`
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
	ReasoningContent string                `json:"reasoning_content,omitempty"` // reasoning text (DeepSeek/OpenRouter)
	Reasoning        string                `json:"reasoning,omitempty"`         // reasoning text (vLLM/step3p7 stream under "reasoning")
	Refusal          string                `json:"refusal,omitempty"`           // model refusal
	ToolCalls        []openAIDeltaToolCall `json:"tool_calls,omitempty"`
}

// reasoningText returns the streamed reasoning, accepting both field names in
// the wild: DeepSeek/OpenRouter emit "reasoning_content"; vLLM-backed models
// (e.g. step3p7) emit "reasoning". Reading only reasoning_content silently drops
// vLLM reasoning so no thinking_delta is ever emitted to the client.
func (d openAIDelta) reasoningText() string {
	if d.ReasoningContent != "" {
		return d.ReasoningContent
	}
	return d.Reasoning
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
	PromptTokensDetails     *promptTokensDetails     `json:"prompt_tokens_details,omitempty"`

	// PromptCacheHitTokens is DeepSeek's official-API spelling of the cached
	// prompt-token count (everyone else nests it under prompt_tokens_details).
	PromptCacheHitTokens int `json:"prompt_cache_hit_tokens,omitempty"`
}

type completionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// promptTokensDetails carries the prefix-cache breakdown of prompt_tokens.
// vLLM (--enable-prefix-caching) and OpenAI both report cached_tokens here.
type promptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

// splitPromptTokens normalizes the OpenAI prompt-token count to Anthropic
// usage semantics: input excludes cache reads, so input + cacheRead equals the
// wire's prompt_tokens. OpenAI-compatible servers report prompt_tokens as the
// TOTAL prompt; mapping it to InputTokens unchanged while also surfacing
// cached_tokens would double-count cache hits in every downstream sum
// (usage tracker, modeltuner read-ratio denominators).
func (u *openAIUsage) splitPromptTokens() (input, cacheRead int) {
	cached := 0
	if u.PromptTokensDetails != nil {
		cached = u.PromptTokensDetails.CachedTokens
	}
	if u.PromptCacheHitTokens > cached {
		cached = u.PromptCacheHitTokens
	}
	if cached <= 0 {
		return u.PromptTokens, 0
	}
	if cached > u.PromptTokens {
		cached = u.PromptTokens
	}
	return u.PromptTokens - cached, cached
}
