package cron

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// RunOutcome represents the result of a cron job execution.
type RunOutcome struct {
	Status     string          `json:"status"` // "ok", "error", "skipped", "timeout"
	Output     string          `json:"output,omitempty"`
	Error      string          `json:"error,omitempty"`
	Delivery   *DeliveryResult `json:"delivery,omitempty"`
	Retries    int             `json:"retries,omitempty"`
	StartedAt  int64           `json:"startedAt"`
	EndedAt    int64           `json:"endedAt"`
	DurationMs int64           `json:"durationMs"`
}

// AgentRunner abstracts the agent execution so the cron package does not
// depend on chat.Handler or protocol (which pull in CGo/FFI).
type AgentRunner interface {
	// RunAgentTurn executes an agent turn for a cron job and returns the text output.
	// It blocks until the agent completes or the context is canceled.
	RunAgentTurn(ctx context.Context, params AgentTurnParams) (output string, err error)
}

// AgentTurnParams holds parameters for a single cron agent turn.
type AgentTurnParams struct {
	SessionKey  string
	SessionKind session.Kind // session kind (KindCron, etc.)
	AgentID     string
	Command     string
	Channel     string
	To          string
	AccountID   string
	ThreadID    string
}

// CronEvent describes a cron system event for listeners.
type CronEvent struct {
	Type   string `json:"type"` // "job_started", "job_finished", "job_failed", "job_added", "job_removed"
	JobID  string `json:"jobId,omitempty"`
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
	Ts     int64  `json:"ts"` //nolint:staticcheck // ST1003 — JSON field name
}

// CronEventListener receives cron events.
type CronEventListener func(event CronEvent)

// TranscriptCloner copies recent messages from one session to another.
// Typically satisfied by chat.TranscriptStore.CloneRecent.
type TranscriptCloner interface {
	CloneRecent(srcKey, dstKey string, limit int) error
}

// ServiceConfig configures the cron service.
type ServiceConfig struct {
	StorePath      string
	DefaultChannel string
	DefaultTo      string
	Enabled        bool
	RetentionMs    int64 // session retention (0 = default 24h)
	TelegramPlugin *telegram.Plugin
	Sessions       *session.Manager // session manager for cron run sessions

	// When MainSessionKey and TranscriptCloner are set,
	// subagent cron runs can inherit recent conversation context.
	MainSessionKey   string           // primary user session key for cloning context
	TranscriptCloner TranscriptCloner // clones transcript messages between sessions

	// SubagentPoller polls for descendant subagent completion after cron agent turns.
	// When a cron job's agent produces an interim response (e.g., "확인 중"),
	// the poller waits for descendant subagents to finish. Nil disables polling.
	SubagentPoller SubagentPoller
}

// ServiceStatus is a snapshot of the cron service health and pending jobs.
type ServiceStatus struct {
	Running     bool  `json:"running"`
	TaskCount   int   `json:"taskCount"`
	NextRunAtMs int64 `json:"nextRunAtMs,omitempty"`
}

// ListOptions controls simple job list queries (no pagination).
type ListOptions struct {
	IncludeDisabled bool
}

// ListPageOptions controls paginated job list queries with optional filtering and sorting.
type ListPageOptions struct {
	Limit           int
	Offset          int
	IncludeDisabled bool
	Query           string // text search across name, ID, payload
	SortBy          string // "name", "nextRunAtMs", "updatedAtMs" (default: nextRunAtMs)
	SortDir         string // "asc" or "desc" (default: asc)
}

// ListPageResult is a single page of jobs returned by a paginated list query.
type ListPageResult struct {
	Jobs    []StoreJob `json:"jobs"`
	Total   int        `json:"total"`
	Offset  int        `json:"offset"`
	Limit   int        `json:"limit"`
	HasMore bool       `json:"hasMore"`
}
