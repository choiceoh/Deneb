package checkpoint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newTestManager creates a Manager rooted in t.TempDir() for isolated runs.
func newTestManager(t *testing.T, sessionID string, opts ...Option) *Manager {
	t.Helper()
	return New(t.TempDir(), sessionID, opts...)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestSnapshotAndRestoreRoundtrip(t *testing.T) {
	m := newTestManager(t, "session-1")
	tmp := t.TempDir()
	target := filepath.Join(tmp, "hello.txt")
	writeFile(t, target, "version 1")

	snap, err := m.Snapshot(context.Background(), target, "fs_write")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.ID == "" || snap.Seq != 1 {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}

	// Mutate the file.
	writeFile(t, target, "version 2 (mutated)")

	// Restore should bring back "version 1".
	if _, err := m.Restore(context.Background(), snap.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "version 1" {
		t.Fatalf("got %q, want %q", string(data), "version 1")
	}

	// Restore should have created a pre-restore snapshot.
	list, err := m.List(target, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) < 2 {
		t.Fatalf("expected >= 2 snapshots after restore, got %d", len(list))
	}
	// Most recent should be pre-restore.
	if list[0].Reason != "pre-restore" {
		t.Fatalf("list[0].Reason = %q, want pre-restore", list[0].Reason)
	}
}

func TestSnapshotDeduplication(t *testing.T) {
	m := newTestManager(t, "session-dedup")
	target := filepath.Join(t.TempDir(), "same.txt")
	writeFile(t, target, "stable")

	s1, err := m.Snapshot(context.Background(), target, "fs_write")
	if err != nil {
		t.Fatalf("snapshot 1: %v", err)
	}
	s2, err := m.Snapshot(context.Background(), target, "fs_write")
	if err != nil {
		t.Fatalf("snapshot 2: %v", err)
	}
	if s1.ID != s2.ID {
		t.Fatalf("expected dedup, got s1=%s s2=%s", s1.ID, s2.ID)
	}
	// Verify the hash matches the actual file.
	sum := sha256.Sum256([]byte("stable"))
	if s1.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha mismatch: %s", s1.SHA256)
	}

	// Real change should NOT dedup.
	writeFile(t, target, "changed")
	s3, err := m.Snapshot(context.Background(), target, "fs_write")
	if err != nil {
		t.Fatalf("snapshot 3: %v", err)
	}
	if s3.ID == s1.ID {
		t.Fatalf("expected new snapshot after change, got same ID")
	}
}

func TestTombstoneRestoreDeletesFile(t *testing.T) {
	m := newTestManager(t, "session-tomb")
	target := filepath.Join(t.TempDir(), "maybe.txt")

	// Snapshot a non-existent path → tombstone.
	s1, err := m.Snapshot(context.Background(), target, "pre-create")
	if err != nil {
		t.Fatalf("snapshot tombstone: %v", err)
	}
	if !s1.Tombstone {
		t.Fatal("expected tombstone=true")
	}

	// Create the file.
	writeFile(t, target, "new content")

	// Restoring the tombstone should delete the file.
	if _, err := m.Restore(context.Background(), s1.ID); err != nil {
		t.Fatalf("restore tombstone: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected file deleted, stat err=%v", err)
	}
}

func TestRetentionKeepN(t *testing.T) {
	m := newTestManager(t, "session-retN", WithRetentionN(3))
	target := filepath.Join(t.TempDir(), "spam.txt")

	// Create 6 distinct snapshots.
	for i := range 6 {
		writeFile(t, target, strings.Repeat("x", i+1))
		if _, err := m.Snapshot(context.Background(), target, "fs_write"); err != nil {
			t.Fatalf("snapshot %d: %v", i, err)
		}
	}

	list, err := m.List(target, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 snapshots after prune, got %d", len(list))
	}
	// Most recent three seqs are 4, 5, 6.
	wantSeqs := []int{6, 5, 4}
	for i, s := range list {
		if s.Seq != wantSeqs[i] {
			t.Fatalf("list[%d].Seq = %d, want %d", i, s.Seq, wantSeqs[i])
		}
	}

	// Pruned blob files should be gone (ignore .lock sidecars from atomicfile).
	entries, _ := os.ReadDir(filepath.Join(m.SessionDir(), list[0].PathHash))
	var blobs int
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".lock") {
			blobs++
		}
	}
	if blobs != 3 {
		t.Fatalf("expected 3 blob files, got %d (entries=%v)", blobs, entries)
	}
}

func TestRetentionMaxBytes(t *testing.T) {
	// Force a tiny byte cap so the second snapshot should evict the first.
	m := newTestManager(t, "session-retB", WithRetentionN(100), WithMaxBytes(10), WithGzip(false))
	target := filepath.Join(t.TempDir(), "big.txt")

	writeFile(t, target, strings.Repeat("a", 50))
	s1, err := m.Snapshot(context.Background(), target, "fs_write")
	if err != nil {
		t.Fatalf("snapshot 1: %v", err)
	}
	writeFile(t, target, strings.Repeat("b", 50))
	s2, err := m.Snapshot(context.Background(), target, "fs_write")
	if err != nil {
		t.Fatalf("snapshot 2: %v", err)
	}

	list, err := m.List("", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Byte-cap prune should have dropped the older one.
	var haveS1, haveS2 bool
	for _, r := range list {
		if r.ID == s1.ID {
			haveS1 = true
		}
		if r.ID == s2.ID {
			haveS2 = true
		}
	}
	if haveS1 {
		t.Fatalf("expected s1 evicted by byte cap, still present")
	}
	if !haveS2 {
		t.Fatal("expected s2 retained")
	}
}

func TestConcurrentSnapshotSameFile(t *testing.T) {
	m := newTestManager(t, "session-race")
	target := filepath.Join(t.TempDir(), "racy.txt")
	writeFile(t, target, "init")

	const workers = 8
	var wg sync.WaitGroup
	wg.Add(workers)
	errs := make(chan error, workers)
	for i := range workers {
		go func(i int) {
			defer wg.Done()
			if _, err := m.Snapshot(context.Background(), target, "worker"); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent snapshot error: %v", err)
	}

	// Because content is identical across workers, dedup should keep just 1
	// record for the path.
	list, err := m.List(target, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 deduped snapshot, got %d", len(list))
	}
}

func TestSessionIsolation(t *testing.T) {
	root := t.TempDir()
	mA := New(root, "session-A")
	mB := New(root, "session-B")

	target := filepath.Join(t.TempDir(), "shared.txt")
	writeFile(t, target, "A1")
	if _, err := mA.Snapshot(context.Background(), target, "A"); err != nil {
		t.Fatalf("A snapshot: %v", err)
	}
	writeFile(t, target, "B1")
	if _, err := mB.Snapshot(context.Background(), target, "B"); err != nil {
		t.Fatalf("B snapshot: %v", err)
	}

	// Session A should only see its one snapshot.
	listA, _ := mA.List("", 0)
	if len(listA) != 1 || listA[0].Reason != "A" {
		t.Fatalf("session A contamination: %+v", listA)
	}
	listB, _ := mB.List("", 0)
	if len(listB) != 1 || listB[0].Reason != "B" {
		t.Fatalf("session B contamination: %+v", listB)
	}
	// And their session dirs must differ.
	if mA.SessionDir() == mB.SessionDir() {
		t.Fatal("session dirs collide")
	}
}

func TestDiff(t *testing.T) {
	m := newTestManager(t, "session-diff")
	target := filepath.Join(t.TempDir(), "code.txt")
	writeFile(t, target, "line 1\nline 2\nline 3\n")
	snap, err := m.Snapshot(context.Background(), target, "fs_write")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	writeFile(t, target, "line 1\nline 2 changed\nline 3\nline 4\n")

	diff, err := m.Diff(snap.ID)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(diff, "-line 2") || !strings.Contains(diff, "+line 2 changed") {
		t.Fatalf("diff missing expected changes:\n%s", diff)
	}
	if !strings.Contains(diff, "+line 4") {
		t.Fatalf("diff missing new line:\n%s", diff)
	}
}

func TestListLimit(t *testing.T) {
	m := newTestManager(t, "session-limit", WithRetentionN(50))
	target := filepath.Join(t.TempDir(), "log.txt")
	for i := range 5 {
		writeFile(t, target, strings.Repeat("z", i+1))
		if _, err := m.Snapshot(context.Background(), target, "fs_write"); err != nil {
			t.Fatalf("snapshot %d: %v", i, err)
		}
	}
	out, err := m.List(target, 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("limit not applied: got %d", len(out))
	}
}

func TestIndexCorruptionTolerated(t *testing.T) {
	m := newTestManager(t, "session-corrupt")
	target := filepath.Join(t.TempDir(), "x.txt")
	writeFile(t, target, "ok")
	if _, err := m.Snapshot(context.Background(), target, "fs_write"); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Append a garbage trailing line.
	f, err := os.OpenFile(m.indexPath(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	if _, err := f.WriteString("{not valid json}\n"); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	f.Close()

	// List should still return the clean record.
	list, err := m.List("", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 valid record, got %d", len(list))
	}
}

func TestRestoreNonExistentSnapshot(t *testing.T) {
	m := newTestManager(t, "session-missing")
	if _, err := m.Restore(context.Background(), "bogus-id"); err == nil {
		t.Fatal("expected error for missing snapshot id")
	}
}
