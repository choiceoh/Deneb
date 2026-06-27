// openai_stream.go — OpenAI SSE → Anthropic StreamEvent translation for the
// LLM client: synthetic message_start/finish-reason mapping and the chunk
// translation loop that re-emits OpenAI deltas as Anthropic-style events.
// Split from openai.go (pure move, no behavior change).
package llm

import (
	"context"
	"encoding/json"
	"fmt"
)

// marshalMessageStart builds a serialized MessageStart payload with optional
// input and cache-read token counts (Anthropic semantics: input excludes the
// cache-read portion — see openAIUsage.splitPromptTokens).
func marshalMessageStart(id, model string, inputTokens, cacheReadTokens int) json.RawMessage {
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
				InputTokens:          inputTokens,
				CacheReadInputTokens: cacheReadTokens,
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

// probeOpenAIError detects a bare OpenAI error body ({"error":{...}}) and
// repacks it as a flat {"type","message"} error payload. Needed in two spots:
// the unparseable-chunk path, and the choice-less-chunk path — an error body
// unmarshals into openAIChunk with all-zero fields, so without the second
// probe it was swallowed as an empty usage chunk and the turn ended as an
// empty success.
func probeOpenAIError(payload json.RawMessage) (json.RawMessage, bool) {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(payload, &errResp) != nil || errResp.Error.Message == "" {
		return nil, false
	}
	p, _ := json.Marshal(map[string]string{
		"type":    errResp.Error.Type,
		"message": errResp.Error.Message,
	})
	return p, true
}

// translateOpenAIStream reads OpenAI SSE chunks from rawEvents and emits
// Anthropic-style StreamEvents to out.
func (c *Client) translateOpenAIStream(ctx context.Context, rawEvents <-chan StreamEvent, out chan<- StreamEvent) {
	firstChunk := true
	sawFinishReason := false // any non-nil choice finish_reason — a clean-end signal
	chunkCount := 0          // parsed data chunks, for the premature-EOF diagnostic
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

	// closeOpenBlocks stops any in-flight thinking/text block so every block
	// the single-active-block consumer sees stays contiguous.
	closeOpenBlocks := func() {
		if thinkingBlockOpen {
			thinkingBlockOpen = false
			closeBlock(thinkingBlockIndex)
		}
		if textBlockOpen {
			textBlockOpen = false
			closeBlock(textBlockIndex)
		}
	}

	// emitBufferedTools emits each accumulated tool_use block contiguously
	// (start → full input_json_delta → stop) in first-seen order, then clears
	// the buffer. dropInvalidArgs guards the premature-end path: a stream cut
	// mid-arguments must not surface a half-specified call — the consumer's
	// content_block_stop path takes arguments as-is without re-validating.
	emitBufferedTools := func(dropInvalidArgs bool) {
		for _, idx := range toolOrder {
			tb := toolBuilders[idx]
			if dropInvalidArgs && len(tb.args) > 0 && !json.Valid(tb.args) {
				c.logger.Warn("dropping tool call with truncated arguments at premature stream end",
					"tool", tb.name, "argsLen", len(tb.args))
				continue
			}
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
		toolBuilders = map[int]*toolBuilder{}
		toolOrder = nil
	}

	// flushPremature rescues buffered state when the server explicitly ended
	// the stream ([DONE]) without a finish_reason chunk. Open text/thinking
	// blocks are closed and complete buffered tool calls are emitted — without
	// this, a dropped finish chunk silently discarded every tool call of the
	// turn. No stop_reason is synthesized: the consumer executes tools based
	// on block presence, and a fake "tool_use" would misrepresent the cut as a
	// clean stop. After a normal finish chunk the buffers are already empty,
	// so this is a no-op. A bare EOF (no [DONE], no finish_reason) does NOT
	// take this path — that is a connection cut and surfaces as an error
	// event below instead.
	flushPremature := func() {
		closeOpenBlocks()
		if len(toolOrder) == 0 {
			return
		}
		c.logger.Warn("openai stream ended without finish_reason; flushing buffered tool calls",
			"count", len(toolOrder))
		emitBufferedTools(true)
	}

	for raw := range rawEvents {
		// OpenAI sends "data: [DONE]" as the final event.
		if string(raw.Payload) == "[DONE]" {
			flushPremature()
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
			if errPayload, ok := probeOpenAIError(raw.Payload); ok {
				emit(ctx, out, StreamEvent{Type: "error", Payload: errPayload})
				return
			}
			c.logger.Warn("skipping unparseable OpenAI stream chunk",
				"error", err, "payload", string(raw.Payload))
			continue
		}
		chunkCount++

		// Emit synthetic message_start on first chunk.
		if firstChunk {
			firstChunk = false
			emit(ctx, out, StreamEvent{
				Type:    "message_start",
				Payload: marshalMessageStart(chunk.ID, chunk.Model, 0, 0),
			})
		}

		if len(chunk.Choices) == 0 {
			// A bare {"error":{...}} body parses cleanly into a zero-valued
			// openAIChunk, so the unmarshal-error probe above never sees it.
			// Probe again here or the provider's error is swallowed as an
			// empty usage chunk and vanishes from the turn entirely.
			if chunk.Usage == nil {
				if errPayload, ok := probeOpenAIError(raw.Payload); ok {
					emit(ctx, out, StreamEvent{Type: "error", Payload: errPayload})
					return
				}
			}

			// Usage-only chunk (OpenAI sends this at the end with stream_options).
			// Re-emit message_start with accurate input tokens, plus message_delta
			// with output tokens, so consumeStream picks up correct usage.
			if chunk.Usage != nil {
				if chunk.Usage.PromptTokens > 0 {
					input, cached := chunk.Usage.splitPromptTokens()
					emit(ctx, out, StreamEvent{
						Type:    "message_start",
						Payload: marshalMessageStart(chunk.ID, chunk.Model, input, cached),
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
				closeOpenBlocks()
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
			sawFinishReason = true
			closeOpenBlocks()

			// Emit each accumulated tool_use block contiguously
			// (start → full input_json_delta → stop) in first-seen order, so the
			// single-active-block consumer assembles every call's arguments
			// instead of dropping interleaved or overwritten blocks.
			emitBufferedTools(false)

			outputTokens := 0
			if chunk.Usage != nil {
				outputTokens = chunk.Usage.CompletionTokens

				// Some providers bundle usage on the finish_reason chunk
				// instead of (or in addition to) a separate usage-only chunk.
				// Re-emit corrected message_start so consumeStream captures InputTokens.
				if chunk.Usage.PromptTokens > 0 {
					input, cached := chunk.Usage.splitPromptTokens()
					emit(ctx, out, StreamEvent{
						Type:    "message_start",
						Payload: marshalMessageStart(chunk.ID, chunk.Model, input, cached),
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

	// Stream ended without [DONE]. If a finish_reason chunk arrived, the model
	// completed its answer and the server merely omitted the [DONE] sentinel
	// (either signal counts as a clean end) — emit the normal stop.
	if sawFinishReason {
		flushPremature()
		emit(ctx, out, StreamEvent{Type: "message_stop"})
		return
	}

	// Clean EOF with neither finish_reason nor [DONE]: a close-delimited
	// (non-chunked) response whose connection died mid-answer, or an empty 200
	// body. ParseSSE cannot tell this from a normal end (scanner.Err() == nil),
	// and synthesizing message_stop here delivered the empty-or-truncated turn
	// to the user as a SUCCESS (reproduced live by killing an HTTP/1.0 broker
	// mid-response — PR #2268 review). Surface a retryable error instead: the
	// executor retries once on the same model, then escalates to the fallback
	// chain. Buffered tool calls are deliberately dropped, not flushed — the
	// flush rescue is reserved for an explicit [DONE], where the server (not
	// the transport) ended the stream.
	if len(toolOrder) > 0 {
		c.logger.Warn("dropping buffered tool calls at mid-stream EOF",
			"count", len(toolOrder))
	}
	errPayload, _ := json.Marshal(struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}{
		Type: "premature_end",
		Message: fmt.Sprintf(
			"provider stream ended without finish_reason or [DONE] after %d chunks — connection cut mid-response",
			chunkCount,
		),
	})
	emit(ctx, out, StreamEvent{Type: "error", Payload: errPayload})
}

func emit(ctx context.Context, ch chan<- StreamEvent, ev StreamEvent) {
	select {
	case ch <- ev:
	case <-ctx.Done():
	}
}
