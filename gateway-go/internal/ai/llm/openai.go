package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelcaps"
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
			// A turn whose only content was thinking blocks (dropped above when
			// preserveThinking is off) would serialize as
			// {"role":"assistant","content":null} — a contentless assistant
			// message that some chat templates reject and models misread as
			// "I replied with nothing". Skip it entirely; no tool_use means no
			// later tool_result can orphan.
			if textParts == "" && len(toolCalls) == 0 && thinkingParts == "" {
				c.logger.Debug("skipping assistant message with no convertible content")
				continue
			}
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
		// Dual-mode vLLM models (DeepSeek V4 family) accept a real per-request
		// off-switch through chat_template_kwargs; when the capability layer
		// named the kwarg, use it and send no reasoning_effort at all.
		if req.Thinking.TemplateKwarg != "" {
			oaiReq.ChatTemplateKwargs = map[string]any{req.Thinking.TemplateKwarg: false}
			return
		}
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
