package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWALRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()

	// Start WAL.
	wal := NewWAL(mgr, WALConfig{Dir: dir, CompactInterval: 0})
	if err := wal.Start(); err != nil {
		t.Fatalf("wal start: %v", err)
	}

	// Create and mutate sessions.
	mgr.Create("s1", KindDirect)
	mgr.ApplyLifecycleEvent("s1", LifecycleEvent{Phase: PhaseStart})
	mgr.ApplyLifecycleEvent("s1", LifecycleEvent{Phase: PhaseEnd})
	mgr.Create("s2", KindCron)

	// Allow events to propagate (EventBus emits synchronously, but let WAL write).
	time.Sleep(10 * time.Millisecond)
	wal.Stop()

	// Verify WAL file exists and has content.
	walPath := filepath.Join(dir, walFileName)
	info, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("wal file: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("wal file is empty")
	}

	// Create a new manager and replay.
	mgr2 := NewManager()
	wal2 := NewWAL(mgr2, WALConfig{Dir: dir, CompactInterval: 0})
	if err := wal2.Start(); err != nil {
		t.Fatalf("wal2 start: %v", err)
	}
	defer wal2.Stop()

	// Verify restored sessions.
	s1 := mgr2.Get("s1")
	if s1 == nil {
		t.Fatal("s1 not restored")
	}
	if s1.Status != StatusDone {
		t.Errorf("s1 status = %q, want %q", s1.Status, StatusDone)
	}
	if s1.Kind != KindDirect {
		t.Errorf("s1 kind = %q, want %q", s1.Kind, KindDirect)
	}

	s2 := mgr2.Get("s2")
	if s2 == nil {
		t.Fatal("s2 not restored")
	}
	if s2.Kind != KindCron {
		t.Errorf("s2 kind = %q, want %q", s2.Kind, KindCron)
	}
}

func TestWALDelete(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()

	wal := NewWAL(mgr, WALConfig{Dir: dir, CompactInterval: 0})
	if err := wal.Start(); err != nil {
		t.Fatalf("wal start: %v", err)
	}

	mgr.Create("s1", KindDirect)
	mgr.Delete("s1")
	time.Sleep(10 * time.Millisecond)
	wal.Stop()

	// Replay should not have s1.
	mgr2 := NewManager()
	wal2 := NewWAL(mgr2, WALConfig{Dir: dir, CompactInterval: 0})
	if err := wal2.Start(); err != nil {
		t.Fatalf("wal2 start: %v", err)
	}
	defer wal2.Stop()

	if mgr2.Get("s1") != nil {
		t.Error("deleted session should not be restored")
	}
}

func TestWALCompaction(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()

	wal := NewWAL(mgr, WALConfig{Dir: dir, CompactInterval: 0})
	if err := wal.Start(); err != nil {
		t.Fatalf("wal start: %v", err)
	}

	// Create sessions.
	mgr.Create("s1", KindDirect)
	mgr.Create("s2", KindDirect)
	mgr.Delete("s1")
	time.Sleep(10 * time.Millisecond)

	// Compact.
	if err := wal.Compact(); err != nil {
		t.Fatalf("compact: %v", err)
	}
	wal.Stop()

	// Replay from snapshot should have only s2.
	mgr2 := NewManager()
	wal2 := NewWAL(mgr2, WALConfig{Dir: dir, CompactInterval: 0})
	if err := wal2.Start(); err != nil {
		t.Fatalf("wal2 start: %v", err)
	}
	defer wal2.Stop()

	if mgr2.Get("s1") != nil {
		t.Error("s1 should not be in snapshot")
	}
	if mgr2.Get("s2") == nil {
		t.Error("s2 should be restored from snapshot")
	}
}

func TestWALCorruptEntry(t *testing.T) {
	dir := t.TempDir()

	// Write a WAL with: valid entry, concatenated pair, truncated junk.
	walPath := filepath.Join(dir, walFileName)
	s1 := `{"op":"set","session":{"key":"s1","kind":"direct","status":"idle","created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"},"ts":1}`
	s2 := `{"op":"set","session":{"key":"s2","kind":"direct","status":"idle","created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"},"ts":2}`
	s3 := `{"op":"set","session":{"key":"s3","kind":"direct","status":"idle","created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"},"ts":3}`

	content := s1 + "\n" + // normal line
		s2 + s3 + "\n" + // two entries concatenated on one line
		`{"op":"set","ses` + "\n" // truncated garbage

	if err := os.WriteFile(walPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write wal: %v", err)
	}

	mgr := NewManager()
	wal := NewWAL(mgr, WALConfig{Dir: dir, CompactInterval: 0})
	if err := wal.Start(); err != nil {
		t.Fatalf("wal start: %v", err)
	}
	defer wal.Stop()

	// s1: normal line — must be restored.
	if mgr.Get("s1") == nil {
		t.Error("s1 not restored (normal line)")
	}
	// s2, s3: concatenated on one line — both should be recovered.
	if mgr.Get("s2") == nil {
		t.Error("s2 not restored (concatenated line)")
	}
	if mgr.Get("s3") == nil {
		t.Error("s3 not restored (concatenated line)")
	}
}

func TestWALEmptyDir(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()

	// Starting with no existing WAL should not error.
	wal := NewWAL(mgr, WALConfig{Dir: dir, CompactInterval: 0})
	if err := wal.Start(); err != nil {
		t.Fatalf("wal start on empty dir: %v", err)
	}
	wal.Stop()

	if mgr.Count() != 0 {
		t.Error("expected 0 sessions")
	}
}
