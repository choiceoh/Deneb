package chat

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

func TestTruncateForBroadcast(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"under limit", "hello", 10, "hello"},
		{"at limit", "hello", 5, "hello"},
		{"over limit", "hello world", 5, "hello... [truncated]"},
		{"empty string", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateForBroadcast(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateForBroadcast(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestStreamBroadcasterNilSafe(t *testing.T) {
	// All methods should be safe to call with nil broadcastRaw.
	sb := newStreamBroadcaster(nil, "session-1", "run-1")

	// None of these should panic.
	sb.EmitDelta("text")
	sb.EmitToolStart("read", "tool-1")
	sb.EmitToolResult("tool-1", "result", false)
	sb.EmitComplete("done", llm.TokenUsage{InputTokens: 10, OutputTokens: 5})
	sb.EmitError("something broke")
	sb.EmitStarted()
	sb.EmitAborted("partial")
}

func TestStreamBroadcasterEmitDelta(t *testing.T) {
	t.Run("skips empty text", func(t *testing.T) {
		called := false
		sb := newStreamBroadcaster(func(event string, data []byte) int {
			called = true
			return 0
		}, "s1", "r1")
		sb.EmitDelta("")
		if called {
			t.Error("should not broadcast empty delta")
		}
	})

	t.Run("broadcasts non-empty text", func(t *testing.T) {
		var captured struct {
			event string
			data  []byte
		}
		sb := newStreamBroadcaster(func(event string, data []byte) int {
			captured.event = event
			captured.data = data
			return 1
		}, "s1", "r1")
		sb.EmitDelta("hello")

		if captured.event != eventDelta {
			t.Errorf("event = %q, want %q", captured.event, eventDelta)
		}

		var msg map[string]any
		json.Unmarshal(captured.data, &msg)
		payload := msg["payload"].(map[string]any)
		if payload["delta"] != "hello" {
			t.Errorf("delta = %v, want %q", payload["delta"], "hello")
		}
		if payload["sessionKey"] != "s1" {
			t.Errorf("sessionKey = %v, want %q", payload["sessionKey"], "s1")
		}
		if payload["clientRunId"] != "r1" {
			t.Errorf("clientRunId = %v, want %q", payload["clientRunId"], "r1")
		}
	})
}

func TestStreamBroadcasterEvents(t *testing.T) {
	var mu sync.Mutex
	var events []struct {
		event string
		data  map[string]any
	}

	sb := newStreamBroadcaster(func(event string, data []byte) int {
		var parsed map[string]any
		json.Unmarshal(data, &parsed)
		mu.Lock()
		events = append(events, struct {
			event string
			data  map[string]any
		}{event, parsed})
		mu.Unlock()
		return 1
	}, "sess", "run")

	sb.EmitStarted()
	sb.EmitDelta("chunk1")
	sb.EmitToolStart("read", "t1")
	sb.EmitToolResult("t1", "file content", false)
	sb.EmitDelta("chunk2")
	sb.EmitComplete("final", llm.TokenUsage{InputTokens: 100, OutputTokens: 50})

	if len(events) != 6 {
		t.Fatalf("expected 6 events, got %d", len(events))
	}

	// Verify event types.
	wantEvents := []string{eventChat, eventDelta, eventTool, eventTool, eventDelta, eventChat}
	for i, want := range wantEvents {
		if events[i].event != want {
			t.Errorf("event[%d] = %q, want %q", i, events[i].event, want)
		}
	}

	// Verify seq increments.
	for i, ev := range events {
		payload := ev.data["payload"].(map[string]any)
		seq := payload["seq"].(float64)
		if int(seq) != i+1 {
			t.Errorf("event[%d] seq = %v, want %d", i, seq, i+1)
		}
	}
}

func TestStreamBroadcasterToolResult(t *testing.T) {
	var captured map[string]any
	sb := newStreamBroadcaster(func(event string, data []byte) int {
		json.Unmarshal(data, &captured)
		return 1
	}, "s1", "r1")

	sb.EmitToolResult("tool-id", "error message", true)

	payload := captured["payload"].(map[string]any)
	if payload["state"] != "completed" {
		t.Errorf("state = %v, want %q", payload["state"], "completed")
	}
	if payload["toolUseId"] != "tool-id" {
		t.Errorf("toolUseId = %v, want %q", payload["toolUseId"], "tool-id")
	}
	if payload["isError"] != true {
		t.Errorf("isError = %v, want true", payload["isError"])
	}
}

func TestStreamBroadcasterError(t *testing.T) {
	var captured map[string]any
	sb := newStreamBroadcaster(func(event string, data []byte) int {
		json.Unmarshal(data, &captured)
		return 1
	}, "s1", "r1")

	sb.EmitError("something failed")

	payload := captured["payload"].(map[string]any)
	if payload["state"] != "error" {
		t.Errorf("state = %v, want %q", payload["state"], "error")
	}
	if payload["error"] != "something failed" {
		t.Errorf("error = %v, want %q", payload["error"], "something failed")
	}
}

func TestStreamBroadcasterAborted(t *testing.T) {
	var captured map[string]any
	sb := newStreamBroadcaster(func(event string, data []byte) int {
		json.Unmarshal(data, &captured)
		return 1
	}, "s1", "r1")

	sb.EmitAborted("partial text")

	payload := captured["payload"].(map[string]any)
	if payload["state"] != "aborted" {
		t.Errorf("state = %v, want %q", payload["state"], "aborted")
	}
	if payload["text"] != "partial text" {
		t.Errorf("text = %v, want %q", payload["text"], "partial text")
	}
}
