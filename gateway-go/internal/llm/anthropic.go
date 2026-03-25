package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	// DefaultAnthropicBaseURL is the default Anthropic API base URL.
	DefaultAnthropicBaseURL = "https://api.anthropic.com"
	// AnthropicAPIVersion is the API version header value.
	AnthropicAPIVersion = "2023-06-01"
)

// StreamChat sends a streaming chat request to the Anthropic Messages API.
// Returns a channel of StreamEvents. The channel is closed when the stream
// ends or the context is cancelled.
//
// The caller must consume the channel to completion; otherwise the
// underlying HTTP response body will not be closed.
func (c *Client) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	req.Stream = true

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", AnthropicAPIVersion)

	respBody, err := c.DoStream(ctx, httpReq)
	if err != nil {
		return nil, err
	}

	// Parse SSE stream in background; closing respBody stops the parser.
	rawEvents := ParseSSE(respBody)

	// Map raw SSE events to typed StreamEvents, closing respBody on completion.
	out := make(chan StreamEvent, 16)
	done := make(chan struct{})

	// Watch for context cancellation and close respBody to unblock the SSE
	// parser goroutine. Without this, the parser stays blocked on
	// scanner.Scan() when the caller stops consuming events.
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

		for raw := range rawEvents {
			// Use the SSE event type if set, otherwise infer from data.
			eventType := raw.Type
			if eventType == "" {
				// Try to extract type from the JSON payload.
				var probe struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(raw.Payload, &probe) == nil && probe.Type != "" {
					eventType = probe.Type
				}
			}

			select {
			case out <- StreamEvent{
				Type:    eventType,
				Payload: raw.Payload,
			}:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, nil
}
