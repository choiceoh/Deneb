package polaris

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

func TestSweepExpired_RemovesOnlyStaleMessageFiles(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "polaris.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Stale session: message file + summary file, mtime pushed 60 days back.
	if err := store.AppendMessage("cron:old:123", toolctx.ChatMessage{Role: "user", Content: json.RawMessage(`"오래된 작업"`)}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := os.WriteFile(store.summariesPath("cron:old:123"), []byte(`[]`), 0o644); err != nil {
		t.Fatalf("write summary: %v", err)
	}
	old := time.Now().Add(-60 * 24 * time.Hour)
	if err := os.Chtimes(store.messagesPath("cron:old:123"), old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	// Drop the in-memory entry so the sweep sees it as not-loaded (a real
	// startup sweep runs before any session is touched).
	store.mu.Lock()
	delete(store.sessions, "cron:old:123")
	store.mu.Unlock()

	// Fresh session: must survive.
	if err := store.AppendMessage("client:main", toolctx.ChatMessage{Role: "user", Content: json.RawMessage(`"오늘 일정"`)}); err != nil {
		t.Fatalf("append fresh: %v", err)
	}

	if got := store.SweepExpired(45*24*time.Hour, nil); got != 1 {
		t.Fatalf("SweepExpired removed %d files, want 1", got)
	}
	if _, err := os.Stat(store.messagesPath("cron:old:123")); !os.IsNotExist(err) {
		t.Error("stale message file should be removed")
	}
	if _, err := os.Stat(store.summariesPath("cron:old:123")); err != nil {
		t.Error("summary file must be KEPT — it is the session's condensed memory")
	}
	if _, err := os.Stat(store.messagesPath("client:main")); err != nil {
		t.Error("fresh message file should survive the sweep")
	}
}

func TestSweepExpired_SkipsLoadedSessionsAndDisabled(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "polaris.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.AppendMessage("client:main", toolctx.ChatMessage{Role: "user", Content: json.RawMessage(`"활성 세션"`)}); err != nil {
		t.Fatalf("append: %v", err)
	}
	old := time.Now().Add(-90 * 24 * time.Hour)
	if err := os.Chtimes(store.messagesPath("client:main"), old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Loaded in memory (AppendMessage populated the session map) → skipped
	// even though the file looks ancient.
	if got := store.SweepExpired(45*24*time.Hour, nil); got != 0 {
		t.Errorf("loaded session swept: removed=%d", got)
	}

	// maxAge <= 0 disables the sweep outright.
	store.mu.Lock()
	delete(store.sessions, "client:main")
	store.mu.Unlock()
	if got := store.SweepExpired(0, nil); got != 0 {
		t.Errorf("disabled sweep removed %d files", got)
	}
}
