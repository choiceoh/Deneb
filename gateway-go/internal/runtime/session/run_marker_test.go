package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunMarkerStore_WriteReadDelete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewRunMarkerStore(dir)

	m := RunMarker{
		SessionKey:     "telegram:42",
		StartedAt:      time.Now().UnixMilli(),
		LastActivityAt: time.Now().UnixMilli(),
		Channel:        "telegram",
	}

	// Write + Read round-trip.
	if err := store.Write(m); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := store.Read("telegram:42")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got == nil {
		t.Fatal("Read returned nil, want marker")
	}
	if got.SessionKey != m.SessionKey || got.Channel != m.Channel {
		t.Errorf("round-trip mismatch: got %+v want %+v", got, m)
	}

	// Delete.
	if err := store.Delete("telegram:42"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err = store.Read("telegram:42")
	if err != nil {
		t.Fatalf("Read after Delete: %v", err)
	}
	if got != nil {
		t.Errorf("expected marker to be gone, got %+v", got)
	}

	// Delete-on-missing is a no-op.
	if err := store.Delete("never-existed"); err != nil {
		t.Errorf("Delete on missing should be nil, got %v", err)
	}
}

func TestRunMarkerStore_List(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewRunMarkerStore(dir)

	if err := store.Write(RunMarker{SessionKey: "telegram:1", StartedAt: 100}); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(RunMarker{SessionKey: "telegram:2", StartedAt: 200}); err != nil {
		t.Fatal(err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List: got %d want 2: %+v", len(list), list)
	}
}

func TestRunMarkerStore_ListSkipsCorruptFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewRunMarkerStore(dir)

	// Good marker.
	if err := store.Write(RunMarker{SessionKey: "telegram:good", StartedAt: 1}); err != nil {
		t.Fatal(err)
	}
	// Corrupt file: invalid JSON.
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{{{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	list, err := store.List()
	// List returns an error summary for corrupt files but still yields the good one.
	if err == nil {
		t.Errorf("expected corrupt-file summary error")
	}
	if len(list) != 1 {
		t.Errorf("List: got %d markers, want 1", len(list))
	}
	if len(list) == 1 && list[0].SessionKey != "telegram:good" {
		t.Errorf("wrong marker returned: %+v", list[0])
	}
}

func TestRunMarkerStore_IncrementResumeAttempts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewRunMarkerStore(dir)

	if err := store.Write(RunMarker{SessionKey: "telegram:42", StartedAt: 1, ResumeAttempts: 0}); err != nil {
		t.Fatal(err)
	}
	n, err := store.IncrementResumeAttempts("telegram:42")
	if err != nil {
		t.Fatalf("Increment: %v", err)
	}
	if n != 1 {
		t.Errorf("first increment: got %d want 1", n)
	}
	n, err = store.IncrementResumeAttempts("telegram:42")
	if err != nil {
		t.Fatalf("Increment: %v", err)
	}
	if n != 2 {
		t.Errorf("second increment: got %d want 2", n)
	}

	// Persisted to disk?
	m, err := store.Read("telegram:42")
	if err != nil || m == nil {
		t.Fatalf("Read: %v m=%v", err, m)
	}
	if m.ResumeAttempts != 2 {
		t.Errorf("persisted attempts: got %d want 2", m.ResumeAttempts)
	}

	// Missing session: no-op, no error.
	n, err = store.IncrementResumeAttempts("never-existed")
	if err != nil {
		t.Errorf("Increment missing: got %v want nil", err)
	}
	if n != 0 {
		t.Errorf("Increment missing: got %d want 0", n)
	}
}

func TestRunMarkerStore_Touch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewRunMarkerStore(dir)

	original := int64(100)
	if err := store.Write(RunMarker{SessionKey: "telegram:1", StartedAt: 1, LastActivityAt: original}); err != nil {
		t.Fatal(err)
	}

	if err := store.Touch("telegram:1"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	m, err := store.Read("telegram:1")
	if err != nil || m == nil {
		t.Fatalf("Read: %v m=%v", err, m)
	}
	if m.LastActivityAt == original {
		t.Errorf("Touch did not update LastActivityAt (still %d)", m.LastActivityAt)
	}

	// Touch on missing marker is a no-op.
	if err := store.Touch("never-existed"); err != nil {
		t.Errorf("Touch missing: got %v want nil", err)
	}
}

func TestRunMarkerStore_SanitizeKey(t *testing.T) {
	t.Parallel()
	// Path traversal attempts must not escape baseDir.
	dir := t.TempDir()
	store := NewRunMarkerStore(dir)

	// ".." → sanitized away. Slashes/backslashes → underscore.
	nasty := "../../etc/passwd"
	if err := store.Write(RunMarker{SessionKey: nasty, StartedAt: 1}); err != nil {
		t.Fatalf("Write sanitized key: %v", err)
	}
	// File must live inside baseDir.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected single file in baseDir, got %d", len(entries))
	}
	// Round-trip the nasty key — we should find it.
	m, err := store.Read(nasty)
	if err != nil || m == nil {
		t.Fatalf("Read nasty: err=%v m=%v", err, m)
	}
}
