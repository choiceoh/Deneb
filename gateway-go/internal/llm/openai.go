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

// StreamChat sends a streaming chat request to an OpenAI-compatible
// /chat/completions endpoint and translates the response into the same
// StreamEvent types that consumeStream expects (message_start,
// content_block_start, content_block_delta, content_block_stop,
// message_delta, message_stop).
//
// This enables RunAgent to work with any OpenAI-compatible provider
// (z.ai, localai, vLLM, etc.) without changes to the agent loop.
func (c *Client) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	req.Stream = true

	// Normalize messages: merge consecutive same-role messages that may
	// arise from mid-loop compaction or post-compaction restoration.
	// Applied right before the API call so callers' slices stay untouched.
	req.Messages = NormalizeMessages(req.Messages)

	// Build OpenAI-format request body.
	oaiReq := openAIRequest{
		Model:         req.Model,
		Stream:        true,
		StreamOptions: &openAIStreamOpts{IncludeUsage: true}, // #6: request usage data in stream
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

	// Convert system prompt to a system message.
	// Supports both string and array-of-blocks formats.
	systemText := ExtractSystemText(req.System)
	if systemText != "" {
		oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
			Role:    "system",
			Content: systemText,
		})
	}

	// Convert messages (handles plain text, tool_use, tool_result, and image blocks).
	for _, m := range req.Messages {
		// Try plain text string first.
		var text string
		if err := json.Unmarshal(m.Content, &text); err == nil {
			oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
				Role:    m.Role,
				Content: text,
			})
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
		var toolCalls []openAIToolCall
		var toolResults []ContentBlock
		var imageParts []openAIContentPart
		for _, b := range blocks {
			switch b.Type {
			case "text":
				textParts += b.Text
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
				// #7: Anthropic image block (base64) → OpenAI image_url with data URI.
				if b.Source != nil && b.Source.Data != "" {
					dataURI := "data:" + b.Source.MediaType + ";base64," + b.Source.Data
					imageParts = append(imageParts, openAIContentPart{
						Type:     "image_url",
						ImageURL: &openAIImgURL{URL: dataURI},
					})
				}
			case "image_url":
				// #7: Already in OpenAI format (image_url block).
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
			oaiReq.Messages = append(oaiReq.Messages, msg)
			continue
		}

		// Tool result messages (role=user with tool_result blocks → separate "tool" messages).
		if len(toolResults) > 0 {
			for _, tr := range toolResults {
				oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
					Role:       "tool",
					Content:    tr.Content,
					ToolCallID: tr.ToolUseID,
				})
			}
			// After normalization/merge, a message may contain both tool_results
			// and text (e.g., tool results + restoration context). Emit any
			// remaining text as a separate user message so it isn't silently dropped.
			if textParts != "" {
				oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
					Role:    m.Role,
					Content: textParts,
				})
			}
			continue
		}

		// #7: If message has images, use multipart content array.
		if len(imageParts) > 0 {
			var parts []openAIContentPart
			if textParts != "" {
				parts = append(parts, openAIContentPart{Type: "text", Text: textParts})
			}
			parts = append(parts, imageParts...)
			oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
				Role:    m.Role,
				Content: parts,
			})
			continue
		}

		// Default: user/other message with text only.
		if textParts != "" {
			oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
				Role:    m.Role,
				Content: textParts,
			})
		}
	}

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

		firstChunk := true
		// nextBlockIndex tracks the Anthropic-style content block index.
		// Block 0 is reserved for thinking (if present), then text, then tools.
		nextBlockIndex := 0
		textBlockOpen := false
		textBlockIndex := -1 // assigned when text block is opened
		thinkingBlockOpen := false

		// toolBuilders accumulates streamed tool call fragments by OpenAI tool index.
		type toolBuilder struct {
			id       string
			name     string
			args     []byte
			blockIdx int // assigned Anthropic-style block index
		}
		toolBuilders := map[int]*toolBuilder{}

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
			}

			if len(chunk.Choices) == 0 {
				// #8: Usage-only chunk (OpenAI sends this at the end with stream_options).
				// Re-emit message_start with accurate input tokens, plus message_delta
				// with output tokens, so consumeStream picks up correct usage.
				if chunk.Usage != nil {
					if chunk.Usage.PromptTokens > 0 {
						correctedStart, _ := json.Marshal(MessageStart{
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
								Usage: struct {
									InputTokens  int `json:"input_tokens"`
									OutputTokens int `json:"output_tokens"`
								}{
									InputTokens: chunk.Usage.PromptTokens,
								},
							},
						})
						emit(ctx, out, StreamEvent{Type: "message_start", Payload: correctedStart})
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
				thinkingBlockIdx := 0 // thinking is always block 0
				if !thinkingBlockOpen {
					thinkingBlockOpen = true
					cbsPayload, _ := json.Marshal(ContentBlockStart{
						Index:        thinkingBlockIdx,
						ContentBlock: ContentBlock{Type: "thinking"},
					})
					emit(ctx, out, StreamEvent{Type: "content_block_start", Payload: cbsPayload})
					if nextBlockIndex == 0 {
						nextBlockIndex = 1 // reserve 0 for thinking
					}
				}
				cbdPayload, _ := json.Marshal(ContentBlockDelta{
					Index: thinkingBlockIdx,
					Delta: struct {
						Type        string `json:"type"`
						Text        string `json:"text,omitempty"`
						PartialJSON string `json:"partial_json,omitempty"`
					}{
						Type: "thinking_delta",
						Text: choice.Delta.ReasoningContent,
					},
				})
				emit(ctx, out, StreamEvent{Type: "content_block_delta", Payload: cbdPayload})
			}

			// Emit text delta — open text block lazily on first text content.
			if choice.Delta.Content != "" {
				// Close thinking block if transitioning to text.
				if thinkingBlockOpen && !textBlockOpen {
					thinkingBlockOpen = false
					cbStopPayload, _ := json.Marshal(ContentBlockStop{Index: 0})
					emit(ctx, out, StreamEvent{Type: "content_block_stop", Payload: cbStopPayload})
				}
				if !textBlockOpen {
					textBlockOpen = true
					textBlockIndex = nextBlockIndex
					cbsPayload, _ := json.Marshal(ContentBlockStart{
						Index:        textBlockIndex,
						ContentBlock: ContentBlock{Type: "text"},
					})
					emit(ctx, out, StreamEvent{Type: "content_block_start", Payload: cbsPayload})
					nextBlockIndex++
				}
				cbdPayload, _ := json.Marshal(ContentBlockDelta{
					Index: textBlockIndex,
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

			// Handle streamed tool calls.
			// toolBlockBase maps OpenAI tool call indices to our block indices.
			for _, tc := range choice.Delta.ToolCalls {
				tb, exists := toolBuilders[tc.Index]
				if !exists {
					// Close thinking block before tool calls if open.
					if thinkingBlockOpen {
						thinkingBlockOpen = false
						cbStopPayload, _ := json.Marshal(ContentBlockStop{Index: 0})
						emit(ctx, out, StreamEvent{Type: "content_block_stop", Payload: cbStopPayload})
					}
					// Close text block before first tool call if open.
					if textBlockOpen {
						textBlockOpen = false
						cbStopPayload, _ := json.Marshal(ContentBlockStop{Index: textBlockIndex})
						emit(ctx, out, StreamEvent{Type: "content_block_stop", Payload: cbStopPayload})
					}

					// New tool call — emit content_block_start for tool_use.
					tb = &toolBuilder{id: tc.ID, name: tc.Function.Name, blockIdx: nextBlockIndex}
					toolBuilders[tc.Index] = tb

					cbsPayload, _ := json.Marshal(ContentBlockStart{
						Index: nextBlockIndex,
						ContentBlock: ContentBlock{
							Type: "tool_use",
							ID:   tc.ID,
							Name: tc.Function.Name,
						},
					})
					emit(ctx, out, StreamEvent{Type: "content_block_start", Payload: cbsPayload})
					tb.args = nil
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
					cbdPayload, _ := json.Marshal(ContentBlockDelta{
						Index: tb.blockIdx,
						Delta: struct {
							Type        string `json:"type"`
							Text        string `json:"text,omitempty"`
							PartialJSON string `json:"partial_json,omitempty"`
						}{
							Type:        "input_json_delta",
							PartialJSON: tc.Function.Arguments,
						},
					})
					emit(ctx, out, StreamEvent{Type: "content_block_delta", Payload: cbdPayload})
				}
			}

			// Check finish reason (nil = not yet finished, non-nil = terminal).
			if choice.FinishReason != nil {
				// Close thinking block if still open.
				if thinkingBlockOpen {
					thinkingBlockOpen = false
					cbStopPayload, _ := json.Marshal(ContentBlockStop{Index: 0})
					emit(ctx, out, StreamEvent{Type: "content_block_stop", Payload: cbStopPayload})
				}

				// Close text block if still open.
				if textBlockOpen {
					textBlockOpen = false
					cbStopPayload, _ := json.Marshal(ContentBlockStop{Index: textBlockIndex})
					emit(ctx, out, StreamEvent{Type: "content_block_stop", Payload: cbStopPayload})
				}

				// Close all open tool_use blocks.
				for _, tb := range toolBuilders {
					cbStopPayload, _ := json.Marshal(ContentBlockStop{Index: tb.blockIdx})
					emit(ctx, out, StreamEvent{Type: "content_block_stop", Payload: cbStopPayload})
				}

				// Map OpenAI finish reasons to Anthropic stop reasons.
				stopReason := "end_turn"
				switch *choice.FinishReason {
				case "length":
					stopReason = "max_tokens"
				case "stop":
					stopReason = "end_turn"
				case "tool_calls", "function_call":
					stopReason = "tool_use"
				case "content_filter":
					stopReason = "content_filtered"
				}

				outputTokens := 0
				if chunk.Usage != nil {
					outputTokens = chunk.Usage.CompletionTokens

					// Some providers bundle usage on the finish_reason chunk
					// instead of (or in addition to) a separate usage-only chunk.
					// Re-emit corrected message_start so consumeStream captures InputTokens.
					if chunk.Usage.PromptTokens > 0 {
						correctedStart, _ := json.Marshal(MessageStart{
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
								Usage: struct {
									InputTokens  int `json:"input_tokens"`
									OutputTokens int `json:"output_tokens"`
								}{
									InputTokens: chunk.Usage.PromptTokens,
								},
							},
						})
						emit(ctx, out, StreamEvent{Type: "message_start", Payload: correctedStart})
					}
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

func setOpenAIBearerAuth(req *http.Request, apiKey string) {
	if strings.TrimSpace(apiKey) == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
}

// Complete sends a non-streaming request to an OpenAI-compatible
// /chat/completions endpoint and returns the full response text.
// Intended for lightweight single-turn tasks (e.g. thread title generation).
func (c *Client) Complete(ctx context.Context, req ChatRequest) (string, error) {
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

// --- OpenAI request/response types ---

type openAIRequest struct {
	Model               string            `json:"model"`
	Messages            []openAIMessage   `json:"messages"`
	Stream              bool              `json:"stream"`
	StreamOptions       *openAIStreamOpts `json:"stream_options,omitempty"`
	MaxTokens           int               `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int              `json:"max_completion_tokens,omitempty"` // for reasoning models
	Temperature         *float64          `json:"temperature,omitempty"`
	TopP                *float64          `json:"top_p,omitempty"`
	FrequencyPenalty    *float64          `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64          `json:"presence_penalty,omitempty"`
	Stop                []string          `json:"stop,omitempty"`
	Tools               []openAITool      `json:"tools,omitempty"`
	ToolChoice          any               `json:"tool_choice,omitempty"`
	ResponseFormat      *ResponseFormat   `json:"response_format,omitempty"`
	ReasoningEffort     string            `json:"reasoning_effort,omitempty"` // "low", "medium", "high"
}

// openAIStreamOpts controls streaming behavior.
type openAIStreamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

// openAITool wraps a function definition in OpenAI's tool format.
type openAITool struct {
	Type     string         `json:"type"` // always "function"
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// openAIMessage represents a message in the OpenAI chat format.
// Content is any because it can be a string, []openAIContentPart (for vision),
// or nil (marshals to JSON null, required by OpenAI for tool-only assistant messages).
type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`                // string, []openAIContentPart, or nil (→ null)
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`   // assistant tool calls
	ToolCallID string           `json:"tool_call_id,omitempty"` // tool result reference
}

// openAIContentPart is a single part in a multipart content array (text or image_url).
type openAIContentPart struct {
	Type     string        `json:"type"`                // "text" or "image_url"
	Text     string        `json:"text,omitempty"`      // for type="text"
	ImageURL *openAIImgURL `json:"image_url,omitempty"` // for type="image_url"
}

// openAIImgURL holds the URL (or data URI) for an image content part.
type openAIImgURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "auto", "low", "high"
}

// openAIToolCall represents a tool call in an assistant message.
type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int         `json:"index"`
		Delta        openAIDelta `json:"delta"`
		FinishReason *string     `json:"finish_reason"` // pointer: null → nil, "stop" → &"stop"
	} `json:"choices"`
	Usage *openAIUsage `json:"usage,omitempty"`
}

type openAIDelta struct {
	Role             string                `json:"role,omitempty"`
	Content          string                `json:"content,omitempty"`
	ReasoningContent string                `json:"reasoning_content,omitempty"` // reasoning model thinking
	Refusal          string                `json:"refusal,omitempty"`           // model refusal
	ToolCalls        []openAIDeltaToolCall `json:"tool_calls,omitempty"`
}

// openAIDeltaToolCall is a streamed fragment of a tool call.
type openAIDeltaToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type openAIUsage struct {
	PromptTokens            int                      `json:"prompt_tokens"`
	CompletionTokens        int                      `json:"completion_tokens"`
	TotalTokens             int                      `json:"total_tokens"`
	CompletionTokensDetails *completionTokensDetails `json:"completion_tokens_details,omitempty"`
}

type completionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// truncateForLog truncates s to maxLen bytes for safe inclusion in log messages.
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
