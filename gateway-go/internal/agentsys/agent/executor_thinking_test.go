package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// A streamed extended-thinking block (content_block_start type=thinking →
// thinking_delta → content_block_stop) must store its reasoning in the block's
// Thinking field, not Text. The bug: content_block_stop appended the block by
// value and then did the Text→Thinking swap on the discarded local copy, so the
// stored block kept Thinking="" with the reasoning stranded in Text — breaking
// joinAllThinkingTexts and the interleaved-thinking round-trip on the main path.
func TestConsumeStreamInto_StreamedThinkingStoresReasoning(t *testing.T) {
	mkDelta := func(thinking string) json.RawMessage {
		var cbd llm.ContentBlockDelta
		cbd.Index = 0
		cbd.Delta.Type = "thinking_delta"
		cbd.Delta.Thinking = thinking
		b, _ := json.Marshal(cbd)
		return b
	}
	start, _ := json.Marshal(llm.ContentBlockStart{Index: 0, ContentBlock: llm.ContentBlock{Type: "thinking"}})
	stop, _ := json.Marshal(llm.ContentBlockStop{Index: 0})

	events := make(chan llm.StreamEvent, 8)
	events <- llm.StreamEvent{Type: "content_block_start", Payload: start}
	events <- llm.StreamEvent{Type: "content_block_delta", Payload: mkDelta("let me ")}
	events <- llm.StreamEvent{Type: "content_block_delta", Payload: mkDelta("reason")}
	events <- llm.StreamEvent{Type: "content_block_stop", Payload: stop}
	events <- makeStreamEvent("message_stop")
	close(events)

	result := &turnResult{}
	if err := consumeStreamInto(context.Background(), events, StreamHooks{}, result, -1, nil); err != nil {
		t.Fatalf("consumeStreamInto: %v", err)
	}

	if len(result.contentBlocks) != 1 {
		t.Fatalf("got %d content blocks, want 1", len(result.contentBlocks))
	}
	b := result.contentBlocks[0]
	if b.Type != "thinking" {
		t.Fatalf("block type = %q, want thinking", b.Type)
	}
	if b.Thinking != "let me reason" {
		t.Errorf("block.Thinking = %q, want %q (reasoning stranded in Text?)", b.Thinking, "let me reason")
	}
	if b.Text != "" {
		t.Errorf("block.Text = %q, want empty", b.Text)
	}
	// Thinking must not leak into user-visible text.
	if result.text != "" {
		t.Errorf("result.text = %q, want empty", result.text)
	}
}
