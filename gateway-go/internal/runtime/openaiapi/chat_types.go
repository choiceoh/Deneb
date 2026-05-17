package openaiapi

// chatCompletionsRequest mirrors the OpenAI POST /v1/chat/completions
// request body. Only fields used by the v1 handler are typed; unknown
// fields are tolerated (json.Decoder default).
//
// Multi-modal content (image_url parts) and response_format are
// accepted by the parser but not yet forwarded — see chat_translate.go.
type chatCompletionsRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	TopP        *float64      `json:"top_p,omitempty"`
	Stop        any           `json:"stop,omitempty"` // string or []string
	User        string        `json:"user,omitempty"`
}

// chatMessage is one message in the OpenAI request.
//
// Content is json.RawMessage because OpenAI permits either a plain
// string or an array of content parts (text + image_url).
type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
	Name    string `json:"name,omitempty"`
}

// chatCompletionResponse mirrors the OpenAI non-stream response.
type chatCompletionResponse struct {
	ID                string       `json:"id"`
	Object            string       `json:"object"`
	Created           int64        `json:"created"`
	Model             string       `json:"model"`
	Choices           []chatChoice `json:"choices"`
	Usage             chatUsage    `json:"usage"`
	SystemFingerprint string       `json:"system_fingerprint,omitempty"`
}

type chatChoice struct {
	Index        int                 `json:"index"`
	Message      chatResponseMessage `json:"message"`
	FinishReason string              `json:"finish_reason"`
}

type chatResponseMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
