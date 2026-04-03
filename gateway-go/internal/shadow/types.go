// Package shadow implements a background monitoring service that observes
// main session conversations and performs background bookkeeping tasks
// (task detection, health monitoring, periodic digests).
package shadow

import (
	"context"
	"time"
)

// TrackedTask is a pending task detected from conversation content.
type TrackedTask struct {
	ID         string `json:"id"`
	Content    string `json:"content"`    // task description
	DetectedAt int64  `json:"detectedAt"` // unix ms
	SessionKey string `json:"sessionKey"`
	Status     string `json:"status"` // "pending", "done", "dismissed"
}

// TopicChange records a detected topic transition in conversation.
type TopicChange struct {
	Topic string `json:"topic"`
	Ts    int64  `json:"ts"`
}

// HealthAlert records a session health issue.
type HealthAlert struct {
	Type    string `json:"type"`    // "session_failed", "session_timeout", "long_running", "repeated_failures"
	Message string `json:"message"` // Korean description
	Ts      int64  `json:"ts"`
}

// ServiceStatus is the snapshot returned by the shadow.status RPC.
type ServiceStatus struct {
	Active       bool   `json:"active"`
	SessionKey   string `json:"sessionKey"`
	MonitoredKey string `json:"monitoredKey"`
	StartedAt    int64  `json:"startedAt"`
	PendingTasks int    `json:"pendingTasks"`
	Alerts       int    `json:"alerts"`
	LastActivity int64  `json:"lastActivity"`
}

// EventListener receives shadow lifecycle events.
type EventListener func(event ShadowEvent)

// ShadowEvent describes a lifecycle event for external consumers (broadcast).
type ShadowEvent struct {
	Type    string `json:"type"` // "task_detected", "health_alert", "digest"
	Payload any    `json:"payload,omitempty"`
	Ts      int64  `json:"ts"`
}

// Notifier delivers significant events to the user (e.g., via Telegram).
type Notifier interface {
	Notify(ctx context.Context, message string) error
}

// ExtendedStatus is the full shadow status including all modules.
type ExtendedStatus struct {
	ServiceStatus
	Analytics       *UsageReport            `json:"analytics,omitempty"`
	CronSuggestions []CronSuggestion        `json:"cronSuggestions,omitempty"`
	RecentReviews   []CodeReviewResult      `json:"recentReviews,omitempty"`
	ExtractedFacts  int                     `json:"extractedFacts"`
	RecurringErrors int                     `json:"recurringErrors"`
	Continuity      *ContinuitySnapshot     `json:"continuity,omitempty"`
	PrefetchedCtx   []PrefetchedContext     `json:"prefetchedContexts,omitempty"`
}

// digestInterval is how often the shadow service sends a periodic summary.
const digestInterval = 4 * time.Hour

// maxPendingTasks caps the in-memory tracked task list.
const maxPendingTasks = 100

// maxHealthAlerts caps the in-memory health alert list.
const maxHealthAlerts = 50

// maxTopicHistory caps the topic change log.
const maxTopicHistory = 20
