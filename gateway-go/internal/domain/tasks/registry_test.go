package tasks

import (
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := OpenStore(StoreConfig{
		DatabasePath: filepath.Join(dir, "tasks.json"),
	}, nil)
	testutil.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	store := testStore(t)
	reg := testutil.Must(NewRegistry(store, nil))
	return reg
}

// --- Store Tests ---

func TestStore_OpenAndClose(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(StoreConfig{
		DatabasePath: filepath.Join(dir, "tasks.json"),
	}, nil)
	testutil.NoError(t, err)
	if err := store.Close(); err != nil {
		t.Fatal(err)
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

	got := testutil.Must(store.Task("task-1"))
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

	active := testutil.Must(store.ListActive())
	// queued + running + blocked = 3
	if len(active) != 3 {
		t.Errorf("ListActive() returned %d tasks, want 3", len(active))
	}
}

func TestStore_DeleteTerminalBefore(t *testing.T) {
	store := testStore(t)

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

	pruned := testutil.Must(store.DeleteTerminalBefore(5000))
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}

	got := testutil.Must(store.Task("old-task"))
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

	events := testutil.Must(store.ListEvents("task-1"))
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

	sum := testutil.Must(store.Summary())
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

// --- Registry Tests ---

func TestRegistry_PutAndGet(t *testing.T) {
	reg := testRegistry(t)

	task := &TaskRecord{
		TaskID:          "task-1",
		Runtime:         RuntimeCron,
		OwnerKey:        "owner-a",
		ScopeKind:       ScopeSession,
		RunID:           "run-123",
		ChildSessionKey: "child-sess",
		Task:            "test",
		Status:          StatusRunning,
		DeliveryStatus:  DeliveryPending,
		NotifyPolicy:    NotifyDoneOnly,
		CreatedAt:       1000,
	}

	if err := reg.Put(task); err != nil {
		t.Fatal(err)
	}

	got := reg.Get("task-1")
	if got == nil || got.TaskID != "task-1" {
		t.Fatal("Get by ID failed")
	}

	got = reg.ByRunID("run-123")
	if got == nil || got.TaskID != "task-1" {
		t.Fatal("ByRunID failed")
	}

	got = reg.ByChildSessionKey("child-sess")
	if got == nil || got.TaskID != "task-1" {
		t.Fatal("ByChildSessionKey failed")
	}

	list := reg.ListByOwner("owner-a")
	if len(list) != 1 {
		t.Fatalf("ListByOwner returned %d, want 1", len(list))
	}
}

func TestRegistry_Delete(t *testing.T) {
	reg := testRegistry(t)

	task := &TaskRecord{
		TaskID:         "task-del",
		Runtime:        RuntimeCLI,
		OwnerKey:       "owner",
		ScopeKind:      ScopeSession,
		RunID:          "run-del",
		Task:           "test",
		Status:         StatusQueued,
		DeliveryStatus: DeliveryPending,
		NotifyPolicy:   NotifyDoneOnly,
		CreatedAt:      1000,
	}
	if err := reg.Put(task); err != nil {
		t.Fatal(err)
	}

	if err := reg.Delete("task-del"); err != nil {
		t.Fatal(err)
	}

	if got := reg.Get("task-del"); got != nil {
		t.Error("expected nil after delete")
	}
	if got := reg.ByRunID("run-del"); got != nil {
		t.Error("expected nil in RunID index after delete")
	}
}

func TestRegistry_RestorePersistence(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "tasks.json")

	// Create and populate.
	store1 := testutil.Must(OpenStore(StoreConfig{DatabasePath: cfgPath}, nil))
	reg1 := testutil.Must(NewRegistry(store1, nil))
	if err := reg1.Put(&TaskRecord{
		TaskID:         "persist-1",
		Runtime:        RuntimeCron,
		OwnerKey:       "owner",
		ScopeKind:      ScopeSession,
		Task:           "persist test",
		Status:         StatusRunning,
		DeliveryStatus: DeliveryPending,
		NotifyPolicy:   NotifyDoneOnly,
		CreatedAt:      1000,
	}); err != nil {
		t.Fatal(err)
	}
	store1.Close()

	// Reopen and verify restoration.
	store2 := testutil.Must(OpenStore(StoreConfig{DatabasePath: cfgPath}, nil))
	defer store2.Close()
	reg2 := testutil.Must(NewRegistry(store2, nil))

	got := reg2.Get("persist-1")
	if got == nil {
		t.Fatal("expected task to survive store reopen")
	}
	if got.Task != "persist test" {
		t.Errorf("Task = %q, want %q", got.Task, "persist test")
	}
}

// --- Executor Tests ---

func TestExecutor_CreateAndTransition(t *testing.T) {
	reg := testRegistry(t)

	task, err := CreateQueuedTask(reg, CreateParams{
		Runtime:             RuntimeSubagent,
		RequesterSessionKey: "sess-1",
		Task:                "do something",
	})
	testutil.NoError(t, err)
	if task.Status != StatusQueued {
		t.Errorf("Status = %q, want queued", task.Status)
	}

	if err := StartTask(reg, task.TaskID); err != nil {
		t.Fatal(err)
	}
	got := reg.Get(task.TaskID)
	if got.Status != StatusRunning {
		t.Errorf("Status = %q, want running", got.Status)
	}
	if got.StartedAt == 0 {
		t.Error("StartedAt should be set")
	}

	if err := RecordProgress(reg, task.TaskID, "50% done"); err != nil {
		t.Fatal(err)
	}
	got = reg.Get(task.TaskID)
	if got.ProgressSummary != "50% done" {
		t.Errorf("ProgressSummary = %q, want %q", got.ProgressSummary, "50% done")
	}

	if err := CompleteTask(reg, task.TaskID, "all done"); err != nil {
		t.Fatal(err)
	}
	got = reg.Get(task.TaskID)
	if got.Status != StatusSucceeded {
		t.Errorf("Status = %q, want succeeded", got.Status)
	}
	if got.EndedAt == 0 {
		t.Error("EndedAt should be set")
	}
	if got.CleanupAfter == 0 {
		t.Error("CleanupAfter should be set")
	}
}

func TestExecutor_CancelTask(t *testing.T) {
	reg := testRegistry(t)

	task, err := CreateRunningTask(reg, CreateParams{
		Runtime: RuntimeACP,
		Task:    "cancel me",
	})
	testutil.NoError(t, err)

	if err := CancelTask(reg, task.TaskID); err != nil {
		t.Fatal(err)
	}
	got := reg.Get(task.TaskID)
	if got.Status != StatusCancelled {
		t.Errorf("Status = %q, want cancelled", got.Status)
	}
}

func TestExecutor_BlockAndResume(t *testing.T) {
	reg := testRegistry(t)

	task, err := CreateRunningTask(reg, CreateParams{
		Runtime: RuntimeSubagent,
		Task:    "block me",
	})
	testutil.NoError(t, err)

	if err := BlockTask(reg, task.TaskID, "waiting for input"); err != nil {
		t.Fatal(err)
	}
	got := reg.Get(task.TaskID)
	if got.Status != StatusBlocked {
		t.Errorf("Status = %q, want blocked", got.Status)
	}

	if err := StartTask(reg, task.TaskID); err != nil {
		t.Fatal(err)
	}
	got = reg.Get(task.TaskID)
	if got.Status != StatusRunning {
		t.Errorf("Status = %q, want running after resume", got.Status)
	}
}

func TestExecutor_MarkLost(t *testing.T) {
	reg := testRegistry(t)

	task, err := CreateRunningTask(reg, CreateParams{
		Runtime: RuntimeCLI,
		Task:    "orphan",
	})
	testutil.NoError(t, err)

	if err := MarkLost(reg, task.TaskID); err != nil {
		t.Fatal(err)
	}
	got := reg.Get(task.TaskID)
	if got.Status != StatusLost {
		t.Errorf("Status = %q, want lost", got.Status)
	}
}

// --- Audit Tests ---

func TestAudit_StaleDetection(t *testing.T) {
	reg := testRegistry(t)

	now := int64(100_000_000)

	if err := reg.Put(&TaskRecord{
		TaskID:         "stale-queued",
		Runtime:        RuntimeCron,
		OwnerKey:       "owner",
		ScopeKind:      ScopeSession,
		Task:           "stale",
		Status:         StatusQueued,
		DeliveryStatus: DeliveryPending,
		NotifyPolicy:   NotifyDoneOnly,
		CreatedAt:      now - 20*60*1000,
		LastEventAt:    now - 20*60*1000,
	}); err != nil {
		t.Fatal(err)
	}

	if err := reg.Put(&TaskRecord{
		TaskID:         "stale-running",
		Runtime:        RuntimeSubagent,
		OwnerKey:       "owner",
		ScopeKind:      ScopeSession,
		Task:           "stale running",
		Status:         StatusRunning,
		DeliveryStatus: DeliveryPending,
		NotifyPolicy:   NotifyDoneOnly,
		CreatedAt:      now - 45*60*1000,
		LastEventAt:    now - 45*60*1000,
	}); err != nil {
		t.Fatal(err)
	}

	summary := RunAudit(reg, AuditOptions{Now: now})

	if summary.Total < 2 {
		t.Errorf("Expected at least 2 findings, got %d", summary.Total)
	}
	if summary.ByCode[AuditStaleQueued] != 1 {
		t.Errorf("stale_queued count = %d, want 1", summary.ByCode[AuditStaleQueued])
	}
	if summary.ByCode[AuditStaleRunning] != 1 {
		t.Errorf("stale_running count = %d, want 1", summary.ByCode[AuditStaleRunning])
	}
}

// --- Flow Tests ---

func TestFlow_CreateAndLink(t *testing.T) {
	reg := testRegistry(t)

	flow, err := CreateFlow(reg, CreateFlowParams{
		Label:    "deploy pipeline",
		OwnerKey: "owner-1",
	})
	testutil.NoError(t, err)
	if flow.Status != FlowActive {
		t.Errorf("Status = %q, want active", flow.Status)
	}

	t1, err := CreateRunningTask(reg, CreateParams{
		Runtime: RuntimeCron,
		Task:    "step 1",
		FlowID:  flow.FlowID,
	})
	testutil.NoError(t, err)
	t2, err := CreateQueuedTask(reg, CreateParams{
		Runtime: RuntimeCron,
		Task:    "step 2",
		FlowID:  flow.FlowID,
	})
	testutil.NoError(t, err)

	flowTasks := reg.ListByFlowID(flow.FlowID)
	if len(flowTasks) != 2 {
		t.Fatalf("ListByFlowID returned %d, want 2", len(flowTasks))
	}

	if err := CompleteTask(reg, t1.TaskID, "done"); err != nil {
		t.Fatal(err)
	}
	f := reg.Flow(flow.FlowID)
	if f.CompletedCount != 1 {
		t.Errorf("CompletedCount = %d, want 1", f.CompletedCount)
	}

	if err := StartTask(reg, t2.TaskID); err != nil {
		t.Fatal(err)
	}
	if err := CompleteTask(reg, t2.TaskID, "done"); err != nil {
		t.Fatal(err)
	}
	f = reg.Flow(flow.FlowID)
	if f.Status != FlowCompleted {
		t.Errorf("Flow Status = %q, want completed", f.Status)
	}
}

func TestFlow_BlockedAndResume(t *testing.T) {
	reg := testRegistry(t)

	flow, err := CreateFlow(reg, CreateFlowParams{
		Label:    "blocked flow",
		OwnerKey: "owner",
	})
	testutil.NoError(t, err)

	task, err := CreateRunningTask(reg, CreateParams{
		Runtime: RuntimeSubagent,
		Task:    "blockable step",
		FlowID:  flow.FlowID,
	})
	testutil.NoError(t, err)

	if err := BlockTask(reg, task.TaskID, "waiting"); err != nil {
		t.Fatal(err)
	}
	f := reg.Flow(flow.FlowID)
	if f.Status != FlowBlocked {
		t.Errorf("Flow Status = %q, want blocked", f.Status)
	}

	resumed, err := ResumeBlockedFlow(reg, flow.FlowID)
	testutil.NoError(t, err)
	if resumed != 1 {
		t.Errorf("resumed = %d, want 1", resumed)
	}

	f = reg.Flow(flow.FlowID)
	if f.Status != FlowActive {
		t.Errorf("Flow Status = %q, want active after resume", f.Status)
	}
}

// --- Maintenance Tests ---

func TestMaintenance_OrphanRecovery(t *testing.T) {
	reg := testRegistry(t)

	now := int64(100_000_000)
	oldTime := now - 10*60*1000

	if err := reg.Put(&TaskRecord{
		TaskID:          "orphan-task",
		Runtime:         RuntimeSubagent,
		OwnerKey:        "owner",
		ScopeKind:       ScopeSession,
		ChildSessionKey: "dead-session",
		Task:            "orphaned",
		Status:          StatusRunning,
		DeliveryStatus:  DeliveryPending,
		NotifyPolicy:    NotifyDoneOnly,
		CreatedAt:       oldTime,
		LastEventAt:     oldTime,
	}); err != nil {
		t.Fatal(err)
	}

	hasSession := func(key string) bool { return false }

	result := RunMaintenance(reg, hasSession, now)
	if result.MarkedLost != 1 {
		t.Errorf("MarkedLost = %d, want 1", result.MarkedLost)
	}

	got := reg.Get("orphan-task")
	if got.Status != StatusLost {
		t.Errorf("Status = %q, want lost", got.Status)
	}
}

func TestMaintenance_StampCleanup(t *testing.T) {
	reg := testRegistry(t)

	now := int64(100_000_000)

	if err := reg.Put(&TaskRecord{
		TaskID:         "unstamped",
		Runtime:        RuntimeCLI,
		OwnerKey:       "owner",
		ScopeKind:      ScopeSession,
		Task:           "done but no cleanup",
		Status:         StatusSucceeded,
		DeliveryStatus: DeliveryDelivered,
		NotifyPolicy:   NotifyDoneOnly,
		CreatedAt:      1000,
		EndedAt:        2000,
	}); err != nil {
		t.Fatal(err)
	}

	result := RunMaintenance(reg, nil, now)
	if result.Stamped != 1 {
		t.Errorf("Stamped = %d, want 1", result.Stamped)
	}

	got := reg.Get("unstamped")
	if got.CleanupAfter == 0 {
		t.Error("expected CleanupAfter to be stamped")
	}
}
