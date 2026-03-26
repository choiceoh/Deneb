// Package copilot provides a background system monitor that uses the local
// sglang LLM to analyze logs, detect anomalies, and report issues via Telegram.
package copilot

import "context"

// CheckResult is the outcome of a single system check.
type CheckResult struct {
	Name    string `json:"name"`              // Check identifier (e.g., "disk_usage").
	Status  string `json:"status"`            // "ok", "warning", "critical"
	Message string `json:"message"`           // Human-readable one-liner.
	Details string `json:"details,omitempty"` // Extended info (AI analysis, raw data).
}

// ServiceConfig configures the copilot background service.
type ServiceConfig struct {
	CheckIntervalMin int    // Check cycle interval in minutes (default 15).
	SglangBaseURL    string // sglang endpoint (e.g., "http://127.0.0.1:30000/v1").
	SglangModel      string // Model name (e.g., "Qwen/Qwen3.5-35B-A3B").
}

// ServiceStatus is the snapshot returned by Status().
type ServiceStatus struct {
	Running       bool          `json:"running"`
	Enabled       bool          `json:"enabled"`
	LastCheckAt   int64         `json:"lastCheckAt,omitempty"`
	LastResults   []CheckResult `json:"lastResults,omitempty"`
	TotalChecks   int           `json:"totalChecks"`
	TotalWarnings int           `json:"totalWarnings"`
	Uptime        string        `json:"uptime"`
}

// Notifier delivers significant events to the user (e.g., Telegram).
type Notifier interface {
	Notify(ctx context.Context, message string) error
}
