package cron

import (
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
)

// CronEvent describes a cron system event for listeners.
type CronEvent struct {
	Type   string `json:"type"` // "job_started", "job_finished", "job_failed", "job_added", "job_removed"
	JobID  string `json:"jobId,omitempty"`
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
	Ts     int64  `json:"ts"`
}

// CronEventListener receives cron events.
type CronEventListener func(event CronEvent)

// ServiceConfig configures the cron service.
type ServiceConfig struct {
	StorePath      string
	DefaultChannel string
	DefaultTo      string
	Enabled        bool
	RetentionMs    int64 // session retention (0 = default 24h)
	TelegramPlugin *telegram.Plugin

	// Sessions is the central session manager. When set, cron creates
	// sessions with KindCron instead of ad-hoc key-only tracking.
	Sessions *session.Manager

	// MainSessionKey is the primary user session (e.g. "telegram:<chatId>")
	// used as the clone source for shadow sessions.
	MainSessionKey string

	// TranscriptCloner provides transcript cloning for shadow sessions.
	// When set (along with MainSessionKey), cron jobs with sessionTarget=subagent
	// clone the main session transcript before execution.
	TranscriptCloner TranscriptCloner
}

// ServiceStatus is a snapshot of the cron service health and pending jobs.
type ServiceStatus struct {
	Running     bool         `json:"running"`
	TaskCount   int          `json:"taskCount"`
	NextRunAtMs int64        `json:"nextRunAtMs,omitempty"`
	Tasks       []TaskStatus `json:"tasks,omitempty"`
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
