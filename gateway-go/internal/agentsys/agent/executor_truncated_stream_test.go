package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// Event constructors mirroring what the stream translators emit.

func truncBlockStart(t *testing.T, idx int, block llm.ContentBlock) llm.StreamEvent {
	t.Helper()
	p, err := json.Marshal(llm.ContentBlockStart{Index: idx, ContentBlock: block})
	if err != nil {
		t.Fatalf("marshal start: %v", err)
	}
	return llm.StreamEvent{Type: "content_block_start", Payload: p}
}

func truncDelta(t *testing.T, idx int, deltaType, text, partialJSON string) llm.StreamEvent {
	t.Helper()
	var cbd llm.ContentBlockDelta
	cbd.Index = idx
	cbd.Delta.Type = deltaType
	cbd.Delta.Text = text
	cbd.Delta.PartialJSON = partialJSON
	p, err := json.Marshal(cbd)
	if err != nil {
		t.Fatalf("marshal delta: %v", err)
	}
	return llm.StreamEvent{Type: "content_block_delta", Payload: p}
}

func runTruncated(t *testing.T, events []llm.StreamEvent) *turnResult {
	t.Helper()
	ch := make(chan llm.StreamEvent, len(events))
	for _, ev := range events {
		ch <- ev
	}
	close(ch)
	result := &turnResult{}
	if err := consumeStreamInto(context.Background(), ch, StreamHooks{}, result, -1, nil); err != nil {
		t.Fatalf("consumeStreamInto: %v", err)
	}
	return result
}

// A stream cut after text deltas but before content_block_stop (lost finish
// chunk, mid-stream EOF, [DONE] with a block still open) must NOT drop the
// text the user already watched stream in. Before the fix, message_stop and
// channel-close returned without finalizing the pending block, so the final
// result was empty — a silent no-reply — while OnTextDelta had already shown
// the text live.
func TestConsumeStreamInto_TruncatedStream_RescuesPendingText(t *testing.T) {
	cases := []struct {
		name string
		tail []llm.StreamEvent // events after the unfinished text block
	}{
		{"message_stop without block stop", []llm.StreamEvent{{Type: "message_stop"}}},
		{"bare channel close", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events := []llm.StreamEvent{
				truncBlockStart(t, 0, llm.ContentBlock{Type: "text"}),
				truncDelta(t, 0, "text_delta", "partial ", ""),
				truncDelta(t, 0, "text_delta", "reply", ""),
			}
			events = append(events, tc.tail...)
			result := runTruncated(t, events)

			if result.text != "partial reply" {
				t.Errorf("result.text = %q, want %q (pending text dropped on truncated stream)",
					result.text, "partial reply")
			}
			if len(result.contentBlocks) != 1 || result.contentBlocks[0].Type != "text" {
				t.Errorf("contentBlocks = %+v, want one text block", result.contentBlocks)
			}
		})
	}
}

// A truncated thinking block must go through the same Text→Thinking finalize
// move as the content_block_stop path: reasoning lands in Thinking, never in
// user-visible text.
func TestConsumeStreamInto_TruncatedStream_FinalizesThinking(t *testing.T) {
	result := runTruncated(t, []llm.StreamEvent{
		truncBlockStart(t, 0, llm.ContentBlock{Type: "thinking"}),
		truncDelta(t, 0, "thinking_delta", "let me think", ""),
		// stream cut: no content_block_stop, no message_stop
	})

	if len(result.contentBlocks) != 1 {
		t.Fatalf("got %d content blocks, want 1", len(result.contentBlocks))
	}
	b := result.contentBlocks[0]
	if b.Thinking != "let me think" || b.Text != "" {
		t.Errorf("block = {Thinking:%q Text:%q}, want reasoning in Thinking only", b.Thinking, b.Text)
	}
	if result.text != "" {
		t.Errorf("result.text = %q, want empty (thinking must not leak)", result.text)
	}
}

// An incomplete tool_use (arguments truncated mid-JSON) must be DROPPED, not
// executed: running a half-specified action is worse than losing it. But a
// pending tool_use whose accumulated arguments are complete valid JSON (only
// the stop event was lost) is kept.
func TestConsumeStreamInto_TruncatedStream_ToolUseValidityGate(t *testing.T) {
	t.Run("truncated args dropped", func(t *testing.T) {
		result := runTruncated(t, []llm.StreamEvent{
			truncBlockStart(t, 0, llm.ContentBlock{Type: "tool_use", ID: "call_1", Name: "write"}),
			truncDelta(t, 0, "input_json_delta", "", `{"path":"f`),
		})
		if len(result.toolCalls) != 0 || len(result.contentBlocks) != 0 {
			t.Errorf("toolCalls=%d contentBlocks=%d, want 0/0 (incomplete tool_use must be dropped)",
				len(result.toolCalls), len(result.contentBlocks))
		}
	})

	t.Run("complete valid args kept", func(t *testing.T) {
		result := runTruncated(t, []llm.StreamEvent{
			truncBlockStart(t, 0, llm.ContentBlock{Type: "tool_use", ID: "call_1", Name: "read"}),
			truncDelta(t, 0, "input_json_delta", "", `{"path":"f.go"}`),
			{Type: "message_stop"},
		})
		if len(result.toolCalls) != 1 {
			t.Fatalf("got %d tool calls, want 1 (complete-args tool_use should survive)", len(result.toolCalls))
		}
		if got := string(result.toolCalls[0].Input); got != `{"path":"f.go"}` {
			t.Errorf("tool input = %q, want full args", got)
		}
	})
}

// The normal path (block properly stopped) must be unchanged: no double
// append from the flush, exact same result as before the fix.
func TestConsumeStreamInto_ProperlyStoppedBlock_NoDoubleAppend(t *testing.T) {
	stop, _ := json.Marshal(llm.ContentBlockStop{Index: 0})
	result := runTruncated(t, []llm.StreamEvent{
		truncBlockStart(t, 0, llm.ContentBlock{Type: "text"}),
		truncDelta(t, 0, "text_delta", "hello", ""),
		{Type: "content_block_stop", Payload: stop},
		{Type: "message_stop"},
	})
	if result.text != "hello" || len(result.contentBlocks) != 1 {
		t.Errorf("text=%q blocks=%d, want hello/1 (flush must be a no-op after a proper stop)",
			result.text, len(result.contentBlocks))
	}
}
