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
	path, err := w.SessionPath("test-session")
	if err != nil {
		t.Fatal(err)
	}
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
	path, _ := w.SessionPath("sess1")
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

func TestWriter_AppendMessage_NoSliceMutation(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	_ = w.EnsureSession("sess-mut", SessionHeader{Version: 1, ID: "sess-mut"})

	// Create a message with extra capacity so append could mutate it.
	original := []byte(`{"role":"user"}`)
	msg := make([]byte, len(original), len(original)+10)
	copy(msg, original)

	if err := w.AppendMessage("sess-mut", msg); err != nil {
		t.Fatal(err)
	}

	// The original msg should not have been modified.
	if string(msg) != string(original) {
		t.Errorf("AppendMessage mutated input: got %q, want %q", msg, original)
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

	path, _ := w.SessionPath("sess2")
	lines := readLines(t, path)
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
	path, err := w.SessionPath("my-key")
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join("/base/dir", "my-key.jsonl")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestWriter_SessionPath_PathTraversal(t *testing.T) {
	w := NewWriter("/base/dir", nil)

	cases := []string{
		"../etc/passwd",
		"foo/../bar",
		"foo/bar",
		"foo\\bar",
		"",
	}
	for _, key := range cases {
		_, err := w.SessionPath(key)
		if err == nil {
			t.Errorf("expected error for unsafe session key %q", key)
		}
	}
}

func TestWriter_EnsureSession_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	err := w.EnsureSession("../evil", SessionHeader{Version: 1, ID: "evil"})
	if err == nil {
		t.Error("expected error for path traversal session key")
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

func TestWriter_DeleteSession(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	// Create a session with content.
	w.EnsureSession("del-test", SessionHeader{ID: "del-test", Version: 1})
	w.AppendMessage("del-test", json.RawMessage(`{"role":"user","content":"hello"}`))

	// Verify file exists.
	path, _ := w.SessionPath("del-test")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected session file to exist before delete")
	}

	// Delete should succeed.
	if err := w.DeleteSession("del-test"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	// File should be gone.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected session file to be deleted")
	}

	// Delete again should not error (idempotent).
	if err := w.DeleteSession("del-test"); err != nil {
		t.Errorf("DeleteSession (idempotent): %v", err)
	}
}

func TestWriter_DeleteSession_InvalidKey(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	err := w.DeleteSession("../../etc/passwd")
	if err == nil {
		t.Error("expected error for unsafe session key")
	}
}
