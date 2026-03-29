// task.go — PeriodicTask interface for registering recurring background work
// within the autonomous service. Tasks are managed alongside dreaming with
// independent intervals, lifecycle, and panic recovery.
package autonomous

import (
	"context"
	"time"
)

// PeriodicTask represents a recurring background job managed by the autonomous service.
type PeriodicTask interface {
	// Name returns a short identifier for logging and status reporting.
	Name() string
	// Interval returns how often the task should run.
	Interval() time.Duration
	// Run executes a single cycle of the task.
	Run(ctx context.Context) error
}

// TaskStatus reports the current state of a registered periodic task.
type TaskStatus struct {
	Name       string `json:"name"`
	Running    bool   `json:"running"`
	LastRunAt  int64  `json:"lastRunAt,omitempty"`  // unix millis
	LastError  string `json:"lastError,omitempty"`
	RunCount   int64  `json:"runCount"`
	ErrorCount int64  `json:"errorCount"`
}
