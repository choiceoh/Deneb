package llm

import "testing"

func TestDrainStreamText(t *testing.T) {
	events := make(chan StreamEvent, 4)
	events <- StreamEvent{Type: "message_start"}
	events <- StreamEvent{Type: "content_block_delta", Payload: []byte(`{"delta":{"text":"hello "}}`)}
	events <- StreamEvent{Type: "content_block_delta", Payload: []byte(`{"delta":{"text":"world"}}`)}
	events <- StreamEvent{Type: "content_block_delta", Payload: []byte(`{`)}
	close(events)
	if got := DrainStreamText(events); got != "hello world" {
		t.Fatalf("DrainStreamText() = %q", got)
	}
}
