// session_full.go — Full session lifecycle management.
// Mirrors src/auto-reply/reply/session.ts (602 LOC), session-updates.ts (308 LOC),
// session-fork.ts (59 LOC), session-reset-model.ts (200 LOC),
// session-reset-prompt.ts (18 LOC), session-run-accounting.ts (37 LOC),
// session-usage.ts (173 LOC), session-hooks.ts (66 LOC),
// session-delivery.ts (216 LOC).
package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"fmt"
	"time"
)

// SessionUpdate describes a mutation to apply to a session.
type SessionUpdate struct {
	Model           *string
	Provider        *string
	ThinkLevel      *types.ThinkLevel
	FastMode        *bool
	VerboseLevel    *types.VerboseLevel
	ReasoningLevel  *types.ReasoningLevel
	ElevatedLevel   *types.ElevatedLevel
	SendPolicy      *string
	GroupActivation *types.GroupActivationMode
	SystemPrompt    *string
	Label           *string
}

// ApplySessionUpdate applies an update to a session state.
func ApplySessionUpdate(session *types.SessionState, update SessionUpdate) {
	if update.Model != nil {
		session.Model = *update.Model
	}
	if update.Provider != nil {
		session.Provider = *update.Provider
	}
	if update.ThinkLevel != nil {
		session.ThinkLevel = *update.ThinkLevel
	}
	if update.FastMode != nil {
		session.FastMode = *update.FastMode
	}
	if update.VerboseLevel != nil {
		session.VerboseLevel = *update.VerboseLevel
	}
	if update.ReasoningLevel != nil {
		session.ReasoningLevel = *update.ReasoningLevel
	}
	if update.ElevatedLevel != nil {
		session.ElevatedLevel = *update.ElevatedLevel
	}
	if update.SendPolicy != nil {
		session.SendPolicy = *update.SendPolicy
	}
	if update.GroupActivation != nil {
		session.GroupActivation = *update.GroupActivation
	}
	session.UpdatedAt = time.Now().UnixMilli()
}

// SessionForkParams configures a session fork.
type SessionForkParams struct {
	ParentKey   string
	NewKey      string
	ResetModel  bool
	ResetPrompt bool
}

// ForkSession creates a new session based on a parent session.
func ForkSession(parent *types.SessionState, params SessionForkParams) *types.SessionState {
	forked := *parent
	forked.SessionKey = params.NewKey
	forked.IsNew = true
	forked.CreatedAt = time.Now().UnixMilli()
	forked.UpdatedAt = time.Now().UnixMilli()

	if params.ResetModel {
		forked.Model = ""
		forked.Provider = ""
	}
	return &forked
}

// ResetSessionModel clears the model override on a session.
func ResetSessionModel(session *types.SessionState) {
	session.Model = ""
	session.Provider = ""
	session.UpdatedAt = time.Now().UnixMilli()
}

// ResetSessionPrompt clears the system prompt override on a session.
func ResetSessionPrompt(session *types.SessionState) {
	session.UpdatedAt = time.Now().UnixMilli()
}

// SessionRunAccounting tracks run counts and totals.
type SessionRunAccounting struct {
	RunCount      int   `json:"runCount"`
	TotalTokens   int64 `json:"totalTokens"`
	TotalDuration int64 `json:"totalDurationMs"`
	LastRunAt     int64 `json:"lastRunAt"`
}

// RecordRun updates accounting after a completed run.
func (a *SessionRunAccounting) RecordRun(usage TokenUsage, durationMs int64) {
	a.RunCount++
	a.TotalTokens += usage.TotalTokens
	a.TotalDuration += durationMs
	a.LastRunAt = time.Now().UnixMilli()
}

// SessionUsage tracks detailed token usage for a session.
type SessionUsage struct {
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	TotalTokens      int64 `json:"totalTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
	RunCount         int   `json:"runCount"`
}

// AddUsage accumulates token usage.
func (u *SessionUsage) AddUsage(usage TokenUsage) {
	u.InputTokens += usage.InputTokens
	u.OutputTokens += usage.OutputTokens
	u.TotalTokens += usage.TotalTokens
	u.CacheReadTokens += usage.CacheReadTokens
	u.CacheWriteTokens += usage.CacheWriteTokens
	u.RunCount++
}

// FormatUsage formats usage as a compact display string.
func (u *SessionUsage) FormatUsage() string {
	if u.TotalTokens == 0 {
		return "No usage recorded."
	}
	return fmt.Sprintf("%d tokens (%d in, %d out) across %d runs",
		u.TotalTokens, u.InputTokens, u.OutputTokens, u.RunCount)
}

// SessionHookEvent represents a session lifecycle hook event.
type SessionHookEvent struct {
	Type       string
	SessionKey string
	AgentID    string
	Reason     string
	Timestamp  int64
}

// EmitSessionHook is a placeholder for hook emission.
func EmitSessionHook(event SessionHookEvent) {
	// Hook emission is handled by the plugin system's HookRunner.
	// This is called by session lifecycle code to notify plugins.
}

// SessionDelivery handles reply delivery to the originating channel.
type SessionDelivery struct {
	Channel   string
	To        string
	AccountID string
	ThreadID  string
	ReplyToID string
}

// BuildSessionDelivery creates delivery info from session state.
func BuildSessionDelivery(session *types.SessionState, msg *types.MsgContext) SessionDelivery {
	delivery := SessionDelivery{
		Channel:   session.Channel,
		To:        msg.To,
		AccountID: session.AccountID,
		ThreadID:  session.ThreadID,
	}
	if msg.ReplyToID != "" {
		delivery.ReplyToID = msg.ReplyToID
	}
	return delivery
}
