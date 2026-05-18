package cron

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestStoreLoadEmpty(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	s := NewStore(storePath)

	store := testutil.Must(s.Load())
	if store.Version != 1 {
		t.Errorf("version = %d, want 1", store.Version)
	}
	if len(store.Jobs) != 0 {
		t.Errorf("got %d, want 0 jobs", len(store.Jobs))
	}
}

func TestStoreAddAndGet(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	s := NewStore(storePath)

	job := StoreJob{
		ID:       "test-1",
		Name:     "Test Job",
		Enabled:  true,
		Schedule: StoreSchedule{Kind: "every", EveryMs: 60000},
		Payload:  StorePayload{Kind: "agentTurn", Message: "hello"},
	}

	if err := s.AddJob(job); err != nil {
		t.Fatal(err)
	}

	got := s.Job("test-1")
	if got == nil {
		t.Fatal("expected job")
	}
	if got.Name != "Test Job" {
		t.Errorf("name = %q, want 'Test Job'", got.Name)
	}

	// Verify file exists on disk.
	if _, err := os.Stat(storePath); os.IsNotExist(err) {
		t.Error("store file should exist on disk")
	}
}

func TestStoreRemove(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	s := NewStore(storePath)

	s.AddJob(StoreJob{ID: "a", Enabled: true, Payload: StorePayload{Kind: "agentTurn"}})
	s.AddJob(StoreJob{ID: "b", Enabled: true, Payload: StorePayload{Kind: "agentTurn"}})

	if err := s.RemoveJob("a"); err != nil {
		t.Fatal(err)
	}

	if s.Job("a") != nil {
		t.Error("job 'a' should be removed")
	}
	if s.Job("b") == nil {
		t.Error("job 'b' should still exist")
	}
}

func TestStoreUpdateState(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	s := NewStore(storePath)

	s.AddJob(StoreJob{ID: "x", Enabled: true, Payload: StorePayload{Kind: "agentTurn"}})

	err := s.UpdateJobState("x", JobState{
		LastSessionKey:    "cron:x:1000",
		ConsecutiveErrors: 0,
	})
	testutil.NoError(t, err)

	got := s.Job("x")
	if got.State.LastSessionKey != "cron:x:1000" {
		t.Errorf("state.lastSessionKey = %q, want 'cron:x:1000'", got.State.LastSessionKey)
	}
}

func TestStorePersistence(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")

	// Write with one store instance.
	s1 := NewStore(storePath)
	s1.AddJob(StoreJob{ID: "persist", Name: "Persistent", Enabled: true,
		Payload: StorePayload{Kind: "agentTurn", Message: "hi"}})

	// Read with a fresh instance.
	s2 := NewStore(storePath)
	store := testutil.Must(s2.Load())
	if len(store.Jobs) != 1 || store.Jobs[0].ID != "persist" {
		t.Error("expected persisted job")
	}
}

// TestStoreJobByNameLazyLoad guards against the regression flagged on
// PR #1628: JobByName must populate the cache from disk when it has not
// been Loaded yet, so name-based lookups (e.g. POST /api/cron/run) do not
// return false 404s during the startup window before Service.Start runs.
func TestStoreJobByNameLazyLoad(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")

	writer := NewStore(storePath)
	if err := writer.AddJob(StoreJob{
		ID:      "job-1",
		Name:    "email-analysis",
		Enabled: true,
		Payload: StorePayload{Kind: "agentTurn", Message: "analyze"},
	}); err != nil {
		t.Fatal(err)
	}

	// Fresh store instance — cache is nil until something forces a Load.
	reader := NewStore(storePath)
	got, err := reader.JobByName("email-analysis")
	if err != nil {
		t.Fatalf("JobByName returned error: %v", err)
	}
	if got == nil {
		t.Fatal("JobByName returned nil for an on-disk job before explicit Load")
	}
	if got.ID != "job-1" {
		t.Errorf("ID = %q, want %q", got.ID, "job-1")
	}

	// Unknown name on a fresh store returns (nil, nil) — not error.
	reader2 := NewStore(storePath)
	if got, err := reader2.JobByName("does-not-exist"); err != nil || got != nil {
		t.Errorf("JobByName(unknown) = (%v, %v); want (nil, nil)", got, err)
	}
}

// TestStoreJobByNameSurfacesParseError guards against PR #1630 review
// finding: lazy-load must propagate parse/read failures so callers can
// distinguish a corrupt jobs.json from a missing job. Returning nil would
// make the REST endpoint reply 404 for an operational fault.
func TestStoreJobByNameSurfacesParseError(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")

	if err := os.WriteFile(storePath, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}

	reader := NewStore(storePath)
	got, err := reader.JobByName("any")
	if err == nil {
		t.Fatalf("JobByName on corrupt store returned (%v, nil); want non-nil error", got)
	}
	if got != nil {
		t.Errorf("JobByName on error returned non-nil job: %v", got)
	}
}
