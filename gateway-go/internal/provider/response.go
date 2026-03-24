package provider

import (
	"encoding/json"
	"strings"
)

// ContentPart represents a single part of a provider response.
// Mirrors the OpenResponses schema from src/gateway/open-responses.schema.ts.
type ContentPart struct {
	Type     string          `json:"type"`               // "text", "image", "tool_use", "tool_result", "thinking"
	Text     string          `json:"text,omitempty"`      // text content
	URL      string          `json:"url,omitempty"`       // image/file URL
	Base64   string          `json:"base64,omitempty"`    // base64-encoded content
	MimeType string          `json:"mimeType,omitempty"`  // MIME type for image/file
	ToolID   string          `json:"toolId,omitempty"`    // tool_use/tool_result ID
	Name     string          `json:"name,omitempty"`      // tool name
	Input    json.RawMessage `json:"input,omitempty"`     // tool_use input args
	Content  json.RawMessage `json:"content,omitempty"`   // tool_result content
	Thinking string          `json:"thinking,omitempty"`  // thinking content
}

// ProviderResponse represents a complete response from a model provider.
type ProviderResponse struct {
	Role       string        `json:"role"`
	Content    []ContentPart `json:"content"`
	Model      string        `json:"model,omitempty"`
	Provider   string        `json:"provider,omitempty"`
	StopReason string        `json:"stopReason,omitempty"`
	Usage      *UsageInfo    `json:"usage,omitempty"`
	Timestamp  int64         `json:"timestamp"`
	API        string        `json:"api,omitempty"` // e.g. "openai-responses", "anthropic-messages"
}

// UsageInfo tracks token consumption and cost for a provider response.
type UsageInfo struct {
	Input       int64   `json:"input"`
	Output      int64   `json:"output"`
	CacheRead   int64   `json:"cacheRead,omitempty"`
	CacheWrite  int64   `json:"cacheWrite,omitempty"`
	TotalTokens int64   `json:"totalTokens"`
	Cost        float64 `json:"cost,omitempty"`
}

// FormatForChannel flattens a ProviderResponse into plain text suitable
// for delivery to a messaging channel. Non-text parts are summarized.
func FormatForChannel(resp *ProviderResponse) string {
	if resp == nil || len(resp.Content) == 0 {
		return ""
	}
	var b strings.Builder
	for i, part := range resp.Content {
		if i > 0 && b.Len() > 0 {
			b.WriteString("\n")
		}
		switch part.Type {
		case "text":
			b.WriteString(part.Text)
		case "thinking":
			// Thinking blocks are typically not shown to end users.
		case "image":
			if part.URL != "" {
				b.WriteString(part.URL)
			} else {
				b.WriteString("[image]")
			}
		case "tool_use":
			b.WriteString("[tool: ")
			b.WriteString(part.Name)
			b.WriteString("]")
		case "tool_result":
			if len(part.Content) > 0 {
				// Try to extract text from the tool result.
				var text string
				_ = json.Unmarshal(part.Content, &text)
				if text != "" {
					b.WriteString(text)
				}
			}
		}
	}
	return b.String()
}

// FormatForTranscript serializes a ProviderResponse as JSON suitable for
// appending to a JSONL session transcript file.
func FormatForTranscript(resp *ProviderResponse) json.RawMessage {
	if resp == nil {
		return nil
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return nil
	}
	return data
}

// ExtractText returns the concatenated text content from a response.
func ExtractText(resp *ProviderResponse) string {
	if resp == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range resp.Content {
		if part.Type == "text" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(part.Text)
		}
	}
	return b.String()
}
