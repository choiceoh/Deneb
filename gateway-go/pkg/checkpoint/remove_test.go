package checkpoint_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/checkpoint"
)

// TestRemoveSession_DeletesSessionDirAndIndexAndBlobs verifies that
// Manager.RemoveSession wipes the entire on-disk footprint for the session:
// index.jsonl, every blob file, and the session directory itself.
func TestRemoveSession_DeletesSessionDirAndIndexAndBlobs(t *testing.T) {
	root := t.TempDir()
	m := checkpoint.New(root, "session-remove-1")

	// Create a snapshot so the session dir actually has content.
	target := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if _, err := m.Snapshot(context.Background(), target, "fs_write"); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Sanity: directory exists and index.jsonl present.
	sessionDir := m.SessionDir()
	if _, err := os.Stat(sessionDir); err != nil {
		t.Fatalf("session dir should exist before remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "index.jsonl")); err != nil {
		t.Fatalf("index.jsonl should exist: %v", err)
	}

	// Remove.
	if err := m.RemoveSession(); err != nil {
		t.Fatalf("RemoveSession: %v", err)
	}

	// Dir must be gone.
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatalf("session dir should be gone, stat err=%v", err)
	}
}

// TestRemoveSession_Idempotent verifies that calling twice is not an error.
func TestRemoveSession_Idempotent(t *testing.T) {
	root := t.TempDir()
	m := checkpoint.New(root, "session-idem")
	if err := m.RemoveSession(); err != nil {
		t.Fatalf("first RemoveSession: %v", err)
	}
	if err := m.RemoveSession(); err != nil {
		t.Fatalf("second RemoveSession: %v", err)
	}
}

// TestRemoveSessionByID_NoopForMissing verifies that RemoveSessionByID on a
// non-existent session or missing root is not an error.
func TestRemoveSessionByID_NoopForMissing(t *testing.T) {
	root := t.TempDir()
	// Non-existent session under an existing root.
	if err := checkpoint.RemoveSessionByID(root, "never-existed"); err != nil {
		t.Fatalf("RemoveSessionByID on missing session: %v", err)
	}
	// Entirely missing root.
	if err := checkpoint.RemoveSessionByID(filepath.Join(root, "no-such-root"), "some-session"); err != nil {
		t.Fatalf("RemoveSessionByID on missing root: %v", err)
	}
	// Empty root.
	if err := checkpoint.RemoveSessionByID("", "any"); err != nil {
		t.Fatalf("RemoveSessionByID with empty root: %v", err)
	}
}

// TestRemoveSessionByID_RemovesExistingDir verifies RemoveSessionByID actually
// wipes a session that was populated by a Manager.
func TestRemoveSessionByID_RemovesExistingDir(t *testing.T) {
	root := t.TempDir()
	m := checkpoint.New(root, "session-byid")
	target := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := m.Snapshot(context.Background(), target, "fs_write"); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	sessionDir := m.SessionDir()

	if err := checkpoint.RemoveSessionByID(root, "session-byid"); err != nil {
		t.Fatalf("RemoveSessionByID: %v", err)
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatalf("expected dir removed, stat err=%v", err)
	}
}

// TestRemoveSession_IsolationAcrossSessions verifies that removing one
// session's directory does not affect another session under the same root,
// even when the operations overlap in time.
func TestRemoveSession_IsolationAcrossSessions(t *testing.T) {
	root := t.TempDir()

	// Seed session A with real content.
	mA := checkpoint.New(root, "session-A")
	targetA := filepath.Join(t.TempDir(), "A.txt")
	if err := os.WriteFile(targetA, []byte("A"), 0o600); err != nil {
		t.Fatalf("write A: %v", err)
	}
	if _, err := mA.Snapshot(context.Background(), targetA, "fs_write"); err != nil {
		t.Fatalf("snapshot A: %v", err)
	}

	mB := checkpoint.New(root, "session-B")
	targetB := filepath.Join(t.TempDir(), "B.txt")
	if err := os.WriteFile(targetB, []byte("B"), 0o600); err != nil {
		t.Fatalf("write B: %v", err)
	}

	// Concurrent: snapshot on B, remove on A. The two sessions share root but
	// not directory; there must be no interference.
	var wg sync.WaitGroup
	wg.Add(2)
	errCh := make(chan error, 2)
	go func() {
		defer wg.Done()
		if _, err := mB.Snapshot(context.Background(), targetB, "fs_write"); err != nil {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		if err := mA.RemoveSession(); err != nil {
			errCh <- err
		}
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent op error: %v", err)
	}

	// A is gone.
	if _, err := os.Stat(mA.SessionDir()); !os.IsNotExist(err) {
		t.Fatalf("session A should be removed, stat err=%v", err)
	}
	// B still exists and its snapshot survived.
	list, err := mB.List("", 0)
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("session B should have 1 snapshot, got %d", len(list))
	}
}

// TestRemoveSessionByID_DoesNotClimbOutOfRoot verifies that a malicious or
// malformed sessionID with path-escape components is sanitized just like
// Manager construction, so callers cannot accidentally delete a sibling
// directory by passing "../..".
func TestRemoveSessionByID_DoesNotClimbOutOfRoot(t *testing.T) {
	outer := t.TempDir()
	root := filepath.Join(outer, "ckp")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	sibling := filepath.Join(outer, "sibling.txt")
	if err := os.WriteFile(sibling, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write sibling: %v", err)
	}

	// Malicious sessionID. If sanitizeSession fails its job this would delete
	// `outer`. Path sanitizer collapses ".." to "_" so we are safe.
	if err := checkpoint.RemoveSessionByID(root, "../../escape"); err != nil {
		t.Fatalf("RemoveSessionByID: %v", err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("sibling file should still exist after sanitized remove: %v", err)
	}
	if _, err := os.Stat(outer); err != nil {
		t.Fatalf("outer dir should still exist: %v", err)
	}
}
