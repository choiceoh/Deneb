package chat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileTranscriptStore_AppendAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTranscriptStore(dir)

	msg1 := ChatMessage{Role: "user", Content: "hello"}
	msg2 := ChatMessage{Role: "assistant", Content: "hi there"}

	if err := store.Append("test-session", msg1); err != nil {
		t.Fatalf("Append msg1: %v", err)
	}
	if err := store.Append("test-session", msg2); err != nil {
		t.Fatalf("Append msg2: %v", err)
	}

	msgs, total, err := store.Load("test-session", 0)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Errorf("msgs[0].Content = %q", msgs[0].Content)
	}
	if msgs[1].Content != "hi there" {
		t.Errorf("msgs[1].Content = %q", msgs[1].Content)
	}
}

func TestFileTranscriptStore_LoadWithLimit(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTranscriptStore(dir)

	for i := 0; i < 5; i++ {
		store.Append("session", ChatMessage{Role: "user", Content: "msg"})
	}

	msgs, total, err := store.Load("session", 2)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
}

func TestFileTranscriptStore_LoadMissing(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTranscriptStore(dir)

	msgs, total, err := store.Load("nonexistent", 0)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if total != 0 || len(msgs) != 0 {
		t.Errorf("expected empty result, got total=%d msgs=%d", total, len(msgs))
	}
}

func TestFileTranscriptStore_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	store := NewFileTranscriptStore(dir)

	err := store.Append("session", ChatMessage{Role: "user", Content: "test"})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(filepath.Join(dir, "session.jsonl")); err != nil {
		t.Errorf("expected file to exist: %v", err)
	}
}

func TestMemoryTranscriptStore_AppendAndLoad(t *testing.T) {
	store := NewMemoryTranscriptStore()

	store.Append("s1", ChatMessage{Role: "user", Content: "a"})
	store.Append("s1", ChatMessage{Role: "assistant", Content: "b"})

	msgs, total, err := store.Load("s1", 0)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if total != 2 || len(msgs) != 2 {
		t.Errorf("total=%d len=%d", total, len(msgs))
	}

	// Verify independence from store.
	msgs[0].Content = "modified"
	msgs2, _, _ := store.Load("s1", 0)
	if msgs2[0].Content != "a" {
		t.Error("modifying returned slice should not affect store")
	}
}
