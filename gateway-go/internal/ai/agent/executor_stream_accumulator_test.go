package agent

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func accumulatorEvent(t *testing.T, eventType string, payload any) llm.StreamEvent {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s: %v", eventType, err)
	}
	return llm.StreamEvent{Type: eventType, Payload: raw}
}

func accumulatorDelta(t *testing.T, index int, deltaType, text, thinking, signature, partialJSON string) llm.StreamEvent {
	t.Helper()
	var delta llm.ContentBlockDelta
	delta.Index = index
	delta.Delta.Type = deltaType
	delta.Delta.Text = text
	delta.Delta.Thinking = thinking
	delta.Delta.Signature = signature
	delta.Delta.PartialJSON = partialJSON
	return accumulatorEvent(t, "content_block_delta", delta)
}

func TestStreamAccumulatorBuildsTypedBlocksAndDispatchesHooks(t *testing.T) {
	result := &turnResult{}
	var textDeltas, thinkingDeltas []string
	accumulator := newStreamAccumulator(result, StreamHooks{
		OnTextDelta: func(delta string) { textDeltas = append(textDeltas, delta) },
		OnThinking:  func(delta string) { thinkingDeltas = append(thinkingDeltas, delta) },
	}, nil)

	apply := func(event llm.StreamEvent) {
		t.Helper()
		complete, err := accumulator.apply(event)
		if err != nil {
			t.Fatalf("apply %s: %v", event.Type, err)
		}
		if complete {
			t.Fatalf("apply %s completed stream unexpectedly", event.Type)
		}
	}

	apply(accumulatorEvent(t, "content_block_start", llm.ContentBlockStart{
		Index: 3, ContentBlock: llm.ContentBlock{Type: "text"},
	}))
	apply(accumulatorDelta(t, 9, "text_delta", "wrong index", "", "", ""))
	apply(accumulatorDelta(t, 3, "text_delta", "", "", "", ""))
	apply(accumulatorDelta(t, 3, "text_delta", "hello", "", "", ""))
	apply(llm.StreamEvent{Type: "content_block_stop"})

	apply(accumulatorEvent(t, "content_block_start", llm.ContentBlockStart{
		Index: 4, ContentBlock: llm.ContentBlock{Type: "thinking"},
	}))
	apply(accumulatorDelta(t, 4, "thinking_delta", "fallback", "reason", "", ""))
	apply(accumulatorDelta(t, 4, "signature_delta", "", "", "sig", ""))
	apply(llm.StreamEvent{Type: "content_block_stop"})

	apply(accumulatorEvent(t, "content_block_start", llm.ContentBlockStart{
		Index: 5, ContentBlock: llm.ContentBlock{Type: "tool_use", ID: "call-1", Name: "read"},
	}))
	apply(accumulatorDelta(t, 5, "input_json_delta", "", "", "", `{"path":`))
	apply(accumulatorDelta(t, 5, "input_json_delta", "", "", "", `"f.go"}`))
	apply(llm.StreamEvent{Type: "content_block_stop"})

	if result.text != "hello" {
		t.Errorf("text = %q, want hello", result.text)
	}
	if len(textDeltas) != 1 || textDeltas[0] != "hello" {
		t.Errorf("text hooks = %q, want [hello]", textDeltas)
	}
	if len(thinkingDeltas) != 1 || thinkingDeltas[0] != "reason" {
		t.Errorf("thinking hooks = %q, want [reason]", thinkingDeltas)
	}
	if len(result.contentBlocks) != 3 {
		t.Fatalf("content blocks = %d, want 3", len(result.contentBlocks))
	}
	thinking := result.contentBlocks[1]
	if thinking.Thinking != "reason" || thinking.Text != "" || thinking.Signature != "sig" {
		t.Errorf("thinking block = %+v, want reasoning/signature without visible text", thinking)
	}
	if len(result.toolCalls) != 1 || string(result.toolCalls[0].Input) != `{"path":"f.go"}` {
		t.Errorf("tool calls = %+v, want one complete read call", result.toolCalls)
	}
	complete, err := accumulator.apply(llm.StreamEvent{Type: "message_stop"})
	if err != nil || !complete {
		t.Errorf("message_stop = (complete:%v, err:%v), want (true, nil)", complete, err)
	}
}

func TestStreamAccumulatorMergesUsageAndProtectsStopReason(t *testing.T) {
	result := &turnResult{}
	accumulator := newStreamAccumulator(result, StreamHooks{}, nil)

	var start llm.MessageStart
	start.Message.Usage.InputTokens = 11
	start.Message.Usage.CacheReadInputTokens = 12
	start.Message.Usage.CacheCreationInputTokens = 13
	if _, err := accumulator.apply(accumulatorEvent(t, "message_start", start)); err != nil {
		t.Fatalf("apply message_start: %v", err)
	}

	var first llm.MessageDelta
	first.Delta.StopReason = "max_tokens"
	first.Usage.OutputTokens = 20
	first.Usage.CacheReadInputTokens = 22
	first.Usage.CacheCreationInputTokens = 23
	if _, err := accumulator.apply(accumulatorEvent(t, "message_delta", first)); err != nil {
		t.Fatalf("apply first message_delta: %v", err)
	}

	var usageOnly llm.MessageDelta
	usageOnly.Usage.OutputTokens = 24
	if _, err := accumulator.apply(accumulatorEvent(t, "message_delta", usageOnly)); err != nil {
		t.Fatalf("apply usage-only message_delta: %v", err)
	}

	if result.stopReason != "max_tokens" {
		t.Errorf("stop reason = %q, want max_tokens", result.stopReason)
	}
	want := llm.TokenUsage{
		InputTokens:              11,
		OutputTokens:             24,
		CacheReadInputTokens:     22,
		CacheCreationInputTokens: 23,
	}
	if result.usage != want {
		t.Errorf("usage = %+v, want %+v", result.usage, want)
	}
}
