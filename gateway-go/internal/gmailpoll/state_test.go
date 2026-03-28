package gmailpoll

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateStore_LoadEmpty(t *testing.T) {
	dir := t.TempDir()
	store := newStateStore(dir)

	state, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if state.LastPollAt != 0 {
		t.Errorf("LastPollAt = %d, want 0", state.LastPollAt)
	}
	if len(state.SeenIDs) != 0 {
		t.Errorf("SeenIDs = %v, want empty", state.SeenIDs)
	}
}

func TestStateStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := newStateStore(dir)

	state := &PollState{
		LastPollAt: 1234567890,
		SeenIDs:    []string{"msg1", "msg2", "msg3"},
	}

	if err := store.Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.LastPollAt != 1234567890 {
		t.Errorf("LastPollAt = %d, want 1234567890", loaded.LastPollAt)
	}
	if len(loaded.SeenIDs) != 3 {
		t.Errorf("SeenIDs len = %d, want 3", len(loaded.SeenIDs))
	}
}

func TestStateStore_TrimSeenIDs(t *testing.T) {
	dir := t.TempDir()
	store := newStateStore(dir)

	// Create state with more than maxSeenIDs entries.
	ids := make([]string, maxSeenIDs+50)
	for i := range ids {
		ids[i] = "msg" + string(rune('0'+i%10))
	}
	state := &PollState{SeenIDs: ids}

	if err := store.Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.SeenIDs) != maxSeenIDs {
		t.Errorf("SeenIDs len = %d, want %d", len(loaded.SeenIDs), maxSeenIDs)
	}
}

func TestStateStore_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	store := newStateStore(dir)

	state := &PollState{LastPollAt: 42, SeenIDs: []string{"a"}}
	if err := store.Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Temp file should not remain.
	tmpPath := filepath.Join(dir, defaultStateFile+".tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("temp file should not exist after save")
	}

	// Main file should exist.
	mainPath := filepath.Join(dir, defaultStateFile)
	if _, err := os.Stat(mainPath); err != nil {
		t.Errorf("state file should exist: %v", err)
	}
}

func TestPollState_HasSeen(t *testing.T) {
	state := &PollState{SeenIDs: []string{"a", "b", "c"}}

	if !state.hasSeen("b") {
		t.Error("hasSeen(b) = false, want true")
	}
	if state.hasSeen("d") {
		t.Error("hasSeen(d) = true, want false")
	}
}

func TestPollState_MarkSeen(t *testing.T) {
	state := &PollState{}
	state.markSeen("x")
	state.markSeen("y")

	if len(state.SeenIDs) != 2 {
		t.Errorf("SeenIDs len = %d, want 2", len(state.SeenIDs))
	}
	if !state.hasSeen("x") || !state.hasSeen("y") {
		t.Error("markSeen did not add IDs correctly")
	}
}
