package propus

import (
	"encoding/json"
	"testing"
)

func TestMsgText(t *testing.T) {
	msg := MsgText("hello")
	assertMsgType(t, msg, "Text")
	assertDataField(t, msg, "content", "hello")
}

func TestMsgToolStart(t *testing.T) {
	msg := MsgToolStart("grep", "{}")
	assertMsgType(t, msg, "ToolStart")
	assertDataField(t, msg, "name", "grep")
	assertDataField(t, msg, "args", "{}")
}

func TestMsgToolResult(t *testing.T) {
	msg := MsgToolResult("grep", "found 3 matches")
	assertMsgType(t, msg, "ToolResult")
	assertDataField(t, msg, "name", "grep")
	assertDataField(t, msg, "result", "found 3 matches")
}

func TestMsgUsage(t *testing.T) {
	msg := MsgUsage(100, 200, 300)
	assertMsgType(t, msg, "Usage")
	b, _ := json.Marshal(msg)
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)
	data := raw["data"].(map[string]any)
	if int(data["prompt"].(float64)) != 100 {
		t.Fatalf("expected prompt 100, got %v", data["prompt"])
	}
}

func TestMsgDone(t *testing.T) {
	msg := MsgDone()
	assertMsgType(t, msg, "Done")
}

func TestMsgError(t *testing.T) {
	msg := MsgError("something broke")
	assertMsgType(t, msg, "Error")
	assertDataField(t, msg, "message", "something broke")
}

func TestMsgChatCleared(t *testing.T) {
	msg := MsgChatCleared()
	assertMsgType(t, msg, "ChatCleared")
}

func TestMsgPong(t *testing.T) {
	msg := MsgPong()
	assertMsgType(t, msg, "Pong")
}

func TestMsgSessionSaved(t *testing.T) {
	msg := MsgSessionSaved("/tmp/session.jsonl")
	assertMsgType(t, msg, "SessionSaved")
	assertDataField(t, msg, "path", "/tmp/session.jsonl")
}

func TestMsgFile(t *testing.T) {
	msg := MsgFile("test.png", "image/png", 1024, "http://localhost:3710/files/abc")
	assertMsgType(t, msg, "File")
	assertDataField(t, msg, "name", "test.png")
	assertDataField(t, msg, "media_type", "image/png")
	assertDataField(t, msg, "url", "http://localhost:3710/files/abc")
}

func TestMsgTyping(t *testing.T) {
	msg := MsgTyping()
	assertMsgType(t, msg, "Typing")
}

func TestMsgConfigStatus(t *testing.T) {
	msg := MsgConfigStatus("gpt-4", "openai", "running", "propus-123")
	assertMsgType(t, msg, "ConfigStatus")
	assertDataField(t, msg, "model", "gpt-4")
	assertDataField(t, msg, "service", "openai")
	assertDataField(t, msg, "deneb_status", "running")
	assertDataField(t, msg, "conn_id", "propus-123")
}

func TestMsgSessionList(t *testing.T) {
	sessions := []SessionPreview{
		{Key: "propus:conn-1", Title: "프로젝트 분석", UpdatedAt: 1711612800000, MessageCount: 5, Status: "active"},
		{Key: "propus:conn-2", Title: "코드 리뷰", UpdatedAt: 1711526400000, MessageCount: 12, Status: "done"},
	}
	msg := MsgSessionList(sessions)
	assertMsgType(t, msg, "SessionList")

	b, _ := json.Marshal(msg)
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)
	data := raw["data"].(map[string]any)
	sessionsData := data["sessions"].([]any)
	if len(sessionsData) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessionsData))
	}
	first := sessionsData[0].(map[string]any)
	if first["key"] != "propus:conn-1" {
		t.Fatalf("expected key propus:conn-1, got %v", first["key"])
	}
	if first["title"] != "프로젝트 분석" {
		t.Fatalf("expected title 프로젝트 분석, got %v", first["title"])
	}
}

func TestMsgSessionListEmpty(t *testing.T) {
	msg := MsgSessionList(nil)
	assertMsgType(t, msg, "SessionList")

	b, _ := json.Marshal(msg)
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)
	data := raw["data"].(map[string]any)
	if data["sessions"] != nil {
		t.Fatalf("expected nil sessions for empty list")
	}
}

func TestMsgSessionHistory(t *testing.T) {
	history := []SessionHistoryMsg{
		{Role: "user", Content: "안녕"},
		{Role: "assistant", Content: "안녕하세요!"},
	}
	msg := MsgSessionHistory(history)
	assertMsgType(t, msg, "SessionHistory")

	b, _ := json.Marshal(msg)
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)
	data := raw["data"].(map[string]any)
	msgs := data["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

func TestSessionPreviewJSON(t *testing.T) {
	sp := SessionPreview{
		Key:          "propus:test",
		Title:        "테스트 세션",
		UpdatedAt:    1711612800000,
		MessageCount: 10,
		Status:       "idle",
	}
	b, err := json.Marshal(sp)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var got SessionPreview
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if got.Key != sp.Key || got.Title != sp.Title || got.UpdatedAt != sp.UpdatedAt {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", sp, got)
	}
}

// --- helpers ---

func assertMsgType(t *testing.T, msg ServerMessage, expected string) {
	t.Helper()
	if msg.Type != expected {
		t.Fatalf("expected type %q, got %q", expected, msg.Type)
	}
}

func assertDataField(t *testing.T, msg ServerMessage, key, expected string) {
	t.Helper()
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	data, ok := raw["data"]
	if !ok {
		t.Fatalf("no data field in message")
	}
	dataMap, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("data is not an object")
	}
	got, ok := dataMap[key]
	if !ok {
		t.Fatalf("missing key %q in data", key)
	}
	gotStr, ok := got.(string)
	if !ok {
		t.Fatalf("key %q value is not a string: %T", key, got)
	}
	if gotStr != expected {
		t.Fatalf("key %q: expected %q, got %q", key, expected, gotStr)
	}
}
