package autoreply

import (
	"encoding/json"
	"sync"
	"testing"
)

type capturedEvent struct {
	Event string
	Data  json.RawMessage
}

type eventCapture struct {
	mu     sync.Mutex
	events []capturedEvent
}

func (c *eventCapture) broadcastRaw(event string, data []byte) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	c.events = append(c.events, capturedEvent{Event: event, Data: cp})
	return 1
}

func (c *eventCapture) findEvent(event string) (map[string]any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.events {
		if e.Event == event {
			var outer map[string]any
			_ = json.Unmarshal(e.Data, &outer)
			if payload, ok := outer["payload"].(map[string]any); ok {
				return payload, true
			}
		}
	}
	return nil, false
}

func (c *eventCapture) count(event string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.events {
		if e.Event == event {
			n++
		}
	}
	return n
}

func makeGatewayEvent(event string, payload map[string]any) []byte {
	data, _ := json.Marshal(map[string]any{
		"event":   event,
		"payload": payload,
	})
	return data
}

func TestTranslateDelta(t *testing.T) {
	cap := &eventCapture{}
	tr := NewACPStreamTranslator(cap.broadcastRaw, nil)

	data := makeGatewayEvent("chat.delta", map[string]any{
		"sessionKey":  "acp:test:1",
		"clientRunId": "run-1",
		"delta":       "Hello world",
		"seq":         1,
	})
	tr.TranslateEvent("chat.delta", data)

	payload, ok := cap.findEvent(ACPEventMessage)
	if !ok {
		t.Fatal("expected acp.message event")
	}
	if payload["text"] != "Hello world" {
		t.Errorf("expected text='Hello world', got %v", payload["text"])
	}
	if payload["sessionKey"] != "acp:test:1" {
		t.Errorf("expected sessionKey='acp:test:1', got %v", payload["sessionKey"])
	}
}

func TestTranslateToolStart(t *testing.T) {
	cap := &eventCapture{}
	tr := NewACPStreamTranslator(cap.broadcastRaw, nil)

	data := makeGatewayEvent("chat.tool", map[string]any{
		"sessionKey": "acp:test:1",
		"state":      "started",
		"tool":       "read",
		"toolUseId":  "tool-123",
	})
	tr.TranslateEvent("chat.tool", data)

	payload, ok := cap.findEvent(ACPEventToolCall)
	if !ok {
		t.Fatal("expected acp.tool_call event")
	}
	if payload["tool"] != "read" {
		t.Errorf("expected tool='read', got %v", payload["tool"])
	}
	if payload["toolUseId"] != "tool-123" {
		t.Errorf("expected toolUseId='tool-123', got %v", payload["toolUseId"])
	}
}

func TestTranslateToolResult(t *testing.T) {
	cap := &eventCapture{}
	tr := NewACPStreamTranslator(cap.broadcastRaw, nil)

	data := makeGatewayEvent("chat.tool", map[string]any{
		"sessionKey": "acp:test:1",
		"state":      "completed",
		"tool":       "read",
		"toolUseId":  "tool-456",
		"result":     "file content from /home/user/src/main.go",
		"isError":    false,
	})
	tr.TranslateEvent("chat.tool", data)

	payload, ok := cap.findEvent(ACPEventToolCallUpdate)
	if !ok {
		t.Fatal("expected acp.tool_call_update event")
	}
	if payload["tool"] != "read" {
		t.Errorf("expected tool='read', got %v", payload["tool"])
	}
	if payload["isError"] != false {
		t.Errorf("expected isError=false, got %v", payload["isError"])
	}
	// Should extract file paths from result.
	files, ok := payload["files"].([]any)
	if !ok || len(files) == 0 {
		t.Error("expected files to contain extracted paths")
	}
}

func TestTranslateComplete(t *testing.T) {
	cap := &eventCapture{}
	tr := NewACPStreamTranslator(cap.broadcastRaw, nil)

	data := makeGatewayEvent("chat", map[string]any{
		"sessionKey": "acp:test:1",
		"state":      "done",
		"text":       "Task completed",
		"usage": map[string]any{
			"inputTokens":  1000,
			"outputTokens": 500,
		},
	})
	tr.TranslateEvent("chat", data)

	// Should emit acp.done.
	payload, ok := cap.findEvent(ACPEventDone)
	if !ok {
		t.Fatal("expected acp.done event")
	}
	if payload["stopReason"] != "stop" {
		t.Errorf("expected stopReason='stop', got %v", payload["stopReason"])
	}
	if payload["text"] != "Task completed" {
		t.Errorf("expected text='Task completed', got %v", payload["text"])
	}

	// Should also emit acp.usage_update.
	usage, ok := cap.findEvent(ACPEventUsageUpdate)
	if !ok {
		t.Fatal("expected acp.usage_update event")
	}
	if usage["inputTokens"] != float64(1000) {
		t.Errorf("expected inputTokens=1000, got %v", usage["inputTokens"])
	}
}

func TestTranslateError(t *testing.T) {
	cap := &eventCapture{}
	tr := NewACPStreamTranslator(cap.broadcastRaw, nil)

	data := makeGatewayEvent("chat", map[string]any{
		"sessionKey": "acp:test:1",
		"state":      "error",
		"error":      "model timeout",
	})
	tr.TranslateEvent("chat", data)

	payload, ok := cap.findEvent(ACPEventDone)
	if !ok {
		t.Fatal("expected acp.done event")
	}
	if payload["stopReason"] != "error" {
		t.Errorf("expected stopReason='error', got %v", payload["stopReason"])
	}
}

func TestTranslateAborted(t *testing.T) {
	cap := &eventCapture{}
	tr := NewACPStreamTranslator(cap.broadcastRaw, nil)

	data := makeGatewayEvent("chat", map[string]any{
		"sessionKey": "acp:test:1",
		"state":      "aborted",
		"text":       "partial output",
	})
	tr.TranslateEvent("chat", data)

	payload, ok := cap.findEvent(ACPEventDone)
	if !ok {
		t.Fatal("expected acp.done event")
	}
	if payload["stopReason"] != "cancel" {
		t.Errorf("expected stopReason='cancel', got %v", payload["stopReason"])
	}
}

func TestTranslateNonACPSession(t *testing.T) {
	cap := &eventCapture{}
	tr := NewACPStreamTranslator(cap.broadcastRaw, nil)

	// Non-ACP session key should be ignored.
	data := makeGatewayEvent("chat.delta", map[string]any{
		"sessionKey": "agent:main:main",
		"delta":      "Hello",
	})
	tr.TranslateEvent("chat.delta", data)

	if cap.count(ACPEventMessage) != 0 {
		t.Error("expected no ACP events for non-ACP session")
	}
}

func TestTranslateNilEmit(t *testing.T) {
	// Should not panic with nil emit function.
	tr := NewACPStreamTranslator(nil, nil)
	data := makeGatewayEvent("chat.delta", map[string]any{
		"sessionKey": "acp:test:1",
		"delta":      "test",
	})
	tr.TranslateEvent("chat.delta", data)
}

func TestExtractFileLocations_FromResult(t *testing.T) {
	files := ExtractFileLocations("read", "", "content from /home/user/src/main.go here")
	if len(files) == 0 {
		t.Fatal("expected at least one file path")
	}
	found := false
	for _, f := range files {
		if f == "/home/user/src/main.go" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected /home/user/src/main.go in files, got %v", files)
	}
}

func TestExtractFileLocations_FromInput(t *testing.T) {
	input := `{"path": "/workspace/file.ts"}`
	files := ExtractFileLocations("read", input, "")
	if len(files) != 1 || files[0] != "/workspace/file.ts" {
		t.Errorf("expected [/workspace/file.ts], got %v", files)
	}
}

func TestExtractFileLocations_FiltersSysPaths(t *testing.T) {
	files := ExtractFileLocations("grep", "", "found in /proc/cpuinfo and /home/user/code.go")
	for _, f := range files {
		if f == "/proc/cpuinfo" {
			t.Error("should filter /proc/ paths")
		}
	}
}

func TestExtractFileLocations_Deduplicates(t *testing.T) {
	result := "/home/user/a.go and /home/user/a.go again"
	files := ExtractFileLocations("read", "", result)
	count := 0
	for _, f := range files {
		if f == "/home/user/a.go" {
			count++
		}
	}
	if count > 1 {
		t.Error("expected deduplicated paths")
	}
}
