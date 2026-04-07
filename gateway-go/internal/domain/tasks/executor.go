package tasks

import (
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
)

// Default retention period for terminal tasks (7 days).
const defaultRetentionMs = 7 * 24 * 60 * 60 * 1000

// CreateParams holds parameters for creating a new task.
type CreateParams struct {
	Runtime             TaskRuntime
	SourceID            string
	RequesterSessionKey string
	OwnerKey            string
	ScopeKind           ScopeKind
	ChildSessionKey     string
	ParentTaskID        string
	AgentID             string
	RunID               string
	Label               string
	Task                string
	NotifyPolicy        NotifyPolicy
	FlowID              string
}

// CreateQueuedTask creates a new task in queued status.
func CreateQueuedTask(reg *Registry, p CreateParams) (*TaskRecord, error) {
	now := NowMs()
	t := &TaskRecord{
		TaskID:              shortid.New("task"),
		Runtime:             p.Runtime,
		SourceID:            p.SourceID,
		RequesterSessionKey: p.RequesterSessionKey,
		OwnerKey:            resolveOwnerKey(p),
		ScopeKind:           resolveScopeKind(p),
		ChildSessionKey:     p.ChildSessionKey,
		ParentTaskID:        p.ParentTaskID,
		AgentID:             p.AgentID,
		RunID:               p.RunID,
		Label:               p.Label,
		Task:                p.Task,
		Status:              StatusQueued,
		DeliveryStatus:      DeliveryPending,
		NotifyPolicy:        resolveNotifyPolicy(p),
		CreatedAt:           now,
		LastEventAt:         now,
		FlowID:              p.FlowID,
	}

	if err := reg.Put(t); err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	_ = reg.store.AppendEvent(&TaskEventRecord{
		TaskID:  t.TaskID,
		At:      now,
		Kind:    StatusQueued,
		Summary: "task created",
	})

	return t, nil
}

// CreateRunningTask creates a new task already in running status.
func CreateRunningTask(reg *Registry, p CreateParams) (*TaskRecord, error) {
	now := NowMs()
	t := &TaskRecord{
		TaskID:              shortid.New("task"),
		Runtime:             p.Runtime,
		SourceID:            p.SourceID,
		RequesterSessionKey: p.RequesterSessionKey,
		OwnerKey:            resolveOwnerKey(p),
		ScopeKind:           resolveScopeKind(p),
		ChildSessionKey:     p.ChildSessionKey,
		ParentTaskID:        p.ParentTaskID,
		AgentID:             p.AgentID,
		RunID:               p.RunID,
		Label:               p.Label,
		Task:                p.Task,
		Status:              StatusRunning,
		DeliveryStatus:      DeliveryPending,
		NotifyPolicy:        resolveNotifyPolicy(p),
		CreatedAt:           now,
		StartedAt:           now,
		LastEventAt:         now,
		FlowID:              p.FlowID,
	}

	if err := reg.Put(t); err != nil {
		return nil, fmt.Errorf("create running task: %w", err)
	}

	_ = reg.store.AppendEvent(&TaskEventRecord{
		TaskID:  t.TaskID,
		At:      now,
		Kind:    StatusRunning,
		Summary: "task created in running state",
	})

	return t, nil
}

// StartTask transitions a queued task to running.
func StartTask(reg *Registry, taskID string) error {
	t := reg.Get(taskID)
	if t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	if t.Status != StatusQueued && t.Status != StatusBlocked {
		return fmt.Errorf("cannot start task in status %s", t.Status)
	}

	now := NowMs()
	t.Status = StatusRunning
	t.StartedAt = now
	t.LastEventAt = now

	if err := reg.Put(t); err != nil {
		return err
	}

	_ = reg.store.AppendEvent(&TaskEventRecord{
		TaskID:  t.TaskID,
		At:      now,
		Kind:    StatusRunning,
		Summary: "task started",
	})

	return nil
}

// StartTaskByRunID transitions a task identified by runID to running.
func StartTaskByRunID(reg *Registry, runID string) error {
	t := reg.ByRunID(runID)
	if t == nil {
		return fmt.Errorf("task not found for runID: %s", runID)
	}
	return StartTask(reg, t.TaskID)
}

// RecordProgress updates the progress summary of a running task.
func RecordProgress(reg *Registry, taskID, summary string) error {
	t := reg.Get(taskID)
	if t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}

	now := NowMs()
	t.ProgressSummary = summary
	t.LastEventAt = now
	return reg.Put(t)
}

// CompleteTask marks a task as succeeded.
func CompleteTask(reg *Registry, taskID, summary string) error {
	return terminateTask(reg, taskID, StatusSucceeded, summary, "", OutcomeSucceeded)
}

// FailTask marks a task as failed.
func FailTask(reg *Registry, taskID, errMsg, summary string) error {
	return terminateTask(reg, taskID, StatusFailed, summary, errMsg, "")
}

// TimeoutTask marks a task as timed out.
func TimeoutTask(reg *Registry, taskID string) error {
	return terminateTask(reg, taskID, StatusTimedOut, "", "task timed out", "")
}

// CancelTask marks a task as cancelled.
func CancelTask(reg *Registry, taskID string) error {
	t := reg.Get(taskID)
	if t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	if t.Status.IsTerminal() {
		return fmt.Errorf("task already terminal: %s", t.Status)
	}
	return terminateTask(reg, taskID, StatusCancelled, "", "task cancelled", "")
}

// MarkLost marks an active task as lost (orphan detected).
func MarkLost(reg *Registry, taskID string) error {
	t := reg.Get(taskID)
	if t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	if t.Status.IsTerminal() {
		return nil // already done
	}
	return terminateTask(reg, taskID, StatusLost, "", "backing session lost", "")
}

// BlockTask transitions a task to blocked status for later retry.
func BlockTask(reg *Registry, taskID, reason string) error {
	t := reg.Get(taskID)
	if t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	if t.Status.IsTerminal() {
		return fmt.Errorf("task already terminal: %s", t.Status)
	}

	now := NowMs()
	t.Status = StatusBlocked
	t.ProgressSummary = reason
	t.LastEventAt = now

	if err := reg.Put(t); err != nil {
		return err
	}

	_ = reg.store.AppendEvent(&TaskEventRecord{
		TaskID:  t.TaskID,
		At:      now,
		Kind:    StatusBlocked,
		Summary: reason,
	})

	// If the task belongs to a flow, refresh flow counts.
	if t.FlowID != "" {
		_ = reg.RefreshFlowCounts(t.FlowID)
	}

	return nil
}

// SetDeliveryStatus updates the delivery status of a task.
func SetDeliveryStatus(reg *Registry, taskID string, status DeliveryStatus) error {
	t := reg.Get(taskID)
	if t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	t.DeliveryStatus = status
	return reg.Put(t)
}

// DetachToParent marks a detached task's result as delivered back to its parent session.
func DetachToParent(reg *Registry, taskID string) error {
	t := reg.Get(taskID)
	if t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	if t.RequesterSessionKey == "" {
		return fmt.Errorf("task has no requester session: %s", taskID)
	}

	t.DeliveryStatus = DeliverySessionQueued
	t.LastEventAt = NowMs()
	return reg.Put(t)
}

// --- Helpers ---

func terminateTask(reg *Registry, taskID string, status TaskStatus, summary, errMsg string, outcome TerminalOutcome) error {
	t := reg.Get(taskID)
	if t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}

	now := NowMs()
	t.Status = status
	t.EndedAt = now
	t.LastEventAt = now
	t.CleanupAfter = now + defaultRetentionMs

	if summary != "" {
		t.TerminalSummary = summary
	}
	if errMsg != "" {
		t.Error = errMsg
	}
	if outcome != "" {
		t.TerminalOutcome = outcome
	}

	if err := reg.Put(t); err != nil {
		return err
	}

	_ = reg.store.AppendEvent(&TaskEventRecord{
		TaskID:  t.TaskID,
		At:      now,
		Kind:    status,
		Summary: summary,
	})

	// If the task belongs to a flow, refresh flow counts.
	if t.FlowID != "" {
		_ = reg.RefreshFlowCounts(t.FlowID)
	}

	return nil
}

func resolveOwnerKey(p CreateParams) string {
	if p.OwnerKey != "" {
		return p.OwnerKey
	}
	if p.RequesterSessionKey != "" {
		return p.RequesterSessionKey
	}
	return fmt.Sprintf("system:%s:%s", p.Runtime, p.SourceID)
}

func resolveScopeKind(p CreateParams) ScopeKind {
	if p.ScopeKind != "" {
		return p.ScopeKind
	}
	if p.RequesterSessionKey != "" {
		return ScopeSession
	}
	return ScopeSystem
}

func resolveNotifyPolicy(p CreateParams) NotifyPolicy {
	if p.NotifyPolicy != "" {
		return p.NotifyPolicy
	}
	return NotifyDoneOnly
}
