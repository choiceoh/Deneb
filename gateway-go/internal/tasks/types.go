// Package tasks implements a unified background task control plane.
//
// It tracks every background unit of work (ACP agents, subagents, cron jobs,
// CLI background runs) in a single SQLite-backed ledger with lifecycle
// tracking, audit, orphan recovery, and parent-task linking.
package tasks

import "time"

// TaskRuntime identifies the execution subsystem that owns a task.
type TaskRuntime string

const (
	RuntimeSubagent TaskRuntime = "subagent"
	RuntimeACP      TaskRuntime = "acp"
	RuntimeCLI      TaskRuntime = "cli"
	RuntimeCron     TaskRuntime = "cron"
)

// TaskStatus is the lifecycle state of a task.
type TaskStatus string

const (
	StatusQueued    TaskStatus = "queued"
	StatusRunning   TaskStatus = "running"
	StatusSucceeded TaskStatus = "succeeded"
	StatusFailed    TaskStatus = "failed"
	StatusTimedOut  TaskStatus = "timed_out"
	StatusCancelled TaskStatus = "cancelled"
	StatusLost      TaskStatus = "lost"
	StatusBlocked   TaskStatus = "blocked"
)

// IsTerminal returns true if the status represents a final state.
func (s TaskStatus) IsTerminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusTimedOut, StatusCancelled, StatusLost:
		return true
	}
	return false
}

// IsActive returns true if the task is still in progress.
func (s TaskStatus) IsActive() bool {
	return s == StatusQueued || s == StatusRunning || s == StatusBlocked
}

// DeliveryStatus tracks whether the task result was delivered to the requester.
type DeliveryStatus string

const (
	DeliveryPending       DeliveryStatus = "pending"
	DeliveryDelivered     DeliveryStatus = "delivered"
	DeliverySessionQueued DeliveryStatus = "session_queued"
	DeliveryFailed        DeliveryStatus = "failed"
	DeliveryParentMissing DeliveryStatus = "parent_missing"
	DeliveryNotApplicable DeliveryStatus = "not_applicable"
)

// NotifyPolicy controls when the requester is notified about task state changes.
type NotifyPolicy string

const (
	NotifyDoneOnly     NotifyPolicy = "done_only"
	NotifyStateChanges NotifyPolicy = "state_changes"
	NotifySilent       NotifyPolicy = "silent"
)

// TerminalOutcome distinguishes success from a blocked outcome (retryable).
type TerminalOutcome string

const (
	OutcomeSucceeded TerminalOutcome = "succeeded"
	OutcomeBlocked   TerminalOutcome = "blocked"
)

// ScopeKind distinguishes session-scoped tasks from system-level ones.
type ScopeKind string

const (
	ScopeSession ScopeKind = "session"
	ScopeSystem  ScopeKind = "system"
)

// TaskRecord is the core data structure for a tracked task.
type TaskRecord struct {
	TaskID              string          `json:"taskId"`
	Runtime             TaskRuntime     `json:"runtime"`
	SourceID            string          `json:"sourceId,omitempty"`
	RequesterSessionKey string          `json:"requesterSessionKey"`
	OwnerKey            string          `json:"ownerKey"`
	ScopeKind           ScopeKind       `json:"scopeKind"`
	ChildSessionKey     string          `json:"childSessionKey,omitempty"`
	ParentTaskID        string          `json:"parentTaskId,omitempty"`
	AgentID             string          `json:"agentId,omitempty"`
	RunID               string          `json:"runId,omitempty"`
	Label               string          `json:"label,omitempty"`
	Task                string          `json:"task"`
	Status              TaskStatus      `json:"status"`
	DeliveryStatus      DeliveryStatus  `json:"deliveryStatus"`
	NotifyPolicy        NotifyPolicy    `json:"notifyPolicy"`
	CreatedAt           int64           `json:"createdAt"`
	StartedAt           int64           `json:"startedAt,omitempty"`
	EndedAt             int64           `json:"endedAt,omitempty"`
	LastEventAt         int64           `json:"lastEventAt,omitempty"`
	CleanupAfter        int64           `json:"cleanupAfter,omitempty"`
	Error               string          `json:"error,omitempty"`
	ProgressSummary     string          `json:"progressSummary,omitempty"`
	TerminalSummary     string          `json:"terminalSummary,omitempty"`
	TerminalOutcome     TerminalOutcome `json:"terminalOutcome,omitempty"`
	FlowID              string          `json:"flowId,omitempty"`
}

// FlowStatus is the lifecycle state of a flow.
type FlowStatus string

const (
	FlowActive    FlowStatus = "active"
	FlowCompleted FlowStatus = "completed"
	FlowFailed    FlowStatus = "failed"
	FlowCancelled FlowStatus = "cancelled"
	FlowBlocked   FlowStatus = "blocked"
)

// FlowRecord represents a higher-level workflow that groups related tasks.
type FlowRecord struct {
	FlowID          string     `json:"flowId"`
	Label           string     `json:"label"`
	Status          FlowStatus `json:"status"`
	OwnerKey        string     `json:"ownerKey"`
	ParentSessionKey string   `json:"parentSessionKey,omitempty"`
	CreatedAt       int64      `json:"createdAt"`
	UpdatedAt       int64      `json:"updatedAt"`
	CompletedAt     int64      `json:"completedAt,omitempty"`
	Error           string     `json:"error,omitempty"`
	TaskCount       int        `json:"taskCount"`
	CompletedCount  int        `json:"completedCount"`
	FailedCount     int        `json:"failedCount"`
}

// TaskEventRecord is an audit trail entry for a task state change.
type TaskEventRecord struct {
	TaskID  string     `json:"taskId"`
	At      int64      `json:"at"`
	Kind    TaskStatus `json:"kind"`
	Summary string     `json:"summary,omitempty"`
}

// RegistrySummary provides aggregate statistics.
type RegistrySummary struct {
	Total    int                       `json:"total"`
	Active   int                       `json:"active"`
	Terminal int                       `json:"terminal"`
	Failures int                       `json:"failures"`
	ByStatus  map[TaskStatus]int       `json:"byStatus"`
	ByRuntime map[TaskRuntime]int      `json:"byRuntime"`
}

// NowMs returns the current time in milliseconds.
func NowMs() int64 {
	return time.Now().UnixMilli()
}
