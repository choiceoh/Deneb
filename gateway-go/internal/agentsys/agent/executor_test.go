package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestExtractThinkingText_Basic(t *testing.T) {
	blocks := []llm.ContentBlock{
		{Type: "thinking", Thinking: "사용자가 설정 파일을 수정하고 싶어합니다.\n먼저 파일을 읽어봐야겠습니다."},
		{Type: "tool_use", Name: "read"},
	}

	got := extractThinkingText(blocks)
	want := "사용자가 설정 파일을 수정하고 싶어합니다.\n먼저 파일을 읽어봐야겠습니다."
	if got != want {
		t.Errorf("unexpected text: %q", got)
	}
}


func TestExtractThinkingText_MultipleBlocks(t *testing.T) {
	blocks := []llm.ContentBlock{
		{Type: "thinking", Thinking: "first thinking block"},
		{Type: "text", Text: "some text"},
		{Type: "thinking", Thinking: "second thinking block — closer to tools"},
		{Type: "tool_use", Name: "exec"},
	}

	got := extractThinkingText(blocks)
	if got != "second thinking block — closer to tools" {
		t.Errorf("expected last thinking block, got: %q", got)
	}
}

// --- Stream idle watchdog tests ---

// makeStreamEvent creates a minimal SSE event for testing.
func makeStreamEvent(typ string) llm.StreamEvent {
	return llm.StreamEvent{Type: typ, Payload: json.RawMessage(`{}`)}
}

func TestConsumeStreamInto_IdleTimeout(t *testing.T) {
	// Channel that never sends — should trigger idle timeout.
	events := make(chan llm.StreamEvent)
	ctx := context.Background()
	result := &turnResult{}

	err := consumeStreamInto(ctx, events, StreamHooks{}, result, 50*time.Millisecond, nil)
	if !errors.Is(err, ErrStreamIdle) {
		t.Fatalf("expected ErrStreamIdle, got: %v", err)
	}
}

func TestConsumeStreamInto_IdleResetOnEvent(t *testing.T) {
	// Events arrive just before the idle timeout, then stream closes.
	events := make(chan llm.StreamEvent, 3)
	ctx := context.Background()
	result := &turnResult{}

	// Send message_start, then close after a short delay.
	go func() {
		events <- makeStreamEvent("message_start")
		time.Sleep(30 * time.Millisecond)
		events <- makeStreamEvent("message_stop")
	}()

	err := consumeStreamInto(ctx, events, StreamHooks{}, result, 100*time.Millisecond, nil)
	testutil.NoError(t, err)
}

func TestConsumeStreamInto_MalformedEventsSkipped(t *testing.T) {
	// Malformed events should be logged but not crash; valid events still processed.
	events := make(chan llm.StreamEvent, 10)
	ctx := context.Background()
	result := &turnResult{}

	go func() {
		// Valid message_start.
		startPayload, _ := json.Marshal(llm.MessageStart{})
		events <- llm.StreamEvent{Type: "message_start", Payload: startPayload}

		// Malformed content_block_start (bad JSON).
		events <- llm.StreamEvent{Type: "content_block_start", Payload: json.RawMessage(`{bad`)}

		// Malformed content_block_delta.
		events <- llm.StreamEvent{Type: "content_block_delta", Payload: json.RawMessage(`not json`)}

		// Malformed message_delta.
		events <- llm.StreamEvent{Type: "message_delta", Payload: json.RawMessage(`///`)}

		// Valid message_stop.
		events <- llm.StreamEvent{Type: "message_stop", Payload: json.RawMessage(`{}`)}
	}()

	err := consumeStreamInto(ctx, events, StreamHooks{}, result, -1, nil)
	testutil.NoError(t, err)
}

func TestConsumeStreamInto_DeltaIndexMismatch(t *testing.T) {
	// Delta with mismatched index should be dropped (not applied to current block).
	events := make(chan llm.StreamEvent, 10)
	ctx := context.Background()
	result := &turnResult{}

	go func() {
		startPayload, _ := json.Marshal(llm.MessageStart{})
		events <- llm.StreamEvent{Type: "message_start", Payload: startPayload}

		// Open block at index 0.
		cbsPayload, _ := json.Marshal(llm.ContentBlockStart{
			Index:        0,
			ContentBlock: llm.ContentBlock{Type: "text"},
		})
		events <- llm.StreamEvent{Type: "content_block_start", Payload: cbsPayload}

		// Valid delta for index 0.
		cbd0, _ := json.Marshal(llm.ContentBlockDelta{
			Index: 0,
			Delta: struct {
				Type        string `json:"type"`
				Text        string `json:"text,omitempty"`
				PartialJSON string `json:"partial_json,omitempty"`
			}{Type: "text_delta", Text: "hello"},
		})
		events <- llm.StreamEvent{Type: "content_block_delta", Payload: cbd0}

		// Mismatched delta for index 5 — should be dropped.
		cbd5, _ := json.Marshal(llm.ContentBlockDelta{
			Index: 5,
			Delta: struct {
				Type        string `json:"type"`
				Text        string `json:"text,omitempty"`
				PartialJSON string `json:"partial_json,omitempty"`
			}{Type: "text_delta", Text: " SHOULD NOT APPEAR"},
		})
		events <- llm.StreamEvent{Type: "content_block_delta", Payload: cbd5}

		// Close block 0.
		cbStop, _ := json.Marshal(llm.ContentBlockStop{Index: 0})
		events <- llm.StreamEvent{Type: "content_block_stop", Payload: cbStop}

		events <- llm.StreamEvent{Type: "message_stop", Payload: json.RawMessage(`{}`)}
	}()

	err := consumeStreamInto(ctx, events, StreamHooks{}, result, -1, nil)
	testutil.NoError(t, err)

	if result.text != "hello" {
		t.Errorf("text = %q, want %q (mismatched delta should be dropped)", result.text, "hello")
	}
}

