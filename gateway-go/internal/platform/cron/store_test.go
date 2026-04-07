package cron

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreLoadEmpty(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	s := NewStore(storePath)

	store, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if store.Version != 1 {
		t.Errorf("version = %d, want 1", store.Version)
	}
	if len(store.Jobs) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(store.Jobs))
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
	if err != nil {
		t.Fatal(err)
	}

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
	store, err := s2.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Jobs) != 1 || store.Jobs[0].ID != "persist" {
		t.Error("expected persisted job")
	}
}

func TestStoreSkipUnchangedWrite(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs.json")
	s := NewStore(storePath)

	job := StoreJob{ID: "same", Enabled: true, Payload: StorePayload{Kind: "agentTurn"}}
	s.AddJob(job)

	// Save again with same content — should skip write.
	store, _ := s.Load()
	s.Save(store)

	// Verify the store file still exists after save.
	_, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("stat after save: %v", err)
	}
}
