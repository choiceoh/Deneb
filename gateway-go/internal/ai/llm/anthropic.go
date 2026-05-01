// Package llm — Anthropic Messages API native client.
//
// This file implements StreamChat against Anthropic's /v1/messages endpoint
// (or any Anthropic-compatible proxy such as Z.AI's
// https://api.z.ai/api/anthropic). The wire format already matches the
// internal ChatRequest / Message / ContentBlock shape, so we marshal those
// directly and forward Anthropic's SSE events to the consumer without
// translation — saves a layer and preserves `signature` fields required for
// interleaved-thinking-2025-05-14 round-tripping.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// streamChatAnthropic sends a streaming Messages API request and forwards
// the SSE event stream to consumeStream. The Anthropic event names
// (message_start, content_block_start, content_block_delta,
// content_block_stop, message_delta, message_stop, ping, error) are
// already what the consumer expects.
func (c *Client) streamChatAnthropic(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	req.Stream = true
	req.Messages = NormalizeMessages(req.Messages)

	body, err := buildAnthropicRequestBody(&req)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	url := c.baseURL + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	setAnthropicAuth(httpReq, c.apiKey)
	setBetaHeaders(httpReq, &req)

	respBody, err := c.DoStream(ctx, httpReq)
	if err != nil {
		return nil, err
	}

	rawEvents := ParseSSE(respBody)

	out := make(chan StreamEvent, 16)
	done := make(chan struct{})
	closeOnce := sync.OnceFunc(func() { respBody.Close() })

	go func() {
		select {
		case <-ctx.Done():
			closeOnce()
		case <-done:
		}
	}()

	go func() {
		defer close(out)
		defer close(done)
		defer closeOnce()
		c.forwardAnthropicStream(ctx, rawEvents, out)
	}()

	return out, nil
}

// anthropicAPIVersion is the Anthropic Messages API version we target.
// Pinning is required by Anthropic; the value is the latest stable as of
// this writing.
const anthropicAPIVersion = "2023-06-01"

// anthropicRequest is the wire body sent to /v1/messages. Anthropic-specific
// shape: system is a separate top-level field, messages carry content blocks
// directly, and thinking is a structured object with budget_tokens.
type anthropicRequest struct {
	Model         string             `json:"model"`
	Messages      []Message          `json:"messages"`
	System        json.RawMessage    `json:"system,omitempty"`
	MaxTokens     int                `json:"max_tokens"`
	Stream        bool               `json:"stream"`
	Tools         []Tool             `json:"tools,omitempty"`
	ToolChoice    any                `json:"tool_choice,omitempty"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	TopK          *int               `json:"top_k,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Thinking      *anthropicThinking `json:"thinking,omitempty"`
}

// anthropicThinking is the request-side thinking config. Anthropic
// specifies type="enabled" plus a budget. The Interleaved bit on
// ThinkingConfig drives the anthropic-beta header, not this object.
type anthropicThinking struct {
	Type         string `json:"type"`          // "enabled"
	BudgetTokens int    `json:"budget_tokens"` // ≥ 1024
}

// buildAnthropicRequestBody marshals ChatRequest into Anthropic's
// /v1/messages request body. PreSerialize on tools is honoured so the
// hot-path skips reflection; identical to streamChatOpenAI.
func buildAnthropicRequestBody(req *ChatRequest) ([]byte, error) {
	for i := range req.Tools {
		req.Tools[i].PreSerialize()
	}

	body := anthropicRequest{
		Model:         req.Model,
		Messages:      req.Messages,
		System:        req.System,
		MaxTokens:     req.MaxTokens,
		Stream:        true,
		Tools:         req.Tools,
		ToolChoice:    req.ToolChoice,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		TopK:          req.TopK,
		StopSequences: req.StopSequences,
	}
	if req.Thinking != nil && req.Thinking.Type == "enabled" && req.Thinking.BudgetTokens > 0 {
		body.Thinking = &anthropicThinking{
			Type:         "enabled",
			BudgetTokens: req.Thinking.BudgetTokens,
		}
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	if len(req.ExtraBody) > 0 {
		raw, err = mergeJSONFields(raw, req.ExtraBody)
		if err != nil {
			return nil, err
		}
	}
	return raw, nil
}

// setAnthropicAuth attaches the API key for Anthropic-style endpoints.
// Z.AI's Anthropic-compatible proxy accepts Bearer; the upstream Anthropic
// API itself prefers x-api-key. We send both so the same client works
// across proxies without per-host branching — Anthropic ignores the
// Authorization header when x-api-key is present, and Z.AI accepts either.
func setAnthropicAuth(req *http.Request, apiKey string) {
	if apiKey == "" {
		return
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("Authorization", "Bearer "+apiKey)
}

// forwardAnthropicStream pipes Anthropic SSE events directly to the
// consumer. Anthropic's event names already match our internal stream
// vocabulary (message_start, content_block_*, message_delta, message_stop,
// error, ping). `ping` events are dropped — they're keepalives only.
func (c *Client) forwardAnthropicStream(ctx context.Context, in <-chan StreamEvent, out chan<- StreamEvent) {
	for ev := range in {
		switch ev.Type {
		case "":
			// Some upstreams omit the event: line for data: [DONE]; tolerate
			// silently rather than spamming logs.
			continue
		case "ping":
			continue
		}
		emit(ctx, out, ev)
	}
	emit(ctx, out, StreamEvent{Type: "message_stop"})
}
