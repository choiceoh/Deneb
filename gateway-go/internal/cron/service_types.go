package cron

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
}

type ServiceStatus struct {
	Running     bool         `json:"running"`
	TaskCount   int          `json:"taskCount"`
	NextRunAtMs int64        `json:"nextRunAtMs,omitempty"`
	Tasks       []TaskStatus `json:"tasks,omitempty"`
}

type ListOptions struct {
	IncludeDisabled bool
}

type ListPageOptions struct {
	Limit           int
	Offset          int
	IncludeDisabled bool
	Query           string // text search across name, ID, payload
	SortBy          string // "name", "nextRunAtMs", "updatedAtMs" (default: nextRunAtMs)
	SortDir         string // "asc" or "desc" (default: asc)
}

type ListPageResult struct {
	Jobs    []StoreJob `json:"jobs"`
	Total   int        `json:"total"`
	Offset  int        `json:"offset"`
	Limit   int        `json:"limit"`
	HasMore bool       `json:"hasMore"`
}
