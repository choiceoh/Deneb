package gmailpoll

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestStateStore_LoadEmpty(t *testing.T) {
	dir := t.TempDir()
	store := newStateStore(dir)

	state := testutil.Must(store.Load())
	if state.LastPollAt != 0 {
		t.Errorf("LastPollAt = %d, want 0", state.LastPollAt)
	}
	if len(state.SeenIDs) != 0 {
		t.Errorf("SeenIDs = %v, want empty", state.SeenIDs)
	}
	if state.seenSet == nil {
		t.Error("seenSet should be initialized on Load")
	}
}

func TestStateStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := newStateStore(dir)

	state := &PollState{
		LastPollAt: 1234567890,
		SeenIDs:    []string{"msg1", "msg2", "msg3"},
		seenSet:    make(map[string]struct{}),
	}
	for _, id := range state.SeenIDs {
		state.seenSet[id] = struct{}{}
	}

	if err := store.Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded := testutil.Must(store.Load())
	if loaded.LastPollAt != 1234567890 {
		t.Errorf("LastPollAt = %d, want 1234567890", loaded.LastPollAt)
	}
	if len(loaded.SeenIDs) != 3 {
		t.Errorf("SeenIDs len = %d, want 3", len(loaded.SeenIDs))
	}
	// Verify seenSet is rebuilt on load.
	if !loaded.hasSeen("msg2") {
		t.Error("seenSet should contain msg2 after load")
	}
}

func TestStateStore_TrimSeenIDs(t *testing.T) {
	dir := t.TempDir()
	store := newStateStore(dir)

	// Create state with more than maxSeenIDs entries.
	ids := make([]string, maxSeenIDs+50)
	for i := range ids {
		ids[i] = fmt.Sprintf("msg-%d", i)
	}
	state := &PollState{
		SeenIDs: ids,
		seenSet: make(map[string]struct{}),
	}
	for _, id := range ids {
		state.seenSet[id] = struct{}{}
	}

	if err := store.Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded := testutil.Must(store.Load())
	if len(loaded.SeenIDs) != maxSeenIDs {
		t.Errorf("SeenIDs len = %d, want %d", len(loaded.SeenIDs), maxSeenIDs)
	}
	// The earliest entries should have been trimmed.
	if loaded.hasSeen("msg-0") {
		t.Error("msg-0 should have been trimmed")
	}
	// Latest entry should still exist.
	if !loaded.hasSeen(fmt.Sprintf("msg-%d", maxSeenIDs+49)) {
		t.Error("latest msg should still be present")
	}
}

func TestStateStore_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	store := newStateStore(dir)

	state := &PollState{
		LastPollAt: 42,
		SeenIDs:    []string{"a"},
		seenSet:    map[string]struct{}{"a": {}},
	}
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

func TestPollState_HasSeen_MapLookup(t *testing.T) {
	state := &PollState{
		SeenIDs: []string{"a", "b", "c"},
		seenSet: map[string]struct{}{"a": {}, "b": {}, "c": {}},
	}

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
