package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := OpenStore(StoreConfig{
		DatabasePath: filepath.Join(dir, "tasks.db"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// --- Store Tests ---

func TestStore_OpenAndClose(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(StoreConfig{
		DatabasePath: filepath.Join(dir, "tasks.db"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// Verify DB file was created.
	if _, err := os.Stat(filepath.Join(dir, "tasks.db")); err != nil {
		t.Fatalf("expected tasks.db to exist: %v", err)
	}
}

func TestStore_UpsertAndGetTask(t *testing.T) {
	store := testStore(t)

	task := &TaskRecord{
		TaskID:         "task-1",
		Runtime:        RuntimeCron,
		OwnerKey:       "session:abc",
		ScopeKind:      ScopeSession,
		Task:           "run cron job",
		Status:         StatusQueued,
		DeliveryStatus: DeliveryPending,
		NotifyPolicy:   NotifyDoneOnly,
		CreatedAt:      1000,
	}

	if err := store.UpsertTask(task); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetTask("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected task, got nil")
	}
	if got.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", got.TaskID, "task-1")
	}
	if got.Runtime != RuntimeCron {
		t.Errorf("Runtime = %q, want %q", got.Runtime, RuntimeCron)
	}
	if got.Status != StatusQueued {
		t.Errorf("Status = %q, want %q", got.Status, StatusQueued)
	}
}

func TestStore_ListActive(t *testing.T) {
	store := testStore(t)

	for i, status := range []TaskStatus{StatusQueued, StatusRunning, StatusSucceeded, StatusBlocked} {
		task := &TaskRecord{
			TaskID:         "task-" + string(rune('a'+i)),
			Runtime:        RuntimeACP,
			OwnerKey:       "owner",
			ScopeKind:      ScopeSession,
			Task:           "test",
			Status:         status,
			DeliveryStatus: DeliveryPending,
			NotifyPolicy:   NotifyDoneOnly,
			CreatedAt:      int64(1000 + i),
		}
		if err := store.UpsertTask(task); err != nil {
			t.Fatal(err)
		}
	}

	active, err := store.ListActive()
	if err != nil {
		t.Fatal(err)
	}
	// queued + running + blocked = 3
	if len(active) != 3 {
		t.Errorf("ListActive() returned %d tasks, want 3", len(active))
	}
}

func TestStore_DeleteTerminalBefore(t *testing.T) {
	store := testStore(t)

	// Add a terminal task with cleanup_after in the past.
	task := &TaskRecord{
		TaskID:         "old-task",
		Runtime:        RuntimeCLI,
		OwnerKey:       "owner",
		ScopeKind:      ScopeSession,
		Task:           "old",
		Status:         StatusSucceeded,
		DeliveryStatus: DeliveryDelivered,
		NotifyPolicy:   NotifyDoneOnly,
		CreatedAt:      1000,
		EndedAt:        2000,
		CleanupAfter:   3000,
	}
	if err := store.UpsertTask(task); err != nil {
		t.Fatal(err)
	}

	// Delete terminal tasks before t=5000.
	pruned, err := store.DeleteTerminalBefore(5000)
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}

	got, err := store.GetTask("old-task")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected task to be deleted")
	}
}

func TestStore_Events(t *testing.T) {
	store := testStore(t)

	evt1 := &TaskEventRecord{TaskID: "task-1", At: 1000, Kind: StatusQueued, Summary: "created"}
	evt2 := &TaskEventRecord{TaskID: "task-1", At: 2000, Kind: StatusRunning, Summary: "started"}

	if err := store.AppendEvent(evt1); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(evt2); err != nil {
		t.Fatal(err)
	}

	events, err := store.ListEvents("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("ListEvents() returned %d events, want 2", len(events))
	}
	if events[0].Kind != StatusQueued {
		t.Errorf("events[0].Kind = %q, want %q", events[0].Kind, StatusQueued)
	}
	if events[1].Kind != StatusRunning {
		t.Errorf("events[1].Kind = %q, want %q", events[1].Kind, StatusRunning)
	}
}

func TestStore_Summary(t *testing.T) {
	store := testStore(t)

	for _, r := range []struct {
		id      string
		runtime TaskRuntime
		status  TaskStatus
	}{
		{"t1", RuntimeCron, StatusRunning},
		{"t2", RuntimeACP, StatusSucceeded},
		{"t3", RuntimeSubagent, StatusFailed},
		{"t4", RuntimeCLI, StatusQueued},
	} {
		task := &TaskRecord{
			TaskID:         r.id,
			Runtime:        r.runtime,
			OwnerKey:       "owner",
			ScopeKind:      ScopeSession,
			Task:           "test",
			Status:         r.status,
			DeliveryStatus: DeliveryPending,
			NotifyPolicy:   NotifyDoneOnly,
			CreatedAt:      1000,
		}
		if err := store.UpsertTask(task); err != nil {
			t.Fatal(err)
		}
	}

	sum, err := store.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if sum.Total != 4 {
		t.Errorf("Total = %d, want 4", sum.Total)
	}
	if sum.Active != 2 {
		t.Errorf("Active = %d, want 2", sum.Active)
	}
	if sum.Failures != 1 {
		t.Errorf("Failures = %d, want 1", sum.Failures)
	}
}
