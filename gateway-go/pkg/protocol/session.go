// Package protocol — session wire types.
//
// These types mirror the Protobuf definitions in proto/session.proto
// and the TypeScript types in src/protocol/generated/session.ts.
// The internal/session package uses its own Go-idiomatic types;
// these bridge the gap for RPC wire-format serialization.
package protocol

// SessionRunStatus mirrors proto/session.proto SessionRunStatus.
type SessionRunStatus string

const (
	SessionStatusRunning SessionRunStatus = "running"
	SessionStatusDone    SessionRunStatus = "done"
	SessionStatusFailed  SessionRunStatus = "failed"
	SessionStatusKilled  SessionRunStatus = "killed"
	SessionStatusTimeout SessionRunStatus = "timeout"
)

// SessionKind mirrors proto/session.proto SessionKind.
type SessionKind string

const (
	SessionKindDirect   SessionKind = "direct"
	SessionKindGroup    SessionKind = "group"
	SessionKindGlobal   SessionKind = "global"
	SessionKindUnknown  SessionKind = "unknown"
	SessionKindCron     SessionKind = "cron"
	SessionKindSubagent SessionKind = "subagent"
)

// ParseSessionKind converts a string to SessionKind, defaulting to direct.
func ParseSessionKind(s string) SessionKind {
	switch s {
	case "group":
		return SessionKindGroup
	case "global":
		return SessionKindGlobal
	case "unknown":
		return SessionKindUnknown
	case "cron":
		return SessionKindCron
	case "subagent":
		return SessionKindSubagent
	default:
		return SessionKindDirect
	}
}

// SessionLifecyclePhase mirrors proto/session.proto SessionLifecyclePhase.
type SessionLifecyclePhase string

const (
	SessionPhaseStart SessionLifecyclePhase = "start"
	SessionPhaseEnd   SessionLifecyclePhase = "end"
	SessionPhaseError SessionLifecyclePhase = "error"
)

// SessionTransition represents a state transition event.
// Mirrors proto/session.proto SessionTransition.
type SessionTransition struct {
	Key         string           `json:"key"`
	FromStatus  SessionRunStatus `json:"fromStatus"`
	ToStatus    SessionRunStatus `json:"toStatus"`
	TimestampMs int64            `json:"timestampMs"`
	Reason      string           `json:"reason,omitempty"`
	StopReason  string           `json:"stopReason,omitempty"`
	Aborted     *bool            `json:"aborted,omitempty"`
}

// SessionLifecycleEvent represents a session lifecycle event.
// Mirrors proto/session.proto SessionLifecycleEvent.
type SessionLifecycleEvent struct {
	Key         string                `json:"key"`
	Phase       SessionLifecyclePhase `json:"phase"`
	TimestampMs int64                 `json:"timestampMs"`
	StopReason  string                `json:"stopReason,omitempty"`
	Aborted     *bool                 `json:"aborted,omitempty"`
}

// SessionPreviewItem is a single item in a session preview.
// Mirrors proto/session.proto SessionPreviewItem.
type SessionPreviewItem struct {
	Role string `json:"role"` // "user" | "assistant" | "tool" | "system" | "other"
	Text string `json:"text"`
}

// SessionsPreviewEntry is a preview of a session.
// Mirrors proto/session.proto SessionsPreviewEntry.
type SessionsPreviewEntry struct {
	Key    string               `json:"key"`
	Status string               `json:"status"` // "ok" | "empty" | "missing" | "error"
	Items  []SessionPreviewItem `json:"items,omitempty"`
}
