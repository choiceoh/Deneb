package bridge

import (
	"encoding/json"
	"testing"
)

func TestNewRequestFrame(t *testing.T) {
	params := map[string]string{"text": "hello"}
	frame, err := NewRequestFrame("req-1", "chat.send", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if frame.Type != "req" {
		t.Errorf("expected type=req, got %s", frame.Type)
	}
	if frame.ID != "req-1" {
		t.Errorf("expected id=req-1, got %s", frame.ID)
	}
	if frame.Method != "chat.send" {
		t.Errorf("expected method=chat.send, got %s", frame.Method)
	}

	var p map[string]string
	if err := json.Unmarshal(frame.Params, &p); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if p["text"] != "hello" {
		t.Errorf("expected text=hello, got %s", p["text"])
	}
}

func TestNewRequestFrame_NilParams(t *testing.T) {
	frame, err := NewRequestFrame("req-2", "health", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if frame.Params != nil {
		t.Error("expected nil params")
	}
}

func TestRequestFrameJSON(t *testing.T) {
	frame, _ := NewRequestFrame("req-3", "sessions.list", map[string]int{"limit": 10})
	b, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed["type"] != "req" {
		t.Errorf("expected type=req in JSON")
	}
	if parsed["method"] != "sessions.list" {
		t.Errorf("expected method=sessions.list in JSON")
	}
}

func TestPluginHostNotRunning(t *testing.T) {
	h := New()
	if h.IsRunning() {
		t.Error("new plugin host should not be running")
	}
}
