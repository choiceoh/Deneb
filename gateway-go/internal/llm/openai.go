package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// StreamChatOpenAI sends a streaming chat request to an OpenAI-compatible
// /chat/completions endpoint and translates the response into the same
// StreamEvent types that consumeStream expects (message_start,
// content_block_start, content_block_delta, content_block_stop,
// message_delta, message_stop).
//
// This enables RunAgent to work with any OpenAI-compatible provider
// (z.ai, sglang, vLLM, etc.) without changes to the agent loop.
func (c *Client) StreamChatOpenAI(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	req.Stream = true

	// Build OpenAI-format request body.
	oaiReq := openAIRequest{
		Model:     req.Model,
		Stream:    true,
		MaxTokens: req.MaxTokens,
	}

	// Convert system prompt to a system message.
	if req.System != "" {
		oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	// Convert messages.
	for _, m := range req.Messages {
		var text string
		if err := json.Unmarshal(m.Content, &text); err == nil {
			oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
				Role:    m.Role,
				Content: text,
			})
			continue
		}
		// Content blocks — extract text.
		var blocks []ContentBlock
		if err := json.Unmarshal(m.Content, &blocks); err == nil {
			var combined string
			for _, b := range blocks {
				if b.Type == "text" {
					combined += b.Text
				}
			}
			if combined != "" {
				oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
					Role:    m.Role,
					Content: combined,
				})
			}
		}
	}

	if req.Temperature != nil {
		oaiReq.Temperature = req.Temperature
	}

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal openai request: %w", err)
	}

	url := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	respBody, err := c.DoStream(ctx, httpReq)
	if err != nil {
		return nil, err
	}

	rawEvents := ParseSSE(respBody)

	out := make(chan StreamEvent, 16)
	done := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			respBody.Close()
		case <-done:
		}
	}()

	go func() {
		defer close(out)
		defer close(done)
		defer respBody.Close()

		firstChunk := true

		for raw := range rawEvents {
			// OpenAI sends "data: [DONE]" as the final event.
			if string(raw.Payload) == "[DONE]" {
				emit(ctx, out, StreamEvent{Type: "message_stop"})
				return
			}

			var chunk openAIChunk
			if err := json.Unmarshal(raw.Payload, &chunk); err != nil {
				continue
			}

			// Emit synthetic message_start on first chunk.
			if firstChunk {
				firstChunk = false
				startPayload, _ := json.Marshal(MessageStart{
					Message: struct {
						ID    string `json:"id"`
						Model string `json:"model"`
						Usage struct {
							InputTokens  int `json:"input_tokens"`
							OutputTokens int `json:"output_tokens"`
						} `json:"usage"`
					}{
						ID:    chunk.ID,
						Model: chunk.Model,
					},
				})
				emit(ctx, out, StreamEvent{Type: "message_start", Payload: startPayload})

				// Emit content_block_start for text block.
				cbsPayload, _ := json.Marshal(ContentBlockStart{
					Index:        0,
					ContentBlock: ContentBlock{Type: "text"},
				})
				emit(ctx, out, StreamEvent{Type: "content_block_start", Payload: cbsPayload})
			}

			if len(chunk.Choices) == 0 {
				// Usage-only chunk (some providers send this at the end).
				if chunk.Usage != nil {
					mdPayload, _ := json.Marshal(MessageDelta{
						Delta: struct {
							StopReason string `json:"stop_reason"`
						}{StopReason: "end_turn"},
						Usage: struct {
							OutputTokens int `json:"output_tokens"`
						}{OutputTokens: chunk.Usage.CompletionTokens},
					})
					emit(ctx, out, StreamEvent{Type: "message_delta", Payload: mdPayload})
				}
				continue
			}

			choice := chunk.Choices[0]

			// Emit text delta.
			if choice.Delta.Content != "" {
				cbdPayload, _ := json.Marshal(ContentBlockDelta{
					Index: 0,
					Delta: struct {
						Type        string `json:"type"`
						Text        string `json:"text,omitempty"`
						PartialJSON string `json:"partial_json,omitempty"`
					}{
						Type: "text_delta",
						Text: choice.Delta.Content,
					},
				})
				emit(ctx, out, StreamEvent{Type: "content_block_delta", Payload: cbdPayload})
			}

			// Check finish reason.
			if choice.FinishReason != "" {
				// Close the text content block.
				cbStopPayload, _ := json.Marshal(ContentBlockStop{Index: 0})
				emit(ctx, out, StreamEvent{Type: "content_block_stop", Payload: cbStopPayload})

				// Map OpenAI finish reasons to Anthropic stop reasons.
				stopReason := "end_turn"
				switch choice.FinishReason {
				case "length":
					stopReason = "max_tokens"
				case "stop":
					stopReason = "end_turn"
				}

				outputTokens := 0
				if chunk.Usage != nil {
					outputTokens = chunk.Usage.CompletionTokens
				}

				mdPayload, _ := json.Marshal(MessageDelta{
					Delta: struct {
						StopReason string `json:"stop_reason"`
					}{StopReason: stopReason},
					Usage: struct {
						OutputTokens int `json:"output_tokens"`
					}{OutputTokens: outputTokens},
				})
				emit(ctx, out, StreamEvent{Type: "message_delta", Payload: mdPayload})
			}
		}

		// Stream ended without [DONE] — emit stop events.
		emit(ctx, out, StreamEvent{Type: "message_stop"})
	}()

	return out, nil
}

func emit(ctx context.Context, ch chan<- StreamEvent, ev StreamEvent) {
	select {
	case ch <- ev:
	case <-ctx.Done():
	}
}

// --- OpenAI request/response types ---

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Stream      bool            `json:"stream"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int    `json:"index"`
		Delta        openAIDelta `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage,omitempty"`
}

type openAIDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
