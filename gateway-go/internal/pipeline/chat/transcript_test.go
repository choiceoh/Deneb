package chat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestFileTranscriptStore_AppendAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTranscriptStore(dir)

	msg1 := NewTextChatMessage("user", "hello", 0)
	msg2 := NewTextChatMessage("assistant", "hi there", 0)

	if err := store.Append("test-session", msg1); err != nil {
		t.Fatalf("Append msg1: %v", err)
	}
	if err := store.Append("test-session", msg2); err != nil {
		t.Fatalf("Append msg2: %v", err)
	}

	msgs, total, err := store.Load("test-session", 0)
	testutil.NoError(t, err)
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
	if msgs[0].TextContent() != "hello" {
		t.Errorf("msgs[0].Content = %q", msgs[0].TextContent())
	}
	if msgs[1].TextContent() != "hi there" {
		t.Errorf("msgs[1].Content = %q", msgs[1].TextContent())
	}
}

func TestFileTranscriptStore_LoadWithLimit(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTranscriptStore(dir)

	for range 5 {
		store.Append("session", NewTextChatMessage("user", "msg", 0))
	}

	msgs, total, err := store.Load("session", 2)
	testutil.NoError(t, err)
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
}

func TestFileTranscriptStore_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	store := NewFileTranscriptStore(dir)

	err := store.Append("session", NewTextChatMessage("user", "test", 0))
	testutil.NoError(t, err)

	// Verify file exists.
	if _, err := os.Stat(filepath.Join(dir, "session.jsonl")); err != nil {
		t.Errorf("expected file to exist: %v", err)
	}
}
