package types

import (
	"strings"
	"time"
)

// SessionState holds the resolved state for a reply session.
type SessionState struct {
	SessionKey      string
	AgentID         string
	IsNew           bool
	IsReset         bool
	IsGroup         bool
	IsThread        bool
	Channel         string
	AccountID       string
	ThreadID        string
	Model           string
	Provider        string
	ThinkLevel      ThinkLevel
	FastMode        bool
	VerboseLevel    VerboseLevel
	ReasoningLevel  ReasoningLevel
	ElevatedLevel   ElevatedLevel
	GroupActivation GroupActivationMode
	SendPolicy      string
	ToolPreset      string // tool preset restricting available tools (researcher, implementer, verifier, coordinator)
	CreatedAt       int64
	UpdatedAt       int64
}

// SessionResetTrigger identifies what caused a session reset.
type SessionResetTrigger string

const (
	ResetNone      SessionResetTrigger = ""
	ResetCommand   SessionResetTrigger = "command"   // /new or /reset
	ResetExpired   SessionResetTrigger = "expired"   // session exceeded max age
	ResetFreshness SessionResetTrigger = "freshness" // session stale
	ResetForced    SessionResetTrigger = "forced"    // programmatic reset
)

// SessionResetPolicy describes when sessions should be reset.
type SessionResetPolicy struct {
	MaxAgeMs   int64 // 0 = no age limit
	MaxIdleMs  int64 // 0 = no idle limit
	OnNewAgent bool  // reset when switching agents
}

// DefaultSessionResetPolicy returns the default reset policy.
func DefaultSessionResetPolicy() SessionResetPolicy {
	return SessionResetPolicy{
		MaxAgeMs:   0,
		MaxIdleMs:  0,
		OnNewAgent: false,
	}
}

// IsSessionExpired checks if a session has exceeded its max age.
func IsSessionExpired(createdAt int64, policy SessionResetPolicy) bool {
	if policy.MaxAgeMs <= 0 || createdAt <= 0 {
		return false
	}
	return time.Now().UnixMilli()-createdAt > policy.MaxAgeMs
}

// IsSessionIdle checks if a session has been idle too long.
func IsSessionIdle(updatedAt int64, policy SessionResetPolicy) bool {
	if policy.MaxIdleMs <= 0 || updatedAt <= 0 {
		return false
	}
	return time.Now().UnixMilli()-updatedAt > policy.MaxIdleMs
}

// SessionHintFlags contains session state hints for agent context.
type SessionHintFlags struct {
	WasAborted        bool
	PreviousRunFailed bool
	IsResumed         bool
	IsForked          bool
}

// BuildSessionHint produces a brief text hint about session state for the agent.
func BuildSessionHint(flags SessionHintFlags) string {
	var hints []string
	if flags.WasAborted {
		hints = append(hints, "Previous run was aborted by user.")
	}
	if flags.PreviousRunFailed {
		hints = append(hints, "Previous run failed.")
	}
	if flags.IsResumed {
		hints = append(hints, "Session resumed.")
	}
	if flags.IsForked {
		hints = append(hints, "Session forked from parent.")
	}
	if len(hints) == 0 {
		return ""
	}
	return strings.Join(hints, " ")
}

// SessionModification describes changes to apply to the session.
type SessionModification struct {
	Reset           bool
	Model           string
	Provider        string
	ThinkLevel      ThinkLevel
	VerboseLevel    VerboseLevel
	FastMode        *bool
	ReasoningLevel  ReasoningLevel
	ElevatedLevel   ElevatedLevel
	SendPolicy      string
	GroupActivation GroupActivationMode
	SystemPrompt    *string
	Label           *string
	// Session lifecycle.
	MaxAgeMs int64
}
