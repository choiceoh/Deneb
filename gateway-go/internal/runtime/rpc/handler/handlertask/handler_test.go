package handlertask

import (
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/tasks"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
)

var (
	callMethod    = rpctest.Call
	mustOK        = rpctest.MustOK
	mustErr       = rpctest.MustErr
	extractResult = rpctest.Result
)

// newTestRegistry creates an in-memory registry backed by a temp SQLite store.
func newTestRegistry(t *testing.T) *tasks.Registry {
	t.Helper()
	dir := t.TempDir()
	store, err := tasks.OpenStore(tasks.StoreConfig{
		DatabasePath: filepath.Join(dir, "tasks.db"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	reg, err := tasks.NewRegistry(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// seedTask inserts a running task into the registry and returns it.
func seedTask(t *testing.T, reg *tasks.Registry, label string) *tasks.TaskRecord {
	t.Helper()
	tr, err := tasks.CreateRunningTask(reg, tasks.CreateParams{
		Runtime:  tasks.RuntimeSubagent,
		OwnerKey: "test-owner",
		Task:     "test task: " + label,
		Label:    label,
	})
	if err != nil {
		t.Fatalf("seedTask(%q): %v", label, err)
	}
	return tr
}

// seedTaskWithRunID inserts a running task with a specific runID.
func seedTaskWithRunID(t *testing.T, reg *tasks.Registry, label, runID string) *tasks.TaskRecord {
	t.Helper()
	tr, err := tasks.CreateRunningTask(reg, tasks.CreateParams{
		Runtime:  tasks.RuntimeSubagent,
		OwnerKey: "test-owner",
		Task:     "test task: " + label,
		Label:    label,
		RunID:    runID,
	})
	if err != nil {
		t.Fatalf("seedTaskWithRunID(%q): %v", label, err)
	}
	return tr
}

// seedFlow creates a flow and optionally links tasks to it.
func seedFlow(t *testing.T, reg *tasks.Registry, label string, taskIDs ...string) *tasks.FlowRecord {
	t.Helper()
	f, err := tasks.CreateFlow(reg, tasks.CreateFlowParams{
		Label:    label,
		OwnerKey: "test-owner",
	})
	if err != nil {
		t.Fatalf("seedFlow(%q): %v", label, err)
	}
	for _, id := range taskIDs {
		if err := tasks.LinkTaskToFlow(reg, id, f.FlowID); err != nil {
			t.Fatalf("LinkTaskToFlow(%s, %s): %v", id, f.FlowID, err)
		}
	}
	return f
}

// ─── Methods() registration ─────────────────────────────────────────────────

func TestMethods_nilRegistry_returnsNil(t *testing.T) {
	m := Methods(Deps{Registry: nil})
	if m != nil {
		t.Fatalf("expected nil map when Registry is nil, got %d entries", len(m))
	}
}

func TestMethods_registersAll9Handlers(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	if m == nil {
		t.Fatal("Methods returned nil with valid Registry")
	}

	expected := []string{
		"task.status",
		"task.list",
		"task.get",
		"task.events",
		"task.cancel",
		"task.audit",
		"flow.list",
		"flow.show",
		"flow.cancel",
	}
	for _, name := range expected {
		if _, ok := m[name]; !ok {
			t.Errorf("missing handler %q", name)
		}
	}
	if len(m) != len(expected) {
		t.Errorf("expected %d handlers, got %d", len(expected), len(m))
	}
}

// ─── task.status ────────────────────────────────────────────────────────────

func TestTaskStatus_emptyRegistry(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.status", nil)
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["total"].(float64) != 0 {
		t.Errorf("expected 0 total, got %v", result["total"])
	}
	if result["active"].(float64) != 0 {
		t.Errorf("expected 0 active, got %v", result["active"])
	}
}

func TestTaskStatus_withTasks(t *testing.T) {
	reg := newTestRegistry(t)
	seedTask(t, reg, "alpha")
	seedTask(t, reg, "beta")

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.status", nil)
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["total"].(float64) != 2 {
		t.Errorf("expected 2 total, got %v", result["total"])
	}
	if result["active"].(float64) != 2 {
		t.Errorf("expected 2 active, got %v", result["active"])
	}
}

// ─── task.list ──────────────────────────────────────────────────────────────

func TestTaskList_emptyRegistry(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.list", nil)
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["count"].(float64) != 0 {
		t.Errorf("expected 0 count, got %v", result["count"])
	}
}

func TestTaskList_allTasks(t *testing.T) {
	reg := newTestRegistry(t)
	seedTask(t, reg, "one")
	seedTask(t, reg, "two")

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.list", nil)
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["count"].(float64) != 2 {
		t.Errorf("expected 2 count, got %v", result["count"])
	}
}

func TestTaskList_filterActive(t *testing.T) {
	reg := newTestRegistry(t)
	tr := seedTask(t, reg, "running-one")
	seedTask(t, reg, "running-two")
	// Complete one so it is no longer active.
	if err := tasks.CompleteTask(reg, tr.TaskID, "done"); err != nil {
		t.Fatal(err)
	}

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.list", map[string]any{"active": true})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["count"].(float64) != 1 {
		t.Errorf("expected 1 active, got %v", result["count"])
	}
}

func TestTaskList_filterByRuntime(t *testing.T) {
	reg := newTestRegistry(t)
	seedTask(t, reg, "subagent-task")

	// Create a cron task.
	_, err := tasks.CreateRunningTask(reg, tasks.CreateParams{
		Runtime:  tasks.RuntimeCron,
		OwnerKey: "test-owner",
		Task:     "cron task",
		Label:    "cron-one",
	})
	if err != nil {
		t.Fatal(err)
	}

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.list", map[string]any{"runtime": "cron"})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["count"].(float64) != 1 {
		t.Errorf("expected 1 cron task, got %v", result["count"])
	}
}

func TestTaskList_filterByOwner(t *testing.T) {
	reg := newTestRegistry(t)
	seedTask(t, reg, "owned-task")

	// Create a task with a different owner.
	_, err := tasks.CreateRunningTask(reg, tasks.CreateParams{
		Runtime:  tasks.RuntimeSubagent,
		OwnerKey: "other-owner",
		Task:     "other task",
	})
	if err != nil {
		t.Fatal(err)
	}

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.list", map[string]any{"owner": "test-owner"})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["count"].(float64) != 1 {
		t.Errorf("expected 1 task for test-owner, got %v", result["count"])
	}
}

func TestTaskList_filterByFlowID(t *testing.T) {
	reg := newTestRegistry(t)
	tr := seedTask(t, reg, "flow-task")
	seedTask(t, reg, "standalone-task")
	flow := seedFlow(t, reg, "test-flow", tr.TaskID)

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.list", map[string]any{"flowId": flow.FlowID})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["count"].(float64) != 1 {
		t.Errorf("expected 1 task in flow, got %v", result["count"])
	}
}

func TestTaskList_filterByStatus(t *testing.T) {
	reg := newTestRegistry(t)
	tr := seedTask(t, reg, "to-complete")
	seedTask(t, reg, "still-running")
	if err := tasks.CompleteTask(reg, tr.TaskID, "done"); err != nil {
		t.Fatal(err)
	}

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.list", map[string]any{"status": "succeeded"})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["count"].(float64) != 1 {
		t.Errorf("expected 1 succeeded, got %v", result["count"])
	}
}

// ─── task.get ───────────────────────────────────────────────────────────────

func TestTaskGet_missingParams_error(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.get", nil)
	mustErr(t, resp)
}

func TestTaskGet_emptyParams_error(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.get", map[string]any{})
	mustErr(t, resp)
}

func TestTaskGet_byTaskID(t *testing.T) {
	reg := newTestRegistry(t)
	tr := seedTask(t, reg, "lookup-me")

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.get", map[string]any{"taskId": tr.TaskID})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["taskId"] != tr.TaskID {
		t.Errorf("expected taskId=%s, got %v", tr.TaskID, result["taskId"])
	}
}

func TestTaskGet_byRunID(t *testing.T) {
	reg := newTestRegistry(t)
	tr := seedTaskWithRunID(t, reg, "run-lookup", "run-abc-123")

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.get", map[string]any{"runId": "run-abc-123"})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["taskId"] != tr.TaskID {
		t.Errorf("expected taskId=%s, got %v", tr.TaskID, result["taskId"])
	}
}

func TestTaskGet_taskNotFound(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.get", map[string]any{"taskId": "nonexistent"})
	mustErr(t, resp)
}

func TestTaskGet_runIDNotFound(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.get", map[string]any{"runId": "nonexistent"})
	mustErr(t, resp)
}

// ─── task.events ────────────────────────────────────────────────────────────

func TestTaskEvents_missingTaskID_error(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.events", nil)
	mustErr(t, resp)
}

func TestTaskEvents_emptyTaskID_error(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.events", map[string]any{"taskId": ""})
	mustErr(t, resp)
}

func TestTaskEvents_returnsEvents(t *testing.T) {
	reg := newTestRegistry(t)
	tr := seedTask(t, reg, "has-events")

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.events", map[string]any{"taskId": tr.TaskID})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["taskId"] != tr.TaskID {
		t.Errorf("expected taskId=%s, got %v", tr.TaskID, result["taskId"])
	}
	// CreateRunningTask appends a "running" event, so events should be non-nil.
	events, ok := result["events"].([]any)
	if !ok {
		t.Fatalf("expected events array, got %T", result["events"])
	}
	if len(events) == 0 {
		t.Error("expected at least one event for the created task")
	}
}

func TestTaskEvents_noEventsForUnknownTask(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})

	// A valid taskId that has no events returns an empty list (not an error).
	resp := callMethod(m, "task.events", map[string]any{"taskId": "no-such-task"})
	mustOK(t, resp)
	result := extractResult(t, resp)

	// events should be null or empty.
	if result["events"] != nil {
		events, ok := result["events"].([]any)
		if ok && len(events) != 0 {
			t.Errorf("expected empty events, got %d", len(events))
		}
	}
}

// ─── task.cancel ────────────────────────────────────────────────────────────

func TestTaskCancel_missingTaskID_error(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.cancel", nil)
	mustErr(t, resp)
}

func TestTaskCancel_emptyTaskID_error(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.cancel", map[string]any{"taskId": ""})
	mustErr(t, resp)
}

func TestTaskCancel_success(t *testing.T) {
	reg := newTestRegistry(t)
	tr := seedTask(t, reg, "cancel-me")

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.cancel", map[string]any{"taskId": tr.TaskID})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["cancelled"] != true {
		t.Errorf("expected cancelled=true, got %v", result["cancelled"])
	}
	if result["taskId"] != tr.TaskID {
		t.Errorf("expected taskId=%s, got %v", tr.TaskID, result["taskId"])
	}

	// Verify the task is actually cancelled.
	got := reg.Get(tr.TaskID)
	if got == nil {
		t.Fatal("task disappeared after cancel")
	}
	if got.Status != tasks.StatusCancelled {
		t.Errorf("expected status=cancelled, got %s", got.Status)
	}
}

func TestTaskCancel_nonexistentTask_error(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.cancel", map[string]any{"taskId": "nonexistent"})
	mustErr(t, resp)
}

func TestTaskCancel_alreadyTerminal_error(t *testing.T) {
	reg := newTestRegistry(t)
	tr := seedTask(t, reg, "already-done")
	if err := tasks.CompleteTask(reg, tr.TaskID, "finished"); err != nil {
		t.Fatal(err)
	}

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.cancel", map[string]any{"taskId": tr.TaskID})
	mustErr(t, resp)
}

// ─── task.audit ─────────────────────────────────────────────────────────────

func TestTaskAudit_emptyRegistry(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.audit", nil)
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["total"].(float64) != 0 {
		t.Errorf("expected 0 findings, got %v", result["total"])
	}
}

func TestTaskAudit_returnsFindings(t *testing.T) {
	reg := newTestRegistry(t)
	// Create a task and mark it as lost to trigger a finding.
	tr := seedTask(t, reg, "lost-task")
	if err := tasks.MarkLost(reg, tr.TaskID); err != nil {
		t.Fatal(err)
	}

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.audit", nil)
	mustOK(t, resp)
	result := extractResult(t, resp)

	// The lost task should produce at least one finding.
	if result["total"].(float64) < 1 {
		t.Errorf("expected at least 1 finding, got %v", result["total"])
	}
}

// ─── flow.list ──────────────────────────────────────────────────────────────

func TestFlowList_emptyRegistry(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "flow.list", nil)
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["count"].(float64) != 0 {
		t.Errorf("expected 0 flows, got %v", result["count"])
	}
}

func TestFlowList_allFlows(t *testing.T) {
	reg := newTestRegistry(t)
	seedFlow(t, reg, "flow-a")
	seedFlow(t, reg, "flow-b")

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "flow.list", nil)
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["count"].(float64) != 2 {
		t.Errorf("expected 2 flows, got %v", result["count"])
	}
}

func TestFlowList_filterActive(t *testing.T) {
	reg := newTestRegistry(t)
	seedFlow(t, reg, "active-flow")

	// Create and cancel a second flow.
	f2 := seedFlow(t, reg, "cancelled-flow")
	_, _ = tasks.CancelFlow(reg, f2.FlowID)

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "flow.list", map[string]any{"active": true})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["count"].(float64) != 1 {
		t.Errorf("expected 1 active flow, got %v", result["count"])
	}
}

// ─── flow.show ──────────────────────────────────────────────────────────────

func TestFlowShow_missingFlowID_error(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "flow.show", nil)
	mustErr(t, resp)
}

func TestFlowShow_emptyFlowID_error(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "flow.show", map[string]any{"flowId": ""})
	mustErr(t, resp)
}

func TestFlowShow_notFound(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "flow.show", map[string]any{"flowId": "nonexistent"})
	mustErr(t, resp)
}

func TestFlowShow_success(t *testing.T) {
	reg := newTestRegistry(t)
	tr := seedTask(t, reg, "flow-child")
	flow := seedFlow(t, reg, "my-flow", tr.TaskID)

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "flow.show", map[string]any{"flowId": flow.FlowID})
	mustOK(t, resp)
	result := extractResult(t, resp)

	// flow field is present.
	flowData, ok := result["flow"].(map[string]any)
	if !ok {
		t.Fatalf("expected flow map, got %T", result["flow"])
	}
	if flowData["flowId"] != flow.FlowID {
		t.Errorf("expected flowId=%s, got %v", flow.FlowID, flowData["flowId"])
	}

	// tasks field lists the linked task.
	tasksList, ok := result["tasks"].([]any)
	if !ok {
		t.Fatalf("expected tasks array, got %T", result["tasks"])
	}
	if len(tasksList) != 1 {
		t.Errorf("expected 1 linked task, got %d", len(tasksList))
	}
}

// ─── flow.cancel ────────────────────────────────────────────────────────────

func TestFlowCancel_missingFlowID_error(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "flow.cancel", nil)
	mustErr(t, resp)
}

func TestFlowCancel_emptyFlowID_error(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "flow.cancel", map[string]any{"flowId": ""})
	mustErr(t, resp)
}

func TestFlowCancel_notFound(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "flow.cancel", map[string]any{"flowId": "nonexistent"})
	mustErr(t, resp)
}

func TestFlowCancel_success(t *testing.T) {
	reg := newTestRegistry(t)
	tr := seedTask(t, reg, "flow-task-to-cancel")
	flow := seedFlow(t, reg, "cancel-flow", tr.TaskID)

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "flow.cancel", map[string]any{"flowId": flow.FlowID})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["cancelled"] != true {
		t.Errorf("expected cancelled=true, got %v", result["cancelled"])
	}
	if result["flowId"] != flow.FlowID {
		t.Errorf("expected flowId=%s, got %v", flow.FlowID, result["flowId"])
	}
	if result["tasksCancelled"].(float64) != 1 {
		t.Errorf("expected 1 task cancelled, got %v", result["tasksCancelled"])
	}

	// Verify task is actually cancelled.
	got := reg.Get(tr.TaskID)
	if got == nil {
		t.Fatal("task disappeared after flow cancel")
	}
	if got.Status != tasks.StatusCancelled {
		t.Errorf("expected task status=cancelled, got %s", got.Status)
	}
}

func TestFlowCancel_noActiveTasks(t *testing.T) {
	reg := newTestRegistry(t)
	tr := seedTask(t, reg, "already-completed")
	flow := seedFlow(t, reg, "complete-flow", tr.TaskID)

	// Complete the task before cancelling the flow.
	if err := tasks.CompleteTask(reg, tr.TaskID, "done"); err != nil {
		t.Fatal(err)
	}

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "flow.cancel", map[string]any{"flowId": flow.FlowID})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["tasksCancelled"].(float64) != 0 {
		t.Errorf("expected 0 tasks cancelled, got %v", result["tasksCancelled"])
	}
}

// ─── task.list: combined status + default filter ────────────────────────────

func TestTaskList_statusFilterWithDefault(t *testing.T) {
	reg := newTestRegistry(t)
	tr1 := seedTask(t, reg, "fail-me")
	seedTask(t, reg, "keep-running")

	if err := tasks.FailTask(reg, tr1.TaskID, "oops", "failed"); err != nil {
		t.Fatal(err)
	}

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.list", map[string]any{"status": "failed"})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["count"].(float64) != 1 {
		t.Errorf("expected 1 failed task, got %v", result["count"])
	}
}

// ─── task.get: taskId takes priority over runId ─────────────────────────────

func TestTaskGet_taskIDPriority(t *testing.T) {
	reg := newTestRegistry(t)
	tr := seedTaskWithRunID(t, reg, "primary", "run-id-1")

	m := Methods(Deps{Registry: reg})
	// When both taskId and runId are provided, taskId is used.
	resp := callMethod(m, "task.get", map[string]any{
		"taskId": tr.TaskID,
		"runId":  "run-id-1",
	})
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["taskId"] != tr.TaskID {
		t.Errorf("expected taskId=%s, got %v", tr.TaskID, result["taskId"])
	}
}

// ─── Deterministic output check ─────────────────────────────────────────────

// TestTaskList_nilParams ensures nil params does not crash and returns all.
func TestTaskList_nilParams(t *testing.T) {
	reg := newTestRegistry(t)
	seedTask(t, reg, "solo")

	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "task.list", nil)
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["count"].(float64) != 1 {
		t.Errorf("expected count=1 with nil params, got %v", result["count"])
	}
}

// TestFlowList_nilParams ensures nil params does not crash.
func TestFlowList_nilParams(t *testing.T) {
	reg := newTestRegistry(t)
	m := Methods(Deps{Registry: reg})
	resp := callMethod(m, "flow.list", nil)
	mustOK(t, resp)
	result := extractResult(t, resp)

	if result["count"].(float64) != 0 {
		t.Errorf("expected 0 flows, got %v", result["count"])
	}
}
