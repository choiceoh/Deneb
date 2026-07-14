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
func marshalMessageStart(id, model string, inputTokens, cacheReadTokens int) FlexibleJSON {
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
	return FlexibleFromRaw(p)
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
func probeOpenAIError(payload FlexibleJSON) (FlexibleJSON, bool) {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(payload.Bytes(), &errResp) != nil || errResp.Error.Message == "" {
		return FlexibleJSON{}, false
	}
	p, _ := json.Marshal(map[string]string{
		"type":    errResp.Error.Type,
		"message": errResp.Error.Message,
	})
	return FlexibleFromRaw(p), true
}

// translateOpenAIStream reads OpenAI SSE chunks from rawEvents and emits
// Anthropic-style StreamEvents to out.
func (c *Client) translateOpenAIStream(ctx context.Context, rawEvents <-chan StreamEvent, out chan<- StreamEvent) {
	translator := newOpenAIStreamTranslator(ctx, c, out)
	translator.run(rawEvents)
}

type openAIStreamTranslator struct {
	client          *Client
	ctx             context.Context
	out             chan<- StreamEvent
	content         *openAIContentEmitter
	firstChunk      bool
	sawFinishReason bool
	chunkCount      int
	rawBytes        int
}

func newOpenAIStreamTranslator(ctx context.Context, client *Client, out chan<- StreamEvent) *openAIStreamTranslator {
	return &openAIStreamTranslator{
		client:     client,
		ctx:        ctx,
		out:        out,
		content:    newOpenAIContentEmitter(ctx, out, client.logger),
		firstChunk: true,
	}
}

func (t *openAIStreamTranslator) run(rawEvents <-chan StreamEvent) {
	for {
		raw, ok := t.nextRawEvent(rawEvents)
		if !ok {
			if t.ctx.Err() == nil {
				t.finishAtEOF()
			}
			return
		}
		if !t.handleRawEvent(raw) {
			return
		}
	}
}

func (t *openAIStreamTranslator) nextRawEvent(rawEvents <-chan StreamEvent) (StreamEvent, bool) {
	if t.ctx.Err() != nil {
		return StreamEvent{}, false
	}
	select {
	case <-t.ctx.Done():
		return StreamEvent{}, false
	case raw, ok := <-rawEvents:
		return raw, ok
	}
}

func (t *openAIStreamTranslator) handleRawEvent(raw StreamEvent) bool {
	if !t.acceptRawBytes(raw) {
		return false
	}
	if raw.Payload.String() == "[DONE]" {
		t.finishAtDoneSentinel()
		return false
	}
	if raw.Type == "error" {
		t.emitError(raw.Payload)
		return false
	}

	chunk, action := t.decodeChunk(raw.Payload)
	switch action {
	case openAIChunkSkip:
		return true
	case openAIChunkStop:
		return false
	case openAIChunkProcess:
		// Continue with the decoded chunk below.
	}

	t.chunkCount++
	t.emitInitialMessageStart(chunk)
	if len(chunk.Choices) == 0 {
		return t.handleChoiceLessChunk(raw.Payload, chunk)
	}
	if t.sawFinishReason {
		t.client.logger.Debug("ignoring OpenAI choice chunk after finish_reason")
		return true
	}

	t.handleChoiceChunk(chunk)
	return true
}

func (t *openAIStreamTranslator) acceptRawBytes(raw StreamEvent) bool {
	if t.client.maxStreamBytes <= 0 {
		return true
	}
	incoming := len(raw.Type) + raw.Payload.Len()
	if incoming <= t.client.maxStreamBytes-t.rawBytes {
		t.rawBytes += incoming
		return true
	}
	payload, _ := json.Marshal(map[string]string{
		"type": "stream_limit", "message": "provider stream exceeded configured byte limit",
	})
	emit(t.ctx, t.out, StreamEvent{Type: "error", Payload: FlexibleFromRaw(payload)})
	return false
}

type openAIChunkAction uint8

const (
	openAIChunkProcess openAIChunkAction = iota
	openAIChunkSkip
	openAIChunkStop
)

func (t *openAIStreamTranslator) decodeChunk(payload FlexibleJSON) (openAIChunk, openAIChunkAction) {
	var chunk openAIChunk
	if err := json.Unmarshal(payload.Bytes(), &chunk); err != nil {
		if errorPayload, ok := probeOpenAIError(payload); ok {
			t.emitError(errorPayload)
			return openAIChunk{}, openAIChunkStop
		}
		t.client.logger.Warn("skipping unparseable OpenAI stream chunk",
			"error", err, "payload", payload.String())
		return openAIChunk{}, openAIChunkSkip
	}
	return chunk, openAIChunkProcess
}

func (t *openAIStreamTranslator) emitInitialMessageStart(chunk openAIChunk) {
	if !t.firstChunk {
		return
	}
	t.firstChunk = false
	emit(t.ctx, t.out, StreamEvent{
		Type:    "message_start",
		Payload: marshalMessageStart(chunk.ID, chunk.Model, 0, 0),
	})
}

func (t *openAIStreamTranslator) handleChoiceLessChunk(rawPayload FlexibleJSON, chunk openAIChunk) bool {
	// A bare {"error":{...}} body parses into a zero-valued openAIChunk, so it
	// needs a second probe after successful unmarshalling.
	if chunk.Usage == nil {
		if errorPayload, ok := probeOpenAIError(rawPayload); ok {
			t.emitError(errorPayload)
			return false
		}
		return true
	}

	t.emitUsageMessageStart(chunk)
	// Usage-only chunks must not replace the stop reason emitted by the
	// preceding choice chunk.
	emit(t.ctx, t.out, StreamEvent{
		Type:    "message_delta",
		Payload: marshalMessageDelta("", chunk.Usage.CompletionTokens),
	})
	return true
}

func (t *openAIStreamTranslator) handleChoiceChunk(chunk openAIChunk) {
	choice := chunk.Choices[0]
	if reasoning := choice.Delta.reasoningText(); reasoning != "" {
		t.content.emitThinking(reasoning)
	}
	if choice.Delta.Content != "" {
		t.content.emitText(choice.Delta.Content)
	}
	if choice.Delta.Refusal != "" {
		t.content.emitText(choice.Delta.Refusal)
	}

	// Buffer tool fragments so every emitted tool block remains contiguous even
	// when providers interleave fragments from parallel calls.
	t.content.appendToolCalls(choice.Delta.ToolCalls)
	if choice.FinishReason != nil {
		t.finishChoice(chunk, *choice.FinishReason)
	}
}

func (t *openAIStreamTranslator) finishChoice(chunk openAIChunk, finishReason string) {
	t.sawFinishReason = true
	t.content.closeVisible()
	if finishReason == "length" {
		t.content.flushTools(openAIDiscardTools)
	} else {
		t.content.flushTools(openAIFlushAllTools)
	}

	outputTokens := 0
	if chunk.Usage != nil {
		outputTokens = chunk.Usage.CompletionTokens
		t.emitUsageMessageStart(chunk)
	}
	emit(t.ctx, t.out, StreamEvent{
		Type:    "message_delta",
		Payload: marshalMessageDelta(mapFinishReason(finishReason), outputTokens),
	})
}

func (t *openAIStreamTranslator) emitUsageMessageStart(chunk openAIChunk) {
	if chunk.Usage == nil || chunk.Usage.PromptTokens <= 0 {
		return
	}
	input, cached := chunk.Usage.splitPromptTokens()
	emit(t.ctx, t.out, StreamEvent{
		Type:    "message_start",
		Payload: marshalMessageStart(chunk.ID, chunk.Model, input, cached),
	})
}

func marshalMessageDelta(stopReason string, outputTokens int) FlexibleJSON {
	payload, _ := json.Marshal(MessageDelta{
		Delta: struct {
			StopReason string `json:"stop_reason"`
		}{StopReason: stopReason},
		Usage: struct {
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
		}{OutputTokens: outputTokens},
	})
	return FlexibleFromRaw(payload)
}

func (t *openAIStreamTranslator) finishAtDoneSentinel() {
	t.content.closeVisible()
	if count := t.content.bufferedToolCount(); count > 0 {
		t.client.logger.Warn("openai stream ended without finish_reason; flushing buffered tool calls",
			"count", count)
	}
	t.content.flushTools(openAIFlushValidTools)
	emit(t.ctx, t.out, StreamEvent{Type: "message_stop"})
}

func (t *openAIStreamTranslator) emitError(payload FlexibleJSON) {
	t.content.flushTools(openAIDiscardTools)
	emit(t.ctx, t.out, StreamEvent{Type: "error", Payload: payload})
}

func (t *openAIStreamTranslator) finishAtEOF() {
	// A finish_reason is a clean terminal signal even when a compatible server
	// omits the [DONE] sentinel.
	if t.sawFinishReason {
		t.content.closeVisible()
		t.content.flushTools(openAIFlushValidTools)
		emit(t.ctx, t.out, StreamEvent{Type: "message_stop"})
		return
	}

	// Without either terminal signal, fail closed so a truncated answer is not
	// committed as success. Buffered tool calls are discarded to prevent a
	// partial side effect from executing.
	if count := t.content.bufferedToolCount(); count > 0 {
		t.client.logger.Warn("dropping buffered tool calls at mid-stream EOF",
			"count", count)
	}
	t.content.flushTools(openAIDiscardTools)
	errPayload, _ := json.Marshal(struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}{
		Type: "premature_end",
		Message: fmt.Sprintf(
			"provider stream ended without finish_reason or [DONE] after %d chunks — connection cut mid-response",
			t.chunkCount,
		),
	})
	emit(t.ctx, t.out, StreamEvent{Type: "error", Payload: FlexibleFromRaw(errPayload)})
}

func emit(ctx context.Context, ch chan<- StreamEvent, ev StreamEvent) {
	select {
	case ch <- ev:
	case <-ctx.Done():
	}
}
