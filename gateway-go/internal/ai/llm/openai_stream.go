// openai_stream.go — OpenAI SSE → Anthropic StreamEvent protocol translation:
// synthetic message_start/finish-reason mapping, usage handling, and terminal
// state decisions. Content-block lifecycle lives in openai_stream_content.go.
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
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "content_filter":
		return "content_filtered"
	case "":
		return "unknown_finish_reason"
	default:
		return "unknown_finish_reason:" + reason
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
	content := newOpenAIContentEmitter(ctx, out, c.logger)
	rawBytes := 0

streamLoop:
	for {
		if ctx.Err() != nil {
			return
		}
		var raw StreamEvent
		var ok bool
		select {
		case <-ctx.Done():
			return
		case raw, ok = <-rawEvents:
			if !ok {
				break streamLoop
			}
		}
		if c.maxStreamBytes > 0 {
			incoming := len(raw.Type) + len(raw.Payload)
			if incoming > c.maxStreamBytes-rawBytes {
				payload, _ := json.Marshal(map[string]string{
					"type": "stream_limit", "message": "provider stream exceeded configured byte limit",
				})
				emit(ctx, out, StreamEvent{Type: "error", Payload: payload})
				return
			}
			rawBytes += incoming
		}

		// OpenAI sends "data: [DONE]" as the final event.
		if string(raw.Payload) == "[DONE]" {
			content.closeVisible()
			if count := content.bufferedToolCount(); count > 0 {
				c.logger.Warn("openai stream ended without finish_reason; flushing buffered tool calls",
					"count", count)
			}
			content.flushTools(openAIFlushValidTools)
			emit(ctx, out, StreamEvent{Type: "message_stop"})
			return
		}

		// Handle SSE error events from OpenAI-compatible providers.
		if raw.Type == "error" {
			content.flushTools(openAIDiscardTools)
			emit(ctx, out, StreamEvent{Type: "error", Payload: raw.Payload})
			return
		}

		var chunk openAIChunk
		if err := json.Unmarshal(raw.Payload, &chunk); err != nil {
			// Try parsing as an OpenAI error response ({"error": {...}}).
			if errPayload, ok := probeOpenAIError(raw.Payload); ok {
				content.flushTools(openAIDiscardTools)
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
					content.flushTools(openAIDiscardTools)
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
		if sawFinishReason {
			// A finish_reason is the terminal choice event. Some compatible
			// providers send duplicate or stale choice chunks while delivering
			// trailing usage; accepting them could append content after the stop
			// delta or overwrite tool_use with a later end_turn.
			c.logger.Debug("ignoring OpenAI choice chunk after finish_reason")
			continue
		}

		choice := chunk.Choices[0]

		// Emit reasoning content as a thinking block (OpenAI/vLLM reasoning models).
		if rtext := choice.Delta.reasoningText(); rtext != "" {
			content.emitThinking(rtext)
		}

		// Emit text content. emitText opens the text block lazily and closes any
		// open thinking block first.
		if choice.Delta.Content != "" {
			content.emitText(choice.Delta.Content)
		}

		// Surface model refusals. OpenAI streams a refusal on delta.refusal with
		// content null; without this the refusal text is dropped and the user
		// gets an empty reply (a silent no-reply).
		if choice.Delta.Refusal != "" {
			content.emitText(choice.Delta.Refusal)
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
		content.appendToolCalls(choice.Delta.ToolCalls)

		// Check finish reason (nil = not yet finished, non-nil = terminal).
		if choice.FinishReason != nil {
			sawFinishReason = true
			content.closeVisible()

			// Emit each accumulated tool_use block contiguously
			// (start → full input_json_delta → stop) in first-seen order, so the
			// single-active-block consumer assembles every call's arguments
			// instead of dropping interleaved or overwritten blocks. A length
			// finish is different: the tool call may be incomplete even when its
			// partial JSON happens to be valid, so discard it and let max_tokens
			// recovery retry the turn without executing a partial side effect.
			if *choice.FinishReason == "length" {
				content.flushTools(openAIDiscardTools)
			} else {
				content.flushTools(openAIFlushAllTools)
			}

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
		content.closeVisible()
		content.flushTools(openAIFlushValidTools)
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
	if count := content.bufferedToolCount(); count > 0 {
		c.logger.Warn("dropping buffered tool calls at mid-stream EOF",
			"count", count)
	}
	content.flushTools(openAIDiscardTools)
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
