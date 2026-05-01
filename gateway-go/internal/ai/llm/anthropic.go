package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// anthropicAPIVersion is sent with every Anthropic Messages request via
// the `anthropic-version` HTTP header. Pinned so wire behavior stays
// stable across upstream releases.
const anthropicAPIVersion = "2023-06-01"

// streamChatAnthropic sends the request to an Anthropic Messages API
// endpoint (POST /v1/messages) and forwards SSE events through to
// callers. The internal StreamEvent contract already matches Anthropic
// semantics so most events pass through unchanged.
func (c *Client) streamChatAnthropic(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	body, err := buildAnthropicRequestBody(req)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(c.baseURL, "/") + "/v1/messages"
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
		forwardAnthropicStream(ctx, rawEvents, out)
	}()

	return out, nil
}

// buildAnthropicRequestBody serializes a ChatRequest into Anthropic
// Messages API JSON, merging ExtraBody fields at the top level.
func buildAnthropicRequestBody(req ChatRequest) ([]byte, error) {
	areq := anthropicRequest{
		Model:         req.Model,
		MaxTokens:     req.MaxTokens,
		Stream:        true,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		TopK:          req.TopK,
		StopSequences: req.StopSequences,
		ToolChoice:    req.ToolChoice,
	}

	// System prompt: pass through raw (string or []ContentBlock, both valid).
	if len(req.System) > 0 {
		areq.System = req.System
	}

	// Messages: convert from internal format. Anthropic shares semantics, so
	// we only need to ensure each Content payload is a JSON array of blocks
	// (string content is also accepted by Anthropic, but normalizing to
	// blocks keeps tool_use / tool_result handling uniform).
	areq.Messages = make([]anthropicMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		content, err := sanitizeAnthropicContent(m.Content)
		if err != nil {
			return nil, fmt.Errorf("sanitize message content for role %q: %w", m.Role, err)
		}
		areq.Messages = append(areq.Messages, anthropicMessage{
			Role:    m.Role,
			Content: content,
		})
	}

	// Tools: pre-serialize schemas and copy.
	if len(req.Tools) > 0 {
		areq.Tools = make([]anthropicTool, 0, len(req.Tools))
		for i := range req.Tools {
			req.Tools[i].PreSerialize()
			areq.Tools = append(areq.Tools, anthropicTool{
				Name:         req.Tools[i].Name,
				Description:  req.Tools[i].Description,
				InputSchema:  req.Tools[i].RawInputSchema,
				CacheControl: req.Tools[i].CacheControl,
			})
		}
	}

	// Thinking: native Anthropic shape. Interleaved is enabled via the beta
	// header (set in setBetaHeaders), not the body.
	if req.Thinking != nil && req.Thinking.Type == "enabled" && req.Thinking.BudgetTokens > 0 {
		areq.Thinking = &anthropicThinking{
			Type:         "enabled",
			BudgetTokens: req.Thinking.BudgetTokens,
		}
	}

	body, err := json.Marshal(areq)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	if len(req.ExtraBody) > 0 {
		body, err = mergeJSONFields(body, req.ExtraBody)
		if err != nil {
			return nil, fmt.Errorf("merge extra body: %w", err)
		}
	}

	return body, nil
}

// emptyJSONObject is the canonical empty input for tool_use blocks when the
// model called a tool with no arguments. Anthropic-compat servers reject
// `null` here ("sequence item 0: expected str instance, NoneType found").
var emptyJSONObject = json.RawMessage(`{}`)

// sanitizeAnthropicContent ensures the message content payload is a JSON
// array of blocks with all required fields populated. Empty/nil content
// becomes an empty text block (Anthropic rejects null content); plain text
// strings are wrapped in a single text block; block arrays have their
// required fields backfilled (text="", input={}, content="", thinking="")
// so omitempty does not strip a field that the upstream validator requires.
func sanitizeAnthropicContent(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return marshalAnthropicBlocks([]ContentBlock{{Type: "text", Text: ""}})
	}
	// Plain string content → wrap as a single text block.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return marshalAnthropicBlocks([]ContentBlock{{Type: "text", Text: s}})
	}
	// Array of blocks — backfill required fields.
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}
	for i := range blocks {
		fillRequiredBlockFields(&blocks[i])
	}
	if len(blocks) == 0 {
		blocks = []ContentBlock{{Type: "text", Text: ""}}
	}
	return marshalAnthropicBlocks(blocks)
}

// fillRequiredBlockFields populates fields that Anthropic-compat servers
// expect to be non-null even when empty. omitempty on the wire types would
// otherwise drop them.
func fillRequiredBlockFields(b *ContentBlock) {
	switch b.Type {
	case "tool_use":
		if len(b.Input) == 0 || string(b.Input) == "null" {
			b.Input = emptyJSONObject
		}
	}
}

// marshalAnthropicBlocks serializes content blocks while making sure the
// fields with omitempty tags that upstream validators require are emitted
// even when empty. Currently this rewrites text/tool_result/thinking
// blocks so their required string field appears as `""` instead of being
// dropped from the JSON.
func marshalAnthropicBlocks(blocks []ContentBlock) (json.RawMessage, error) {
	type wireBlock struct {
		Type         string          `json:"type"`
		Text         *string         `json:"text,omitempty"`
		ID           string          `json:"id,omitempty"`
		Name         string          `json:"name,omitempty"`
		Input        json.RawMessage `json:"input,omitempty"`
		ToolUseID    string          `json:"tool_use_id,omitempty"`
		Content      *string         `json:"content,omitempty"`
		IsError      bool            `json:"is_error,omitempty"`
		Source       *ImageSource    `json:"source,omitempty"`
		Thinking     *string         `json:"thinking,omitempty"`
		Signature    string          `json:"signature,omitempty"`
		CacheControl *CacheControl   `json:"cache_control,omitempty"`
	}
	out := make([]wireBlock, len(blocks))
	for i, b := range blocks {
		w := wireBlock{
			Type:         b.Type,
			ID:           b.ID,
			Name:         b.Name,
			Input:        b.Input,
			ToolUseID:    b.ToolUseID,
			IsError:      b.IsError,
			Source:       b.Source,
			Signature:    b.Signature,
			CacheControl: b.CacheControl,
		}
		switch b.Type {
		case "text":
			t := b.Text
			w.Text = &t
		case "tool_result":
			c := b.Content
			w.Content = &c
		case "thinking":
			th := b.Thinking
			w.Thinking = &th
		}
		out[i] = w
	}
	return json.Marshal(out)
}

// setAnthropicAuth attaches Anthropic's `x-api-key` header (which Z.ai's
// Anthropic-compatible endpoint also expects). Empty keys are skipped so
// unit tests can hit a local mock without auth.
func setAnthropicAuth(req *http.Request, apiKey string) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return
	}
	req.Header.Set("x-api-key", apiKey)
}

// forwardAnthropicStream copies SSE events from an Anthropic Messages
// response onto the internal StreamEvent channel. The two protocols
// share event names (message_start, content_block_*, message_delta,
// message_stop, ping, error) and payload shapes, so most events pass
// through with no translation.
func forwardAnthropicStream(ctx context.Context, rawEvents <-chan StreamEvent, out chan<- StreamEvent) {
	for raw := range rawEvents {
		switch raw.Type {
		case "ping":
			// Keepalive — do not forward; consumer treats absence of
			// events as idle and would reset its watchdog incorrectly
			// if pings were emitted as real events.
			continue
		case "":
			// Some endpoints send `data: ...` without an explicit `event:`
			// line. Try to recover the type from the JSON payload.
			var probe struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(raw.Payload, &probe) == nil && probe.Type != "" {
				raw.Type = probe.Type
			}
		}
		emit(ctx, out, raw)
		if raw.Type == "message_stop" || raw.Type == "error" {
			return
		}
	}
}
