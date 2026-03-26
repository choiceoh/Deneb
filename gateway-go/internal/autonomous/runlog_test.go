package autonomous

import (
	"path/filepath"
	"testing"
)

func TestRunLog_AppendAndRecent(t *testing.T) {
	dir := t.TempDir()
	goalPath := filepath.Join(dir, "goals.json")
	rl := NewRunLog(goalPath)

	rl.Append(RunLogEntry{Timestamp: 1, Status: "ok", DurationMs: 100})
	rl.Append(RunLogEntry{Timestamp: 2, Status: "error", DurationMs: 200, Error: "fail"})
	rl.Append(RunLogEntry{Timestamp: 3, Status: "ok", DurationMs: 50, GoalWorked: "g1"})

	recent := rl.Recent(2)
	if len(recent) != 2 {
		t.Fatalf("len = %d, want 2", len(recent))
	}
	// Should return the 2 most recent (newest last).
	if recent[0].Timestamp != 2 {
		t.Errorf("first.ts = %d, want 2", recent[0].Timestamp)
	}
	if recent[1].Timestamp != 3 {
		t.Errorf("second.ts = %d, want 3", recent[1].Timestamp)
	}
}

func TestRunLog_RecentMoreThanAvailable(t *testing.T) {
	dir := t.TempDir()
	rl := NewRunLog(filepath.Join(dir, "goals.json"))
	rl.Append(RunLogEntry{Timestamp: 1, Status: "ok"})

	recent := rl.Recent(10)
	if len(recent) != 1 {
		t.Fatalf("len = %d, want 1", len(recent))
	}
}

func TestRunLog_RecentEmpty(t *testing.T) {
	dir := t.TempDir()
	rl := NewRunLog(filepath.Join(dir, "goals.json"))

	recent := rl.Recent(5)
	if recent != nil {
		t.Errorf("expected nil for empty log, got %v", recent)
	}
}

func TestRunLog_RecentZero(t *testing.T) {
	dir := t.TempDir()
	rl := NewRunLog(filepath.Join(dir, "goals.json"))
	rl.Append(RunLogEntry{Timestamp: 1, Status: "ok"})

	recent := rl.Recent(0)
	if recent != nil {
		t.Fatalf("expected nil for n=0")
	}
}

func TestRunLog_RecentNegative(t *testing.T) {
	dir := t.TempDir()
	rl := NewRunLog(filepath.Join(dir, "goals.json"))
	rl.Append(RunLogEntry{Timestamp: 1, Status: "ok"})

	recent := rl.Recent(-1)
	if recent != nil {
		t.Fatalf("expected nil for n=-1")
	}
}

func TestRunLog_Persistence(t *testing.T) {
	dir := t.TempDir()
	goalPath := filepath.Join(dir, "goals.json")

	rl1 := NewRunLog(goalPath)
	rl1.Append(RunLogEntry{Timestamp: 100, Status: "ok", DurationMs: 50, GoalWorked: "g1"})
	rl1.Append(RunLogEntry{Timestamp: 200, Status: "error", Error: "boom"})

	// New instance should reload from disk.
	rl2 := NewRunLog(goalPath)
	recent := rl2.Recent(10)
	if len(recent) != 2 {
		t.Fatalf("len = %d after reload, want 2", len(recent))
	}
	if recent[0].Timestamp != 100 {
		t.Errorf("first.ts = %d, want 100", recent[0].Timestamp)
	}
	if recent[0].GoalWorked != "g1" {
		t.Errorf("goalWorked = %q, want g1", recent[0].GoalWorked)
	}
}

func TestRunLog_MaxEntries(t *testing.T) {
	dir := t.TempDir()
	rl := NewRunLog(filepath.Join(dir, "goals.json"))

	for i := 0; i < maxRunLogEntries+20; i++ {
		rl.Append(RunLogEntry{Timestamp: int64(i), Status: "ok"})
	}

	recent := rl.Recent(maxRunLogEntries + 10)
	if len(recent) != maxRunLogEntries {
		t.Fatalf("len = %d, want %d", len(recent), maxRunLogEntries)
	}
	// Oldest entry should be 20 (trimmed the first 20).
	if recent[0].Timestamp != 20 {
		t.Errorf("oldest.ts = %d, want 20", recent[0].Timestamp)
	}
}
