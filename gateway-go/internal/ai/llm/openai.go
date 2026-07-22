package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

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

	// Normalize messages: repair tool_use/tool_result pairing broken by
	// pruning/compaction (strict backends 400 on orphans), then merge
	// consecutive same-role messages that may arise from mid-loop compaction
	// or the repair's inserted results. Applied right before the API call so
	// callers' slices stay untouched.
	req.Messages = NormalizeMessages(RepairToolPairing(req.Messages))

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
		Seed:          req.Seed,
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
				Parameters:  json.RawMessage(req.Tools[i].RawInputSchema.Bytes()),
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

	return startSSEPipelineWithByteLimit(ctx, respBody, c.maxStreamBytes, func(ctx context.Context, rawEvents <-chan StreamEvent, out chan<- StreamEvent) {
		c.translateOpenAIStream(ctx, rawEvents, out)
	}), nil
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
		out = append(out, c.convertMessageToOpenAI(m, preserveThinking)...)
	}
	return out
}

// openAIMessageParts is the normalized content of one block-form message.
// Keeping classification separate from role-specific projection makes the
// precedence rules explicit: assistant/tool-result handling wins over images,
// exactly as required by the OpenAI chat message schema.
type openAIMessageParts struct {
	text        string
	thinking    string
	toolCalls   []openAIToolCall
	toolResults []ContentBlock
	images      []openAIContentPart
}

func (c *Client) convertMessageToOpenAI(message Message, preserveThinking bool) []openAIMessage {
	// Empty (0-byte) Content has nothing to convert — skip it without the
	// unparseable-content warning below, which would otherwise repeat on every
	// API call for the rest of the run.
	if message.Content.IsZero() {
		c.logger.Debug("skipping message with empty content", "role", message.Role)
		return nil
	}

	var text string
	if err := json.Unmarshal(message.Content.Bytes(), &text); err == nil {
		return []openAIMessage{{Role: message.Role, Content: text}}
	}

	var blocks []ContentBlock
	if err := json.Unmarshal(message.Content.Bytes(), &blocks); err != nil {
		c.logger.Warn("skipping message with unparseable content",
			"role", message.Role, "error", err,
			"content_preview", truncateForLog(message.Content.String(), 200))
		return nil
	}

	parts := classifyOpenAIMessageBlocks(message.Role, blocks, preserveThinking)
	if message.Role == "assistant" {
		return c.convertAssistantMessage(parts)
	}
	if len(parts.toolResults) > 0 {
		return convertToolResultMessage(message.Role, parts)
	}
	if len(parts.images) > 0 {
		return []openAIMessage{convertMultipartMessage(message.Role, parts)}
	}
	if parts.text == "" {
		return nil
	}
	return []openAIMessage{{Role: message.Role, Content: parts.text}}
}

func classifyOpenAIMessageBlocks(role string, blocks []ContentBlock, preserveThinking bool) openAIMessageParts {
	var parts openAIMessageParts
	for _, block := range blocks {
		switch block.Type {
		case "text":
			parts.text += block.Text
		case "thinking":
			if preserveThinking && role == "assistant" {
				parts.thinking += block.Thinking
			}
		case "tool_use":
			parts.toolCalls = append(parts.toolCalls, openAIToolCallFromBlock(block))
		case "tool_result":
			parts.toolResults = append(parts.toolResults, block)
		case "image", "image_url":
			if image, ok := openAIImagePartFromBlock(block); ok {
				parts.images = append(parts.images, image)
			}
		}
	}
	return parts
}

func openAIToolCallFromBlock(block ContentBlock) openAIToolCall {
	arguments := "{}"
	if !block.Input.IsZero() {
		arguments = block.Input.String()
	}
	call := openAIToolCall{ID: block.ID, Type: "function"}
	call.Function.Name = block.Name
	call.Function.Arguments = arguments
	return call
}

func openAIImagePartFromBlock(block ContentBlock) (openAIContentPart, bool) {
	switch block.Type {
	case "image":
		if block.Source == nil || block.Source.Data == "" {
			return openAIContentPart{}, false
		}
		dataURI := "data:" + block.Source.MediaType + ";base64," + block.Source.Data
		return openAIContentPart{Type: "image_url", ImageURL: &openAIImgURL{URL: dataURI}}, true
	case "image_url":
		if block.ImageURL == nil {
			return openAIContentPart{}, false
		}
		return openAIContentPart{
			Type: "image_url",
			ImageURL: &openAIImgURL{
				URL:    block.ImageURL.URL,
				Detail: block.ImageURL.Detail,
			},
		}, true
	default:
		return openAIContentPart{}, false
	}
}

func (c *Client) convertAssistantMessage(parts openAIMessageParts) []openAIMessage {
	// Dropping non-preserved thinking can leave a contentless assistant turn.
	// Skip it rather than sending content:null without a tool call.
	if parts.text == "" && len(parts.toolCalls) == 0 && parts.thinking == "" {
		c.logger.Debug("skipping assistant message with no convertible content")
		return nil
	}
	message := openAIMessage{
		Role:             "assistant",
		ToolCalls:        parts.toolCalls,
		ReasoningContent: parts.thinking,
	}
	if parts.text != "" {
		message.Content = parts.text
	}
	return []openAIMessage{message}
}

func convertToolResultMessage(role string, parts openAIMessageParts) []openAIMessage {
	messages := make([]openAIMessage, 0, len(parts.toolResults))
	for _, result := range parts.toolResults {
		messages = append(messages, openAIMessage{
			Role:       "tool",
			Content:    result.Content,
			ToolCallID: result.ToolUseID,
		})
	}
	// Normalization can merge a user text block into the same message as tool
	// results. Keep that text as a separate user message after the results.
	if parts.text != "" {
		messages = append(messages, openAIMessage{Role: role, Content: parts.text})
	}
	return messages
}

func convertMultipartMessage(role string, parts openAIMessageParts) openAIMessage {
	content := make([]openAIContentPart, 0, len(parts.images)+1)
	if parts.text != "" {
		content = append(content, openAIContentPart{Type: "text", Text: parts.text})
	}
	content = append(content, parts.images...)
	return openAIMessage{Role: role, Content: content}
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
	if !req.ToolChoice.IsZero() {
		oaiReq.ToolChoice = req.ToolChoice
	}

	// Map extended thinking config to OpenAI reasoning_effort.
	if req.Thinking != nil && req.Thinking.Type == "enabled" && req.Thinking.BudgetTokens > 0 {
		switch {
		case reasoningEffortHighOnly(req.Model):
			// DeepSeek-V4 family (dsv4) only accepts reasoning_effort "high"/"max";
			// "low"/"medium" are rejected. Always send "high" when thinking is on.
			oaiReq.ReasoningEffort = "high"
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

// reasoningEffortHighOnly reports whether the model accepts only "high"/"max" for
// reasoning_effort (rejecting "low"/"medium"). The self-hosted DeepSeek-V4 family
// (dsv4, the main model) is dual-mode — thinking is toggled via chat_template_kwargs
// and, when on, only the high effort levels are valid — so a small thinking budget
// must still send "high" rather than a level the server rejects.
func reasoningEffortHighOnly(model string) bool {
	m := strings.ToLower(model)
	return strings.Contains(m, "deepseek-v4") || strings.Contains(m, "deepseek_v4")
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
// Extra values are already serialized FlexibleJSON payloads.
func mergeJSONFields(base []byte, extra map[string]FlexibleJSON) ([]byte, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(base, &obj); err != nil {
		return nil, err
	}
	for k, v := range extra {
		if v.IsZero() {
			obj[k] = json.RawMessage("null")
			continue
		}
		obj[k] = json.RawMessage(v.Bytes())
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
