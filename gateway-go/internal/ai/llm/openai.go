package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// StreamChat dispatches a streaming chat request to the wire protocol the
// client was configured for. The returned channel emits Anthropic-style
// StreamEvents (message_start, content_block_*, message_delta, message_stop)
// regardless of provider — translation happens inside the per-mode helper.
//
// This enables RunAgent to work with any provider (z.ai, localai, vLLM,
// Anthropic-compatible endpoints, etc.) without changes to the agent loop.
func (c *Client) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	req.Stream = true

	// Normalize messages: merge consecutive same-role messages that may
	// arise from mid-loop compaction or post-compaction restoration.
	// Applied right before the API call so callers' slices stay untouched.
	req.Messages = NormalizeMessages(req.Messages)

	if c.apiMode == APIModeAnthropic {
		return c.streamChatAnthropic(ctx, req)
	}
	return c.streamChatOpenAI(ctx, req)
}

// streamChatOpenAI sends the request to an OpenAI-compatible
// /chat/completions endpoint and translates the SSE response into
// Anthropic-style StreamEvents.
func (c *Client) streamChatOpenAI(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	// Build OpenAI-format request body.
	oaiReq := openAIRequest{
		Model:         req.Model,
		Stream:        true,
		StreamOptions: &openAIStreamOpts{IncludeUsage: true},
		MaxTokens:     req.MaxTokens,
	}

	// Convert tools to OpenAI function-calling format.
	// PreSerialize caches RawInputSchema on the backing slice so subsequent
	// calls with the same tools skip json.Marshal entirely.
	for i := range req.Tools {
		req.Tools[i].PreSerialize()
		oaiReq.Tools = append(oaiReq.Tools, openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        req.Tools[i].Name,
				Description: req.Tools[i].Description,
				Parameters:  req.Tools[i].RawInputSchema,
			},
		})
	}

	// System prompt + messages.
	if systemText := ExtractSystemText(req.System); systemText != "" {
		oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
			Role: "system", Content: systemText,
		})
	}
	oaiReq.Messages = append(oaiReq.Messages, c.convertMessagesToOpenAI(req.Messages, interleavedEnabled(&req))...)

	applySamplingParams(&oaiReq, &req)

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal openai request: %w", err)
	}

	// Merge ExtraBody fields into the serialized JSON.
	if len(req.ExtraBody) > 0 {
		body, err = mergeJSONFields(body, req.ExtraBody)
		if err != nil {
			return nil, fmt.Errorf("merge extra body: %w", err)
		}
	}

	url := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	setOpenAIBearerAuth(httpReq, c.apiKey)
	setBetaHeaders(httpReq, &req)

	respBody, err := c.DoStream(ctx, httpReq)
	if err != nil {
		return nil, err
	}

	rawEvents := ParseSSE(respBody)

	out := make(chan StreamEvent, 16)
	done := make(chan struct{})

	// Protect respBody.Close() from concurrent calls (context cancel vs normal exit).
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
		c.translateOpenAIStream(ctx, rawEvents, out)
	}()

	return out, nil
}

// --- StreamChat helpers ---

// convertMessagesToOpenAI translates Anthropic-format messages into OpenAI chat messages.
// Handles plain text, tool_use, tool_result, and image content blocks.
//
// preserveThinking controls whether prior assistant `thinking` blocks are
// echoed back to the API on the `reasoning_content` field. Required for
// Anthropic interleaved thinking and for OpenRouter-proxied reasoning that
// must round-trip across tool boundaries within a single turn.
func (c *Client) convertMessagesToOpenAI(msgs []Message, preserveThinking bool) []openAIMessage {
	var out []openAIMessage
	for _, m := range msgs {
		// Try plain text string first.
		var text string
		if err := json.Unmarshal(m.Content, &text); err == nil {
			out = append(out, openAIMessage{Role: m.Role, Content: text})
			continue
		}

		// Content blocks — may contain text, tool_use, tool_result, or image.
		var blocks []ContentBlock
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			c.logger.Warn("skipping message with unparseable content",
				"role", m.Role, "error", err,
				"content_preview", truncateForLog(string(m.Content), 200))
			continue
		}

		// Classify blocks in this message.
		var textParts string
		var thinkingParts string
		var toolCalls []openAIToolCall
		var toolResults []ContentBlock
		var imageParts []openAIContentPart
		for _, b := range blocks {
			switch b.Type {
			case "text":
				textParts += b.Text
			case "thinking":
				if preserveThinking && m.Role == "assistant" {
					thinkingParts += b.Thinking
				}
			case "tool_use":
				args := "{}"
				if len(b.Input) > 0 {
					args = string(b.Input)
				}
				toolCalls = append(toolCalls, openAIToolCall{
					ID:   b.ID,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      b.Name,
						Arguments: args,
					},
				})
			case "tool_result":
				toolResults = append(toolResults, b)
			case "image":
				// Anthropic image block (base64) → OpenAI image_url with data URI.
				if b.Source != nil && b.Source.Data != "" {
					dataURI := "data:" + b.Source.MediaType + ";base64," + b.Source.Data
					imageParts = append(imageParts, openAIContentPart{
						Type:     "image_url",
						ImageURL: &openAIImgURL{URL: dataURI},
					})
				}
			case "image_url":
				// Already in OpenAI format (image_url block).
				if b.ImageURL != nil {
					imageParts = append(imageParts, openAIContentPart{
						Type:     "image_url",
						ImageURL: &openAIImgURL{URL: b.ImageURL.URL, Detail: b.ImageURL.Detail},
					})
				}
			}
		}

		// Assistant message with tool calls.
		if m.Role == "assistant" {
			msg := openAIMessage{Role: "assistant"}
			if textParts != "" {
				msg.Content = textParts
			}
			if len(toolCalls) > 0 {
				msg.ToolCalls = toolCalls
			}
			if thinkingParts != "" {
				msg.ReasoningContent = thinkingParts
			}
			out = append(out, msg)
			continue
		}

		// Tool result messages (role=user with tool_result blocks → separate "tool" messages).
		if len(toolResults) > 0 {
			for _, tr := range toolResults {
				out = append(out, openAIMessage{
					Role:       "tool",
					Content:    tr.Content,
					ToolCallID: tr.ToolUseID,
				})
			}
			// After normalization/merge, a message may contain both tool_results
			// and text. Emit remaining text as a separate user message.
			if textParts != "" {
				out = append(out, openAIMessage{Role: m.Role, Content: textParts})
			}
			continue
		}

		// If message has images, use multipart content array.
		if len(imageParts) > 0 {
			var parts []openAIContentPart
			if textParts != "" {
				parts = append(parts, openAIContentPart{Type: "text", Text: textParts})
			}
			parts = append(parts, imageParts...)
			out = append(out, openAIMessage{Role: m.Role, Content: parts})
			continue
		}

		// Default: user/other message with text only.
		if textParts != "" {
			out = append(out, openAIMessage{Role: m.Role, Content: textParts})
		}
	}
	return out
}

// applySamplingParams copies optional sampling and thinking parameters to the OpenAI request.
func applySamplingParams(oaiReq *openAIRequest, req *ChatRequest) {
	if req.Temperature != nil {
		oaiReq.Temperature = req.Temperature
	}
	if req.TopP != nil {
		oaiReq.TopP = req.TopP
	}
	if req.FrequencyPenalty != nil {
		oaiReq.FrequencyPenalty = req.FrequencyPenalty
	}
	if req.PresencePenalty != nil {
		oaiReq.PresencePenalty = req.PresencePenalty
	}
	if len(req.StopSequences) > 0 {
		oaiReq.Stop = req.StopSequences
	}
	if req.ResponseFormat != nil {
		oaiReq.ResponseFormat = req.ResponseFormat
	}
	if req.ToolChoice != nil {
		oaiReq.ToolChoice = req.ToolChoice
	}

	// Map extended thinking config to OpenAI reasoning_effort.
	if req.Thinking != nil && req.Thinking.Type == "enabled" && req.Thinking.BudgetTokens > 0 {
		switch {
		case req.Thinking.BudgetTokens <= 4096:
			oaiReq.ReasoningEffort = "low"
		case req.Thinking.BudgetTokens <= 10240:
			oaiReq.ReasoningEffort = "medium"
		default:
			oaiReq.ReasoningEffort = "high"
		}
		// Reasoning models require max_completion_tokens instead of max_tokens.
		oaiReq.MaxCompletionTokens = &oaiReq.MaxTokens
		oaiReq.MaxTokens = 0
	}
}

// marshalMessageStart builds a serialized MessageStart payload with optional input token count.
func marshalMessageStart(id, model string, inputTokens int) json.RawMessage {
	p, _ := json.Marshal(MessageStart{
		Message: struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}{
			ID:    id,
			Model: model,
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{
				InputTokens: inputTokens,
			},
		},
	})
	return p
}

// mapFinishReason translates an OpenAI finish reason to an Anthropic stop reason.
func mapFinishReason(reason string) string {
	switch reason {
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "content_filter":
		return "content_filtered"
	default:
		return "end_turn"
	}
}

// translateOpenAIStream reads OpenAI SSE chunks from rawEvents and emits
// Anthropic-style StreamEvents to out.
func (c *Client) translateOpenAIStream(ctx context.Context, rawEvents <-chan StreamEvent, out chan<- StreamEvent) {
	firstChunk := true
	nextBlockIndex := 0
	textBlockOpen := false
	textBlockIndex := -1
	thinkingBlockOpen := false

	type toolBuilder struct {
		id       string
		name     string
		args     []byte
		blockIdx int
	}
	toolBuilders := map[int]*toolBuilder{}

	closeBlock := func(idx int) {
		p, _ := json.Marshal(ContentBlockStop{Index: idx})
		emit(ctx, out, StreamEvent{Type: "content_block_stop", Payload: p})
	}

	emitDelta := func(idx int, deltaType, text, partialJSON string) {
		var cbd ContentBlockDelta
		cbd.Index = idx
		cbd.Delta.Type = deltaType
		cbd.Delta.Text = text
		cbd.Delta.PartialJSON = partialJSON
		p, _ := json.Marshal(cbd)
		emit(ctx, out, StreamEvent{Type: "content_block_delta", Payload: p})
	}

	for raw := range rawEvents {
		// OpenAI sends "data: [DONE]" as the final event.
		if string(raw.Payload) == "[DONE]" {
			emit(ctx, out, StreamEvent{Type: "message_stop"})
			return
		}

		// Handle SSE error events from OpenAI-compatible providers.
		if raw.Type == "error" {
			emit(ctx, out, StreamEvent{Type: "error", Payload: raw.Payload})
			return
		}

		var chunk openAIChunk
		if err := json.Unmarshal(raw.Payload, &chunk); err != nil {
			// Try parsing as an OpenAI error response ({"error": {...}}).
			var errResp struct {
				Error struct {
					Message string `json:"message"`
					Type    string `json:"type"`
				} `json:"error"`
			}
			if json.Unmarshal(raw.Payload, &errResp) == nil && errResp.Error.Message != "" {
				errPayload, _ := json.Marshal(map[string]string{
					"type":    errResp.Error.Type,
					"message": errResp.Error.Message,
				})
				emit(ctx, out, StreamEvent{Type: "error", Payload: errPayload})
				return
			}
			c.logger.Warn("skipping unparseable OpenAI stream chunk",
				"error", err, "payload", string(raw.Payload))
			continue
		}

		// Emit synthetic message_start on first chunk.
		if firstChunk {
			firstChunk = false
			emit(ctx, out, StreamEvent{
				Type:    "message_start",
				Payload: marshalMessageStart(chunk.ID, chunk.Model, 0),
			})
		}

		if len(chunk.Choices) == 0 {
			// Usage-only chunk (OpenAI sends this at the end with stream_options).
			// Re-emit message_start with accurate input tokens, plus message_delta
			// with output tokens, so consumeStream picks up correct usage.
			if chunk.Usage != nil {
				if chunk.Usage.PromptTokens > 0 {
					emit(ctx, out, StreamEvent{
						Type:    "message_start",
						Payload: marshalMessageStart(chunk.ID, chunk.Model, chunk.Usage.PromptTokens),
					})
				}

				// Only emit usage — do NOT emit a stop_reason here.
				// The real stop_reason was already emitted by the choice chunk
				// with FinishReason (mapped tool_calls→tool_use, stop→end_turn).
				// Emitting "end_turn" here would overwrite a prior "tool_use".
				mdPayload, _ := json.Marshal(MessageDelta{
					Usage: struct {
						OutputTokens int `json:"output_tokens"`
					}{OutputTokens: chunk.Usage.CompletionTokens},
				})
				emit(ctx, out, StreamEvent{Type: "message_delta", Payload: mdPayload})
			}
			continue
		}

		choice := chunk.Choices[0]

		// Emit reasoning content as thinking block (OpenAI reasoning models).
		if choice.Delta.ReasoningContent != "" {
			if !thinkingBlockOpen {
				thinkingBlockOpen = true
				p, _ := json.Marshal(ContentBlockStart{
					Index:        0,
					ContentBlock: ContentBlock{Type: "thinking"},
				})
				emit(ctx, out, StreamEvent{Type: "content_block_start", Payload: p})
				if nextBlockIndex == 0 {
					nextBlockIndex = 1 // reserve 0 for thinking
				}
			}
			emitDelta(0, "thinking_delta", choice.Delta.ReasoningContent, "")
		}

		// Emit text delta — open text block lazily on first text content.
		if choice.Delta.Content != "" {
			// Close thinking block if transitioning to text.
			if thinkingBlockOpen && !textBlockOpen {
				thinkingBlockOpen = false
				closeBlock(0)
			}
			if !textBlockOpen {
				textBlockOpen = true
				textBlockIndex = nextBlockIndex
				p, _ := json.Marshal(ContentBlockStart{
					Index:        textBlockIndex,
					ContentBlock: ContentBlock{Type: "text"},
				})
				emit(ctx, out, StreamEvent{Type: "content_block_start", Payload: p})
				nextBlockIndex++
			}
			emitDelta(textBlockIndex, "text_delta", choice.Delta.Content, "")
		}

		// Handle streamed tool calls.
		for _, tc := range choice.Delta.ToolCalls {
			tb, exists := toolBuilders[tc.Index]
			if !exists {
				// Close thinking block before tool calls if open.
				if thinkingBlockOpen {
					thinkingBlockOpen = false
					closeBlock(0)
				}
				// Close text block before first tool call if open.
				if textBlockOpen {
					textBlockOpen = false
					closeBlock(textBlockIndex)
				}

				// New tool call — emit content_block_start for tool_use.
				tb = &toolBuilder{id: tc.ID, name: tc.Function.Name, blockIdx: nextBlockIndex}
				toolBuilders[tc.Index] = tb

				p, _ := json.Marshal(ContentBlockStart{
					Index: nextBlockIndex,
					ContentBlock: ContentBlock{
						Type: "tool_use",
						ID:   tc.ID,
						Name: tc.Function.Name,
					},
				})
				emit(ctx, out, StreamEvent{Type: "content_block_start", Payload: p})
				nextBlockIndex++
			} else {
				// Update name/id if provided in subsequent chunks.
				if tc.ID != "" {
					tb.id = tc.ID
				}
				if tc.Function.Name != "" {
					tb.name = tc.Function.Name
				}
			}

			// Accumulate argument fragments and emit as input_json_delta.
			if tc.Function.Arguments != "" {
				tb.args = append(tb.args, tc.Function.Arguments...)
				emitDelta(tb.blockIdx, "input_json_delta", "", tc.Function.Arguments)
			}
		}

		// Check finish reason (nil = not yet finished, non-nil = terminal).
		if choice.FinishReason != nil {
			// Close thinking block if still open.
			if thinkingBlockOpen {
				thinkingBlockOpen = false
				closeBlock(0)
			}

			// Close text block if still open.
			if textBlockOpen {
				textBlockOpen = false
				closeBlock(textBlockIndex)
			}

			// Close all open tool_use blocks.
			for _, tb := range toolBuilders {
				closeBlock(tb.blockIdx)
			}

			outputTokens := 0
			if chunk.Usage != nil {
				outputTokens = chunk.Usage.CompletionTokens

				// Some providers bundle usage on the finish_reason chunk
				// instead of (or in addition to) a separate usage-only chunk.
				// Re-emit corrected message_start so consumeStream captures InputTokens.
				if chunk.Usage.PromptTokens > 0 {
					emit(ctx, out, StreamEvent{
						Type:    "message_start",
						Payload: marshalMessageStart(chunk.ID, chunk.Model, chunk.Usage.PromptTokens),
					})
				}
			}

			mdPayload, _ := json.Marshal(MessageDelta{
				Delta: struct {
					StopReason string `json:"stop_reason"`
				}{StopReason: mapFinishReason(*choice.FinishReason)},
				Usage: struct {
					OutputTokens int `json:"output_tokens"`
				}{OutputTokens: outputTokens},
			})
			emit(ctx, out, StreamEvent{Type: "message_delta", Payload: mdPayload})
		}
	}

	// Stream ended without [DONE] — emit stop events.
	emit(ctx, out, StreamEvent{Type: "message_stop"})
}

func emit(ctx context.Context, ch chan<- StreamEvent, ev StreamEvent) {
	select {
	case ch <- ev:
	case <-ctx.Done():
	}
}

func setOpenAIBearerAuth(req *http.Request, apiKey string) {
	if strings.TrimSpace(apiKey) == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
}

// interleavedEnabled reports whether the request opts into Anthropic's
// interleaved thinking beta. Centralised so message conversion and header
// emission stay in lock-step.
func interleavedEnabled(req *ChatRequest) bool {
	return req != nil && req.Thinking != nil && req.Thinking.Type == "enabled" && req.Thinking.Interleaved
}

// betaHeaderInterleavedThinking is the Anthropic beta flag enabling
// thinking blocks between tool calls within a single turn.
const betaHeaderInterleavedThinking = "interleaved-thinking-2025-05-14"

// setBetaHeaders attaches `anthropic-beta` to the outgoing HTTP request,
// merging caller-supplied flags with auto-derived ones (currently:
// interleaved thinking). De-duplicated; empty when no flags apply so
// non-Anthropic providers see no extra header.
func setBetaHeaders(httpReq *http.Request, req *ChatRequest) {
	if req == nil {
		return
	}
	flags := make([]string, 0, len(req.BetaHeaders)+1)
	seen := make(map[string]struct{}, len(req.BetaHeaders)+1)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		flags = append(flags, v)
	}
	for _, v := range req.BetaHeaders {
		add(v)
	}
	if interleavedEnabled(req) {
		add(betaHeaderInterleavedThinking)
	}
	if len(flags) == 0 {
		return
	}
	httpReq.Header.Set("anthropic-beta", strings.Join(flags, ","))
}

// Complete sends a single-turn request and returns the assistant text.
// Intended for lightweight tasks (thread titles, classifiers).
//
// Dispatches by client API mode:
//   - openai: non-streaming POST /chat/completions
//   - anthropic: streaming POST /v1/messages, concatenated text deltas
//
// The streaming reuse for anthropic keeps a single wire path; the upstream
// HTTP cost is the same and the caller still sees a synchronous string.
func (c *Client) Complete(ctx context.Context, req ChatRequest) (string, error) {
	if c.apiMode == APIModeAnthropic {
		return c.completeViaStream(ctx, req)
	}
	return c.completeOpenAI(ctx, req)
}

// completeViaStream consumes the streaming chat as a one-shot Complete,
// concatenating text deltas. Used for Anthropic-mode clients where
// /v1/messages does not have a non-streaming sibling endpoint.
func (c *Client) completeViaStream(ctx context.Context, req ChatRequest) (string, error) {
	events, err := c.StreamChat(ctx, req)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for ev := range events {
		if ev.Type != "content_block_delta" {
			continue
		}
		var cbd ContentBlockDelta
		if json.Unmarshal(ev.Payload, &cbd) != nil {
			continue
		}
		if cbd.Delta.Type == "text_delta" {
			sb.WriteString(cbd.Delta.Text)
		}
	}
	out := strings.TrimSpace(sb.String())
	out = jsonutil.StripThinkingTags(out)
	out = jsonutil.StripThinkingPreamble(out)
	return strings.TrimSpace(out), nil
}

// completeOpenAI sends a non-streaming request to an OpenAI-compatible
// /chat/completions endpoint and returns the full response text.
func (c *Client) completeOpenAI(ctx context.Context, req ChatRequest) (string, error) {
	oaiReq := openAIRequest{
		Model:     req.Model,
		Stream:    false,
		MaxTokens: req.MaxTokens,
	}

	// System prompt → system message.
	if systemText := ExtractSystemText(req.System); systemText != "" {
		oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
			Role:    "system",
			Content: systemText,
		})
	}

	// User messages (text only — title generation doesn't need multimodal).
	for _, m := range req.Messages {
		var text string
		if err := json.Unmarshal(m.Content, &text); err == nil {
			oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
				Role:    m.Role,
				Content: text,
			})
		}
	}

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return "", fmt.Errorf("marshal openai request: %w", err)
	}

	// Merge ExtraBody fields (e.g., local AI's chat_template_kwargs, timeout).
	if len(req.ExtraBody) > 0 {
		body, err = mergeJSONFields(body, req.ExtraBody)
		if err != nil {
			return "", fmt.Errorf("merge extra body: %w", err)
		}
	}

	url := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	setOpenAIBearerAuth(httpReq, c.apiKey)

	respBody, err := c.DoStream(ctx, httpReq)
	if err != nil {
		return "", err
	}
	defer respBody.Close()

	data, err := io.ReadAll(io.LimitReader(respBody, 64*1024))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	// Strip reasoning model artifacts (<think> tags, "Thinking Process:" preamble)
	// that leak into the content field of some local models (DeepSeek-R1, QwQ, etc.).
	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	content = jsonutil.StripThinkingTags(content)
	content = jsonutil.StripThinkingPreamble(content)
	return strings.TrimSpace(content), nil
}

// mergeJSONFields merges extra key-value pairs into a JSON object.
func mergeJSONFields(base []byte, extra map[string]any) ([]byte, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(base, &obj); err != nil {
		return nil, err
	}
	for k, v := range extra {
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		obj[k] = raw
	}
	return json.Marshal(obj)
}

// truncateForLog truncates s to maxLen bytes for safe inclusion in log messages.
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
