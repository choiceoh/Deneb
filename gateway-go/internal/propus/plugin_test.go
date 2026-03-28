package propus

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"
)

func newTestPlugin() *Plugin {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := DefaultConfig()
	return NewPlugin(cfg, logger)
}

func TestNewPlugin(t *testing.T) {
	p := newTestPlugin()
	if p.ID() != "propus" {
		t.Fatalf("expected ID propus, got %s", p.ID())
	}
	if p.Meta().Label != "Propus" {
		t.Fatalf("expected label Propus, got %s", p.Meta().Label)
	}
	caps := p.Capabilities()
	if len(caps.ChatTypes) != 1 || caps.ChatTypes[0] != "coding" {
		t.Fatalf("unexpected chat types: %v", caps.ChatTypes)
	}
}

func TestPlugin_HandleMessage_SendMessage(t *testing.T) {
	p := newTestPlugin()

	var gotSession, gotMessage string
	p.SetChatSend(func(sessionKey, message string) {
		gotSession = sessionKey
		gotMessage = message
	})

	cc := &clientConn{connID: "test-conn-1", lastPong: time.Now()}
	data, _ := json.Marshal(ClientMessage{
		Type: "SendMessage",
		Data: json.RawMessage(`{"text":"hello world"}`),
	})
	p.handleMessage(cc, data)

	if gotSession != "propus:test-conn-1" {
		t.Fatalf("expected session propus:test-conn-1, got %s", gotSession)
	}
	if gotMessage != "hello world" {
		t.Fatalf("expected message 'hello world', got %s", gotMessage)
	}
}

func TestPlugin_HandleMessage_EmptyText(t *testing.T) {
	p := newTestPlugin()

	called := false
	p.SetChatSend(func(_, _ string) {
		called = true
	})

	cc := &clientConn{connID: "test-conn-2", lastPong: time.Now()}
	data, _ := json.Marshal(ClientMessage{
		Type: "SendMessage",
		Data: json.RawMessage(`{"text":""}`),
	})
	p.handleMessage(cc, data)

	if called {
		t.Fatal("chatSend should not be called for empty text")
	}
}

func TestPlugin_HandleMessage_ClearChat(t *testing.T) {
	p := newTestPlugin()

	var abortCalled, clearCalled bool
	p.SetSessionAbort(func(_ string) { abortCalled = true })
	p.SetSessionClear(func(_ string) { clearCalled = true })

	cc := &clientConn{connID: "test-conn-3", lastPong: time.Now()}
	data, _ := json.Marshal(ClientMessage{Type: "ClearChat"})
	p.handleMessage(cc, data)

	if !abortCalled {
		t.Fatal("expected sessionAbort to be called")
	}
	if !clearCalled {
		t.Fatal("expected sessionClear to be called")
	}
}

func TestPlugin_HandleMessage_StopGeneration(t *testing.T) {
	p := newTestPlugin()

	var abortCalled bool
	p.SetSessionAbort(func(_ string) { abortCalled = true })

	cc := &clientConn{connID: "test-conn-4", lastPong: time.Now()}
	data, _ := json.Marshal(ClientMessage{Type: "StopGeneration"})
	p.handleMessage(cc, data)

	if !abortCalled {
		t.Fatal("expected sessionAbort to be called")
	}
}

func TestPlugin_HandleMessage_Ping(t *testing.T) {
	p := newTestPlugin()

	cc := &clientConn{connID: "test-conn-5", lastPong: time.Now().Add(-1 * time.Hour)}
	data, _ := json.Marshal(ClientMessage{Type: "Ping"})
	p.handleMessage(cc, data)

	// Verify lastPong was updated.
	cc.mu.Lock()
	age := time.Since(cc.lastPong)
	cc.mu.Unlock()

	if age > 2*time.Second {
		t.Fatalf("lastPong should be recent, but was %v ago", age)
	}
}

func TestPlugin_HandleMessage_SaveSession(t *testing.T) {
	p := newTestPlugin()

	var savedSession string
	p.SetSessionSave(func(sessionKey string) (string, error) {
		savedSession = sessionKey
		return "/tmp/export.jsonl", nil
	})

	cc := &clientConn{connID: "test-conn-6", lastPong: time.Now()}
	data, _ := json.Marshal(ClientMessage{Type: "SaveSession"})
	p.handleMessage(cc, data)

	if savedSession != "propus:test-conn-6" {
		t.Fatalf("expected session propus:test-conn-6, got %s", savedSession)
	}
}

func TestPlugin_HandleMessage_InvalidJSON(t *testing.T) {
	p := newTestPlugin()
	cc := &clientConn{connID: "test-conn-7", lastPong: time.Now()}
	// Should not panic on invalid JSON.
	p.handleMessage(cc, []byte("not json"))
}

func TestPlugin_BroadcastToSession(t *testing.T) {
	p := newTestPlugin()

	// Register a fake client.
	cc := &clientConn{connID: "test-conn-8", lastPong: time.Now()}
	p.clientsMu.Lock()
	p.clients["test-conn-8"] = cc
	p.clientsMu.Unlock()

	// BroadcastToSession should find the client by trimming "propus:" prefix.
	// (We can't verify the write without a real connection, but we ensure no panic.)
	p.BroadcastToSession("propus:test-conn-8", MsgDone())
}

func TestPlugin_RegisterFile(t *testing.T) {
	p := newTestPlugin()

	fileID := p.RegisterFile("/tmp/test.txt")
	if fileID == "" {
		t.Fatal("expected non-empty file ID")
	}

	p.filesMu.RLock()
	entry, ok := p.files[fileID]
	p.filesMu.RUnlock()

	if !ok {
		t.Fatal("file should be registered")
	}
	if entry.path != "/tmp/test.txt" {
		t.Fatalf("expected path /tmp/test.txt, got %s", entry.path)
	}
	if entry.expiresAt.Before(time.Now()) {
		t.Fatal("file should not be expired yet")
	}
}

func TestPlugin_FileDownloadURL(t *testing.T) {
	cfg := &Config{Port: 3710, Bind: "loopback"}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	p := NewPlugin(cfg, logger)

	url := p.FileDownloadURL("abc123")
	expected := "http://127.0.0.1:3710/files/abc123"
	if url != expected {
		t.Fatalf("expected %s, got %s", expected, url)
	}
}

func TestPlugin_Status_Disabled(t *testing.T) {
	p := newTestPlugin()
	// Default config is disabled.
	_ = p.Start(nil)
	status := p.Status()
	if status.Connected {
		t.Fatal("disabled plugin should not be connected")
	}
}

func TestPlugin_HandleMessage_ListSessions(t *testing.T) {
	p := newTestPlugin()

	var listCalled bool
	p.SetSessionList(func() ([]SessionPreview, error) {
		listCalled = true
		return []SessionPreview{
			{Key: "propus:conn-1", Title: "테스트", UpdatedAt: 1711612800000, MessageCount: 3, Status: "idle"},
		}, nil
	})

	cc := &clientConn{connID: "test-conn-list", lastPong: time.Now()}
	data, _ := json.Marshal(ClientMessage{Type: "ListSessions"})
	p.handleMessage(cc, data)

	if !listCalled {
		t.Fatal("expected sessionList to be called")
	}
}

func TestPlugin_HandleMessage_SwitchSession(t *testing.T) {
	p := newTestPlugin()

	var gotOld, gotNew string
	p.SetSessionSwitch(func(oldKey, newKey string) ([]SessionHistoryMsg, error) {
		gotOld = oldKey
		gotNew = newKey
		return []SessionHistoryMsg{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
		}, nil
	})

	cc := &clientConn{connID: "test-conn-switch", lastPong: time.Now()}
	data, _ := json.Marshal(ClientMessage{
		Type: "SwitchSession",
		Data: json.RawMessage(`{"session_key":"propus:other-conn"}`),
	})
	p.handleMessage(cc, data)

	if gotOld != "propus:test-conn-switch" {
		t.Fatalf("expected old key propus:test-conn-switch, got %s", gotOld)
	}
	if gotNew != "propus:other-conn" {
		t.Fatalf("expected new key propus:other-conn, got %s", gotNew)
	}
}

func TestPlugin_HandleMessage_SwitchSessionEmptyKey(t *testing.T) {
	p := newTestPlugin()

	called := false
	p.SetSessionSwitch(func(_, _ string) ([]SessionHistoryMsg, error) {
		called = true
		return nil, nil
	})

	cc := &clientConn{connID: "test-conn-switch-empty", lastPong: time.Now()}
	data, _ := json.Marshal(ClientMessage{
		Type: "SwitchSession",
		Data: json.RawMessage(`{"session_key":""}`),
	})
	p.handleMessage(cc, data)

	if called {
		t.Fatal("sessionSwitch should not be called for empty key")
	}
}

func TestPlugin_HandleMessage_SearchSessions(t *testing.T) {
	p := newTestPlugin()

	var gotQuery string
	p.SetSessionSearch(func(query string) ([]SessionPreview, error) {
		gotQuery = query
		return []SessionPreview{
			{Key: "propus:conn-1", Title: "검색 결과", MessageCount: 5, Status: "idle"},
		}, nil
	})

	cc := &clientConn{connID: "test-conn-search", lastPong: time.Now()}
	data, _ := json.Marshal(ClientMessage{
		Type: "SearchSessions",
		Data: json.RawMessage(`{"query":"코드 리뷰"}`),
	})
	p.handleMessage(cc, data)

	if gotQuery != "코드 리뷰" {
		t.Fatalf("expected query '코드 리뷰', got '%s'", gotQuery)
	}
}

func TestRandomFileID(t *testing.T) {
	id1 := randomFileID()
	id2 := randomFileID()
	if id1 == id2 {
		t.Fatal("random IDs should be unique")
	}
	if len(id1) != 32 {
		t.Fatalf("expected 32-char hex ID, got %d chars", len(id1))
	}
}
