package llm

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

type contentEventDescriptor struct {
	eventType   string
	index       int
	blockType   string
	id          string
	name        string
	deltaType   string
	text        string
	partialJSON string
}

func describeContentEvents(t *testing.T, events []StreamEvent) []contentEventDescriptor {
	t.Helper()
	descriptors := make([]contentEventDescriptor, 0, len(events))
	for _, event := range events {
		descriptor := contentEventDescriptor{eventType: event.Type, index: -1}
		switch event.Type {
		case "content_block_start":
			var start ContentBlockStart
			if err := json.Unmarshal(event.Payload.Bytes(), &start); err != nil {
				t.Fatalf("decode content_block_start: %v", err)
			}
			descriptor.index = start.Index
			descriptor.blockType = start.ContentBlock.Type
			descriptor.id = start.ContentBlock.ID
			descriptor.name = start.ContentBlock.Name
		case "content_block_delta":
			var delta ContentBlockDelta
			if err := json.Unmarshal(event.Payload.Bytes(), &delta); err != nil {
				t.Fatalf("decode content_block_delta: %v", err)
			}
			descriptor.index = delta.Index
			descriptor.deltaType = delta.Delta.Type
			descriptor.text = delta.Delta.Text
			descriptor.partialJSON = delta.Delta.PartialJSON
		case "content_block_stop":
			var stop ContentBlockStop
			if err := json.Unmarshal(event.Payload.Bytes(), &stop); err != nil {
				t.Fatalf("decode content_block_stop: %v", err)
			}
			descriptor.index = stop.Index
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors
}

func drainBufferedEvents(ch chan StreamEvent) []StreamEvent {
	close(ch)
	var events []StreamEvent
	for event := range ch {
		events = append(events, event)
	}
	return events
}

func deltaToolCall(index int, id, name, arguments string) openAIDeltaToolCall {
	var call openAIDeltaToolCall
	call.Index = index
	call.ID = id
	call.Function.Name = name
	call.Function.Arguments = arguments
	return call
}

func TestOpenAIContentEmitterPreservesBlockAndToolOrder(t *testing.T) {
	out := make(chan StreamEvent, 32)
	emitter := newOpenAIContentEmitter(context.Background(), out, nil)

	emitter.emitThinking("think")
	emitter.emitText("answer")
	emitter.appendToolCalls([]openAIDeltaToolCall{
		deltaToolCall(0, "call_read", "read", `{"pa`),
		deltaToolCall(1, "call_grep", "grep", `{"q":`),
	})
	emitter.appendToolCalls([]openAIDeltaToolCall{
		deltaToolCall(1, "", "", `"x"}`),
		deltaToolCall(0, "", "", `th":"a"}`),
	})
	emitter.closeVisible()
	emitter.flushTools(openAIFlushAllTools)

	got := describeContentEvents(t, drainBufferedEvents(out))
	want := []contentEventDescriptor{
		{eventType: "content_block_start", index: 0, blockType: "thinking"},
		{eventType: "content_block_delta", index: 0, deltaType: "thinking_delta", text: "think"},
		{eventType: "content_block_stop", index: 0},
		{eventType: "content_block_start", index: 1, blockType: "text"},
		{eventType: "content_block_delta", index: 1, deltaType: "text_delta", text: "answer"},
		{eventType: "content_block_stop", index: 1},
		{eventType: "content_block_start", index: 2, blockType: "tool_use", id: "call_read", name: "read"},
		{eventType: "content_block_delta", index: 2, deltaType: "input_json_delta", partialJSON: `{"path":"a"}`},
		{eventType: "content_block_stop", index: 2},
		{eventType: "content_block_start", index: 3, blockType: "tool_use", id: "call_grep", name: "grep"},
		{eventType: "content_block_delta", index: 3, deltaType: "input_json_delta", partialJSON: `{"q":"x"}`},
		{eventType: "content_block_stop", index: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("content event order mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestOpenAIContentEmitterToolFlushPolicies(t *testing.T) {
	t.Run("valid-only", func(t *testing.T) {
		out := make(chan StreamEvent, 16)
		emitter := newOpenAIContentEmitter(context.Background(), out, nil)
		emitter.appendToolCalls([]openAIDeltaToolCall{
			deltaToolCall(0, "call_ok", "read", `{"path":"a"}`),
			deltaToolCall(1, "call_cut", "write", `{"path":"a"`),
		})
		emitter.flushTools(openAIFlushValidTools)

		got := describeContentEvents(t, drainBufferedEvents(out))
		if len(got) != 3 || got[0].name != "read" || got[0].index != 0 {
			t.Fatalf("valid-only flush events = %#v, want one contiguous read block", got)
		}
	})

	t.Run("discard", func(t *testing.T) {
		out := make(chan StreamEvent, 4)
		emitter := newOpenAIContentEmitter(context.Background(), out, nil)
		emitter.appendToolCalls([]openAIDeltaToolCall{
			deltaToolCall(0, "call_drop", "write", `{"path":"a"}`),
		})
		emitter.flushTools(openAIDiscardTools)
		if got := drainBufferedEvents(out); len(got) != 0 {
			t.Fatalf("discard flush emitted %d events, want none", len(got))
		}
	})
}

func translateOpenAITestEvents(t *testing.T, rawEvents ...StreamEvent) []StreamEvent {
	t.Helper()
	raw := make(chan StreamEvent, len(rawEvents))
	for _, event := range rawEvents {
		raw <- event
	}
	close(raw)

	out := make(chan StreamEvent, 128)
	NewClient("http://example.invalid", "test-key").translateOpenAIStream(context.Background(), raw, out)
	return drainBufferedEvents(out)
}

func openAIData(v any) StreamEvent {
	payload, _ := json.Marshal(v)
	return StreamEvent{Payload: FlexibleFromRaw(payload)}
}

func TestTranslateOpenAIStreamLengthDiscardsBufferedTools(t *testing.T) {
	events := translateOpenAITestEvents(
		t,
		openAIData(map[string]any{
			"id": "length", "model": "test-model",
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{"tool_calls": []map[string]any{{
					"index": 0, "id": "call_partial", "type": "function",
					"function": map[string]string{"name": "write", "arguments": `{"path":"a"}`},
				}}},
			}},
		}),
		openAIData(map[string]any{
			"id": "length", "model": "test-model",
			"choices": []map[string]any{{
				"index": 0, "delta": map[string]any{}, "finish_reason": "length",
			}},
		}),
		StreamEvent{Payload: FlexibleFromRaw([]byte(`[DONE]`))},
	)

	var stopReason string
	var sawTool bool
	for _, event := range events {
		switch event.Type {
		case "content_block_start":
			var start ContentBlockStart
			if err := json.Unmarshal(event.Payload.Bytes(), &start); err != nil {
				t.Fatal(err)
			}
			if start.ContentBlock.Type == "tool_use" {
				sawTool = true
			}
		case "message_delta":
			var delta MessageDelta
			if err := json.Unmarshal(event.Payload.Bytes(), &delta); err != nil {
				t.Fatal(err)
			}
			if delta.Delta.StopReason != "" {
				stopReason = delta.Delta.StopReason
			}
		}
	}
	if sawTool {
		t.Fatal("length finish emitted a buffered tool call; max_tokens recovery would be bypassed")
	}
	if stopReason != "max_tokens" {
		t.Fatalf("stop reason = %q, want max_tokens", stopReason)
	}
}

func TestTranslateOpenAIStreamCanceledBeforeReadEmitsNothing(t *testing.T) {
	raw := make(chan StreamEvent, 1)
	raw <- openAIData(map[string]any{
		"id": "canceled", "model": "test-model",
		"choices": []map[string]any{{
			"index": 0, "delta": map[string]any{"content": "must not emit"},
		}},
	})
	close(raw)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := make(chan StreamEvent, 8)
	NewClient("http://example.invalid", "test-key").translateOpenAIStream(ctx, raw, out)
	if got := drainBufferedEvents(out); len(got) != 0 {
		t.Fatalf("canceled translator emitted %d events, want none", len(got))
	}
}

func TestTranslateOpenAIStreamCancellationUnblocksOutputSend(t *testing.T) {
	raw := make(chan StreamEvent, 1)
	raw <- openAIData(map[string]any{
		"id": "blocked", "model": "test-model",
		"choices": []map[string]any{{
			"index": 0, "delta": map[string]any{"content": "blocked output"},
		}},
	})
	close(raw)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan StreamEvent) // no reader: the first message_start send blocks
	done := make(chan struct{})
	go func() {
		NewClient("http://example.invalid", "test-key").translateOpenAIStream(ctx, raw, out)
		close(done)
	}()

	deadline := time.Now().Add(time.Second)
	for len(raw) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(raw) != 0 {
		t.Fatal("translator did not consume the fixture event")
	}
	// Give the translator time to reach the deliberately blocked output send.
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("translator did not return after cancellation unblocked its output send")
	}
}

func TestTranslateOpenAIStreamDoneClosesVisibleBlockWithoutFinishReason(t *testing.T) {
	tests := []struct {
		name      string
		delta     map[string]any
		blockType string
		deltaType string
	}{
		{name: "text", delta: map[string]any{"content": "partial"}, blockType: "text", deltaType: "text_delta"},
		{name: "thinking", delta: map[string]any{"reasoning": "partial"}, blockType: "thinking", deltaType: "thinking_delta"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events := translateOpenAITestEvents(
				t,
				openAIData(map[string]any{
					"id": "done", "model": "test-model",
					"choices": []map[string]any{{"index": 0, "delta": test.delta}},
				}),
				StreamEvent{Payload: FlexibleFromRaw([]byte(`[DONE]`))},
			)

			var types []string
			for _, event := range events {
				types = append(types, event.Type)
			}
			wantTypes := []string{
				"message_start",
				"content_block_start",
				"content_block_delta",
				"content_block_stop",
				"message_stop",
			}
			if !reflect.DeepEqual(types, wantTypes) {
				t.Fatalf("event types = %v, want visible block closed before message_stop: %v", types, wantTypes)
			}

			contentEvents := describeContentEvents(t, events[1:4])
			if contentEvents[0].blockType != test.blockType || contentEvents[0].index != 0 {
				t.Fatalf("block start = %#v, want %s index 0", contentEvents[0], test.blockType)
			}
			if contentEvents[1].deltaType != test.deltaType || contentEvents[1].index != 0 || contentEvents[1].text != "partial" {
				t.Fatalf("block delta = %#v, want %s index 0 with preserved text", contentEvents[1], test.deltaType)
			}
			if contentEvents[2].eventType != "content_block_stop" || contentEvents[2].index != 0 {
				t.Fatalf("block stop = %#v, want index 0", contentEvents[2])
			}
		})
	}
}

func TestTranslateOpenAIStreamIgnoresChoiceEventsAfterFinish(t *testing.T) {
	events := translateOpenAITestEvents(
		t,
		openAIData(map[string]any{
			"id": "finish", "model": "test-model",
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{"tool_calls": []map[string]any{{
					"index": 0, "id": "call_original", "type": "function",
					"function": map[string]string{"name": "read", "arguments": `{"path":"a"}`},
				}}},
				"finish_reason": "tool_calls",
			}},
		}),
		openAIData(map[string]any{
			"id": "finish", "model": "test-model",
			"choices": []map[string]any{{
				"index": 0, "delta": map[string]any{"content": "late text"},
			}},
		}),
		openAIData(map[string]any{
			"id": "finish", "model": "test-model",
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{"tool_calls": []map[string]any{{
					"index": 1, "id": "call_late", "type": "function",
					"function": map[string]string{"name": "write", "arguments": `{}`},
				}}},
			}},
		}),
		openAIData(map[string]any{
			"id": "finish", "model": "test-model",
			"choices": []map[string]any{{
				"index": 0, "delta": map[string]any{}, "finish_reason": "stop",
			}},
		}),
		openAIData(map[string]any{
			"id": "finish", "model": "test-model", "choices": []any{},
			"usage": map[string]int{"prompt_tokens": 21, "completion_tokens": 17},
		}),
		StreamEvent{Payload: FlexibleFromRaw([]byte(`[DONE]`))},
	)

	var toolNames []string
	var stopReasons []string
	var outputTokens int
	for _, event := range events {
		switch event.Type {
		case "content_block_start":
			var start ContentBlockStart
			if err := json.Unmarshal(event.Payload.Bytes(), &start); err != nil {
				t.Fatal(err)
			}
			if start.ContentBlock.Type == "text" {
				t.Fatal("late text choice opened a content block after finish_reason")
			}
			if start.ContentBlock.Type == "tool_use" {
				toolNames = append(toolNames, start.ContentBlock.Name)
			}
		case "message_delta":
			var delta MessageDelta
			if err := json.Unmarshal(event.Payload.Bytes(), &delta); err != nil {
				t.Fatal(err)
			}
			if delta.Delta.StopReason != "" {
				stopReasons = append(stopReasons, delta.Delta.StopReason)
			}
			if delta.Usage.OutputTokens != 0 {
				outputTokens = delta.Usage.OutputTokens
			}
		}
	}
	if !reflect.DeepEqual(toolNames, []string{"read"}) {
		t.Fatalf("tool names = %v, want only original read call", toolNames)
	}
	if !reflect.DeepEqual(stopReasons, []string{"tool_use"}) {
		t.Fatalf("stop reasons = %v, want first finish reason preserved", stopReasons)
	}
	if outputTokens != 17 {
		t.Fatalf("trailing usage output tokens = %d, want 17", outputTokens)
	}
}
