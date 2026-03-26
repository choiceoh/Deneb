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
	rl.Append(RunLogEntry{Timestamp: 2, Status: "error", DurationMs: 200, Error: "failed"})
	rl.Append(RunLogEntry{Timestamp: 3, Status: "ok", DurationMs: 50, GoalWorked: "g1"})

	recent := rl.Recent(2)
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent entries, got %d", len(recent))
	}
	// Should return newest last.
	if recent[0].Timestamp != 2 {
		t.Errorf("expected entry with ts=2 first, got ts=%d", recent[0].Timestamp)
	}
	if recent[1].Timestamp != 3 {
		t.Errorf("expected entry with ts=3 second, got ts=%d", recent[1].Timestamp)
	}
}

func TestRunLog_RecentAll(t *testing.T) {
	dir := t.TempDir()
	goalPath := filepath.Join(dir, "goals.json")
	rl := NewRunLog(goalPath)

	rl.Append(RunLogEntry{Timestamp: 1, Status: "ok"})

	// Request more than available.
	recent := rl.Recent(10)
	if len(recent) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(recent))
	}
}

func TestRunLog_RecentEmpty(t *testing.T) {
	dir := t.TempDir()
	goalPath := filepath.Join(dir, "goals.json")
	rl := NewRunLog(goalPath)

	recent := rl.Recent(5)
	if recent != nil {
		t.Errorf("expected nil for empty log, got %v", recent)
	}

	recent = rl.Recent(0)
	if recent != nil {
		t.Errorf("expected nil for n=0, got %v", recent)
	}

	recent = rl.Recent(-1)
	if recent != nil {
		t.Errorf("expected nil for n=-1, got %v", recent)
	}
}

func TestRunLog_RingBuffer(t *testing.T) {
	dir := t.TempDir()
	goalPath := filepath.Join(dir, "goals.json")
	rl := NewRunLog(goalPath)

	// Add more than maxRunLogEntries.
	for i := 0; i < maxRunLogEntries+10; i++ {
		rl.Append(RunLogEntry{Timestamp: int64(i), Status: "ok"})
	}

	recent := rl.Recent(maxRunLogEntries + 5)
	if len(recent) != maxRunLogEntries {
		t.Errorf("expected %d entries (ring buffer limit), got %d", maxRunLogEntries, len(recent))
	}

	// First entry should be the 11th one (index 10), not the 1st.
	if recent[0].Timestamp != 10 {
		t.Errorf("expected first entry ts=10, got ts=%d", recent[0].Timestamp)
	}
}

func TestRunLog_Persistence(t *testing.T) {
	dir := t.TempDir()
	goalPath := filepath.Join(dir, "goals.json")

	rl1 := NewRunLog(goalPath)
	rl1.Append(RunLogEntry{Timestamp: 42, Status: "ok", GoalWorked: "g1"})

	// Load fresh instance from same path.
	rl2 := NewRunLog(goalPath)
	recent := rl2.Recent(1)
	if len(recent) != 1 {
		t.Fatalf("expected 1 persisted entry, got %d", len(recent))
	}
	if recent[0].Timestamp != 42 {
		t.Errorf("expected ts=42, got ts=%d", recent[0].Timestamp)
	}
	if recent[0].GoalWorked != "g1" {
		t.Errorf("expected goalWorked=g1, got %q", recent[0].GoalWorked)
	}
}
