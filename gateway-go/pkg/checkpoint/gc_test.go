package checkpoint_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/checkpoint"
)

func TestCleanupStaleSessions_RemovesOldSessions(t *testing.T) {
	root := t.TempDir()

	stale := filepath.Join(root, "old-session")
	fresh := filepath.Join(root, "new-session")
	mkdirs(t, stale, fresh)

	gcWriteFile(t, filepath.Join(stale, "index.jsonl"), "{}")
	gcWriteFile(t, filepath.Join(fresh, "index.jsonl"), "{}")

	// Backdate the stale session's files so everything under it predates cutoff.
	old := time.Now().Add(-30 * 24 * time.Hour)
	chtimes(t, stale, old)
	chtimes(t, filepath.Join(stale, "index.jsonl"), old)

	res, err := checkpoint.CleanupStaleSessions(context.Background(), root, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("CleanupStaleSessions: %v", err)
	}
	if res.Scanned != 2 {
		t.Fatalf("Scanned = %d, want 2", res.Scanned)
	}
	if res.Removed != 1 {
		t.Fatalf("Removed = %d, want 1", res.Removed)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale session dir still exists: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh session dir unexpectedly removed: %v", err)
	}
}

func TestCleanupStaleSessions_PreservesRecentInsideOtherwiseOldDir(t *testing.T) {
	// If ANY file under a session dir is newer than cutoff, the whole dir is
	// kept (conservative policy).
	root := t.TempDir()

	sess := filepath.Join(root, "session-A")
	mkdirs(t, sess)

	old := filepath.Join(sess, "old.blob")
	newF := filepath.Join(sess, "new.blob")
	gcWriteFile(t, old, "x")
	gcWriteFile(t, newF, "y")

	chtimes(t, sess, time.Now().Add(-30*24*time.Hour))
	chtimes(t, old, time.Now().Add(-30*24*time.Hour))
	// new.blob keeps default mtime (now).

	res, err := checkpoint.CleanupStaleSessions(context.Background(), root, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("CleanupStaleSessions: %v", err)
	}
	if res.Removed != 0 {
		t.Fatalf("Removed = %d, want 0 (recent file inside)", res.Removed)
	}
	if _, err := os.Stat(sess); err != nil {
		t.Fatalf("session dir unexpectedly removed: %v", err)
	}
}

func TestCleanupStaleSessions_MissingRootIsNoOp(t *testing.T) {
	res, err := checkpoint.CleanupStaleSessions(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"), time.Hour)
	if err != nil {
		t.Fatalf("expected no error for missing root, got %v", err)
	}
	if res.Scanned != 0 || res.Removed != 0 {
		t.Fatalf("expected zero result for missing root, got %+v", res)
	}
}

func TestCleanupStaleSessions_EmptyRootIsNoOp(t *testing.T) {
	res, err := checkpoint.CleanupStaleSessions(context.Background(), "", time.Hour)
	if err != nil {
		t.Fatalf("expected no error for empty root, got %v", err)
	}
	if res.Scanned != 0 {
		t.Fatalf("expected zero scanned for empty root, got %d", res.Scanned)
	}
}

func TestCleanupStaleSessions_IgnoresFilesAtRoot(t *testing.T) {
	// Stray files directly under root (not session dirs) must be left alone
	// regardless of their age.
	root := t.TempDir()
	stray := filepath.Join(root, "stray.log")
	gcWriteFile(t, stray, "lingering")
	chtimes(t, stray, time.Now().Add(-90*24*time.Hour))

	res, err := checkpoint.CleanupStaleSessions(context.Background(), root, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("CleanupStaleSessions: %v", err)
	}
	if res.Scanned != 0 || res.Removed != 0 {
		t.Fatalf("stray file triggered GC: %+v", res)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Fatalf("stray file unexpectedly removed: %v", err)
	}
}

// --- helpers -----------------------------------------------------------

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
}

func gcWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func chtimes(t *testing.T, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}
