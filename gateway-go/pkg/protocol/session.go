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
	SessionKindDirect  SessionKind = "direct"
	SessionKindGroup   SessionKind = "group"
	SessionKindGlobal  SessionKind = "global"
	SessionKindUnknown SessionKind = "unknown"
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
