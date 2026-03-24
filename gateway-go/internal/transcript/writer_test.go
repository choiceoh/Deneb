package transcript

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriter_EnsureSession(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	err := w.EnsureSession("test-session", SessionHeader{
		Version:   1,
		ID:        "test-session",
		Timestamp: 1700000000000,
		Cwd:       "/tmp",
	})
	if err != nil {
		t.Fatal(err)
	}

	// File should exist.
	path := w.SessionPath("test-session")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected session file to exist")
	}

	// Read and verify header.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var header SessionHeader
	if err := json.Unmarshal([]byte(firstLine(data)), &header); err != nil {
		t.Fatal(err)
	}
	if header.Type != "session" {
		t.Errorf("expected type=session, got %q", header.Type)
	}
	if header.Version != 1 {
		t.Errorf("expected version=1, got %d", header.Version)
	}
	if header.ID != "test-session" {
		t.Errorf("expected id=test-session, got %q", header.ID)
	}

	// Idempotent: calling again should not error or duplicate.
	err = w.EnsureSession("test-session", SessionHeader{ID: "test-session"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWriter_AppendMessage(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	_ = w.EnsureSession("sess1", SessionHeader{Version: 1, ID: "sess1"})

	msg1, _ := json.Marshal(map[string]any{
		"role":    "user",
		"content": "hello",
	})
	msg2, _ := json.Marshal(map[string]any{
		"role":    "assistant",
		"content": "hi there",
	})

	if err := w.AppendMessage("sess1", msg1); err != nil {
		t.Fatal(err)
	}
	if err := w.AppendMessage("sess1", msg2); err != nil {
		t.Fatal(err)
	}

	// Read file and count lines.
	path := w.SessionPath("sess1")
	lines := readLines(t, path)

	// 1 header + 2 messages = 3 lines
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}

	// Verify messages are valid JSON.
	for i, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Errorf("line %d is not valid JSON: %q", i, line)
		}
	}
}

func TestWriter_AppendStructured(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	_ = w.EnsureSession("sess2", SessionHeader{Version: 1, ID: "sess2"})

	type chatMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	err := w.AppendStructured("sess2", chatMsg{Role: "user", Content: "test"})
	if err != nil {
		t.Fatal(err)
	}

	lines := readLines(t, w.SessionPath("sess2"))
	if len(lines) != 2 { // header + 1 message
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
}

func TestWriter_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	_ = w.EnsureSession("sess3", SessionHeader{Version: 1, ID: "sess3"})

	err := w.AppendMessage("sess3", []byte("not valid json {"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestWriter_SessionPath(t *testing.T) {
	w := NewWriter("/base/dir", nil)
	expected := filepath.Join("/base/dir", "my-key.jsonl")
	if w.SessionPath("my-key") != expected {
		t.Errorf("expected %q, got %q", expected, w.SessionPath("my-key"))
	}
}

// --- helpers ---

func firstLine(data []byte) string {
	for i, b := range data {
		if b == '\n' {
			return string(data[:i])
		}
	}
	return string(data)
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
