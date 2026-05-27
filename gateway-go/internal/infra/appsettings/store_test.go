package appsettings

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStore_RoundTrip writes ActiveHome, reopens, and confirms persistence.
// This is the only thing the file actually needs to do — the production code
// reads on boot and writes on /use-forum, never more than once per minute.
func TestStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if got := st.ActiveHome(); got.IsSet() {
		t.Fatalf("fresh store should have empty ActiveHome, got %+v", got)
	}

	want := ActiveHome{ChatID: -1001234567890, Type: "supergroup"}
	if err := st.SetActiveHome(want.ChatID, want.Type); err != nil {
		t.Fatalf("SetActiveHome: %v", err)
	}

	// Reopen to confirm disk persistence (not just in-memory state).
	st2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore reopen: %v", err)
	}
	if got := st2.ActiveHome(); got != want {
		t.Fatalf("after reopen got %+v, want %+v", got, want)
	}
}

// TestStore_MissingFile makes sure a fresh install (no settings file yet)
// boots cleanly rather than refusing to start. This is the most common
// state of the file in production — only set after the user's first migration.
func TestStore_MissingFile(t *testing.T) {
	dir := t.TempDir()
	// Sanity: the file truly does not exist.
	if _, err := os.Stat(filepath.Join(dir, "app-settings.json")); !os.IsNotExist(err) {
		t.Fatalf("test setup error: expected file absent")
	}
	st, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore on empty dir: %v", err)
	}
	if got := st.ActiveHome(); got.IsSet() {
		t.Fatalf("expected unset ActiveHome, got %+v", got)
	}
}

// TestStore_AtomicWrite verifies the tmp+rename pattern leaves no orphan
// .tmp file on success. A leftover .tmp would suggest the write path didn't
// follow through and would slowly fill the data directory.
func TestStore_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir)
	_ = st.SetActiveHome(42, "supergroup")
	if _, err := os.Stat(filepath.Join(dir, "app-settings.json.tmp")); err == nil {
		t.Fatalf("orphan .tmp file left behind after successful save")
	}
}
