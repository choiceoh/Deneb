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

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelcaps"
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
	c.setOpenAIBearerAuth(httpReq)
	setBetaHeaders(httpReq, &req)
	c.applyHeaders(httpReq)

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
		// Empty (0-byte) Content has nothing to convert — skip it without the
		// unparseable-content warning below, which would otherwise fire on
		// every API call for the rest of the run. Message factories guarantee
		// valid JSON Content (see marshalBlocks), so this is defense in depth;
		// a tool_use-bearing message can no longer arrive here empty, hence
		// skipping cannot orphan a later tool_result.
		if len(m.Content) == 0 {
			c.logger.Debug("skipping message with empty content", "role", m.Role)
			continue
		}

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
	if req.TopK != nil {
		oaiReq.TopK = req.TopK
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
		// Genuine OpenAI reasoning models (o-series, gpt-5) require
		// max_completion_tokens and reject max_tokens. OpenAI-compatible
		// servers (self-hosted vLLM, etc.) keep max_tokens and 400 on
		// max_tokens=0 ("max_tokens must be at least 1, got 0"), so only
		// remap for models that actually use the reasoning endpoint.
		if isOpenAIReasoningModel(req.Model) {
			// Copy the value before zeroing — aliasing &oaiReq.MaxTokens would
			// drag max_completion_tokens to 0 along with max_tokens.
			maxCompletion := oaiReq.MaxTokens
			oaiReq.MaxCompletionTokens = &maxCompletion
			oaiReq.MaxTokens = 0
		}
	} else if req.Thinking != nil && req.Thinking.Type == "disabled" {
		// Minimize reasoning on openai-compatible reasoning models. step3p7 cannot
		// actually disable thinking (its chat template force-opens every turn with
		// <think>), so "disabled" maps to the effort level that empirically yields
		// the SHORTEST chain-of-thought. vLLM accepts reasoning_effort in
		// {none, minimal, low, medium, high}; counter-intuitively "low" — not
		// "minimal" or "none" — is the floor. Measured over N=4 deterministic
		// samples on a real analysis prompt (chars of reasoning):
		//   low: 2648/3480/4022 (min/mean/max, non-overlapping below all others)
		//   minimal: 3211/4504/6175   none: 5097/6641/10052   med/high: ~6000+.
		// "none" and "minimal" both reason ~2x more than "low" and are noisier.
		// Without any value the model emits a multi-thousand-char chain-of-thought
		// that eats the max_tokens budget (truncating the real answer). Even at
		// "low" the budget must be generous — step3p7 still spends ~2500 reasoning
		// tokens. anthropic.go sends the native {"type":"disabled"} for the GLM
		// path; this is the openai-compatible equivalent.
		oaiReq.ReasoningEffort = "low"
	}
}

// isOpenAIReasoningModel reports whether model is a genuine OpenAI reasoning
// model that requires max_completion_tokens instead of max_tokens. The
// heuristic lives in modelcaps so the capability registry and this wire-level
// remap share one definition.
func isOpenAIReasoningModel(model string) bool {
	return modelcaps.IsOpenAIReasoningModel(model)
}

// marshalMessageStart builds a serialized MessageStart payload with optional input token count.
func marshalMessageStart(id, model string, inputTokens int) json.RawMessage {
	p, _ := json.Marshal(MessageStart{
		Message: struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Usage struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
			} `json:"usage"`
		}{
			ID:    id,
			Model: model,
			Usage: struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
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
	thinkingBlockIndex := -1

	type toolBuilder struct {
		id       string
		name     string
		args     []byte
		blockIdx int
	}
	toolBuilders := map[int]*toolBuilder{}
	var toolOrder []int // tool-call indices in first-seen order, for deterministic contiguous emission at finish

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

	// emitText routes a string into the (lazily opened) text block, closing an
	// open thinking block first so the single-active-block consumer doesn't
	// discard it. Shared by streamed content and surfaced model refusals.
	emitText := func(s string) {
		if thinkingBlockOpen {
			thinkingBlockOpen = false
			closeBlock(thinkingBlockIndex)
		}
		if !textBlockOpen {
			textBlockOpen = true
			textBlockIndex = nextBlockIndex
			nextBlockIndex++
			start, _ := json.Marshal(ContentBlockStart{
				Index:        textBlockIndex,
				ContentBlock: ContentBlock{Type: "text"},
			})
			emit(ctx, out, StreamEvent{Type: "content_block_start", Payload: start})
		}
		emitDelta(textBlockIndex, "text_delta", s, "")
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
						OutputTokens             int `json:"output_tokens"`
						CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
						CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
					}{OutputTokens: chunk.Usage.CompletionTokens},
				})
				emit(ctx, out, StreamEvent{Type: "message_delta", Payload: mdPayload})
			}
			continue
		}

		choice := chunk.Choices[0]

		// Emit reasoning content as a thinking block (OpenAI/vLLM reasoning models).
		if rtext := choice.Delta.reasoningText(); rtext != "" {
			if !thinkingBlockOpen {
				// Close an already-open text block first. Reasoning normally
				// precedes text, but some providers emit content before reasoning;
				// opening thinking over an un-stopped text block makes the
				// single-active-block consumer discard the text, and a hardcoded
				// index 0 would collide with the text block already at 0. Give the
				// thinking block its own index instead.
				if textBlockOpen {
					textBlockOpen = false
					closeBlock(textBlockIndex)
				}
				thinkingBlockOpen = true
				thinkingBlockIndex = nextBlockIndex
				nextBlockIndex++
				p, _ := json.Marshal(ContentBlockStart{
					Index:        thinkingBlockIndex,
					ContentBlock: ContentBlock{Type: "thinking"},
				})
				emit(ctx, out, StreamEvent{Type: "content_block_start", Payload: p})
			}
			emitDelta(thinkingBlockIndex, "thinking_delta", rtext, "")
		}

		// Emit text content. emitText opens the text block lazily and closes any
		// open thinking block first.
		if choice.Delta.Content != "" {
			emitText(choice.Delta.Content)
		}

		// Surface model refusals. OpenAI streams a refusal on delta.refusal with
		// content null; without this the refusal text is dropped and the user
		// gets an empty reply (a silent no-reply).
		if choice.Delta.Refusal != "" {
			emitText(choice.Delta.Refusal)
		}

		// Accumulate streamed tool calls; emit each as a CONTIGUOUS block at
		// finish (see the finish handler below). OpenAI interleaves argument
		// fragments across tool-call indices and never closes one tool block
		// before opening the next. The consumer (executor.consumeStreamInto)
		// tracks a single active block, so emitting tool deltas live would route
		// a later fragment for index N — arriving after index N+1 started — to
		// the wrong block or drop it, and the un-stopped block N gets overwritten
		// and lost. Buffering and emitting start → full args → stop together per
		// tool keeps every block contiguous and correctly assembled.
		for _, tc := range choice.Delta.ToolCalls {
			tb, exists := toolBuilders[tc.Index]
			if !exists {
				// Close thinking/text block before the first tool call if open.
				if thinkingBlockOpen {
					thinkingBlockOpen = false
					closeBlock(thinkingBlockIndex)
				}
				if textBlockOpen {
					textBlockOpen = false
					closeBlock(textBlockIndex)
				}
				tb = &toolBuilder{id: tc.ID, name: tc.Function.Name, blockIdx: nextBlockIndex}
				toolBuilders[tc.Index] = tb
				toolOrder = append(toolOrder, tc.Index)
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
			tb.args = append(tb.args, tc.Function.Arguments...)
		}

		// Check finish reason (nil = not yet finished, non-nil = terminal).
		if choice.FinishReason != nil {
			// Close thinking block if still open.
			if thinkingBlockOpen {
				thinkingBlockOpen = false
				closeBlock(thinkingBlockIndex)
			}

			// Close text block if still open.
			if textBlockOpen {
				textBlockOpen = false
				closeBlock(textBlockIndex)
			}

			// Emit each accumulated tool_use block contiguously
			// (start → full input_json_delta → stop) in first-seen order, so the
			// single-active-block consumer assembles every call's arguments
			// instead of dropping interleaved or overwritten blocks.
			for _, idx := range toolOrder {
				tb := toolBuilders[idx]
				if tb.id == "" {
					// Some OpenAI-compatible servers stream tool calls without an
					// id. Synthesize one — tool_use↔tool_result pairing and the
					// echo-back to the provider both require a non-empty id.
					tb.id = fmt.Sprintf("call_%d", tb.blockIdx)
				}
				startP, _ := json.Marshal(ContentBlockStart{
					Index:        tb.blockIdx,
					ContentBlock: ContentBlock{Type: "tool_use", ID: tb.id, Name: tb.name},
				})
				emit(ctx, out, StreamEvent{Type: "content_block_start", Payload: startP})
				if len(tb.args) > 0 {
					emitDelta(tb.blockIdx, "input_json_delta", "", string(tb.args))
				}
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
					OutputTokens             int `json:"output_tokens"`
					CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
					CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
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

func (c *Client) setOpenAIBearerAuth(req *http.Request) {
	apiKey := c.resolveAPIKey()
	if apiKey == "" {
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
	c.setOpenAIBearerAuth(httpReq)
	c.applyHeaders(httpReq)

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
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	msg := resp.Choices[0].Message
	if strings.TrimSpace(msg.Content) == "" && msg.Refusal != "" {
		// A refusal arrives on `refusal` with content null. Returning "" with
		// a nil error would let background callers (wiki dreamer/verify/merge)
		// treat the refusal as a successful empty result.
		return "", fmt.Errorf("model refused: %s", truncateForLog(msg.Refusal, 200))
	}
	// Strip reasoning model artifacts (<think> tags, "Thinking Process:" preamble)
	// that leak into the content field of some local models (DeepSeek-R1, QwQ, etc.).
	content := strings.TrimSpace(msg.Content)
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
