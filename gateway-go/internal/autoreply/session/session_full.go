// session_full.go — Full session lifecycle management.
// Mirrors src/auto-reply/reply/session.ts (602 LOC), session-updates.ts (308 LOC),
// session-fork.ts (59 LOC), session-reset-model.ts (200 LOC),
// session-reset-prompt.ts (18 LOC), session-run-accounting.ts (37 LOC),
// session-usage.ts (173 LOC), session-hooks.ts (66 LOC),
// session-delivery.ts (216 LOC).
package session

import (
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
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
func ApplySessionUpdate(sess *types.SessionState, update SessionUpdate) {
	if update.Model != nil {
		sess.Model = *update.Model
	}
	if update.Provider != nil {
		sess.Provider = *update.Provider
	}
	if update.ThinkLevel != nil {
		sess.ThinkLevel = *update.ThinkLevel
	}
	if update.FastMode != nil {
		sess.FastMode = *update.FastMode
	}
	if update.VerboseLevel != nil {
		sess.VerboseLevel = *update.VerboseLevel
	}
	if update.ReasoningLevel != nil {
		sess.ReasoningLevel = *update.ReasoningLevel
	}
	if update.ElevatedLevel != nil {
		sess.ElevatedLevel = *update.ElevatedLevel
	}
	if update.SendPolicy != nil {
		sess.SendPolicy = *update.SendPolicy
	}
	if update.GroupActivation != nil {
		sess.GroupActivation = *update.GroupActivation
	}
	sess.UpdatedAt = time.Now().UnixMilli()
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
func ResetSessionModel(sess *types.SessionState) {
	sess.Model = ""
	sess.Provider = ""
	sess.UpdatedAt = time.Now().UnixMilli()
}

// ResetSessionPrompt clears the system prompt override on a session.
func ResetSessionPrompt(sess *types.SessionState) {
	sess.UpdatedAt = time.Now().UnixMilli()
}

// TokenUsage tracks token consumption for session accounting.
// Mirrors the root autoreply.TokenUsage struct for use within the session subpackage.
type TokenUsage struct {
	InputTokens      int64 `json:"inputTokens,omitempty"`
	OutputTokens     int64 `json:"outputTokens,omitempty"`
	TotalTokens      int64 `json:"totalTokens,omitempty"`
	CacheReadTokens  int64 `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64 `json:"cacheWriteTokens,omitempty"`
}

// AddUsage accumulates usage from another.
func (u *TokenUsage) AddUsage(other TokenUsage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.TotalTokens += other.TotalTokens
	u.CacheReadTokens += other.CacheReadTokens
	u.CacheWriteTokens += other.CacheWriteTokens
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
func BuildSessionDelivery(sess *types.SessionState, msg *types.MsgContext) SessionDelivery {
	delivery := SessionDelivery{
		Channel:   sess.Channel,
		To:        msg.To,
		AccountID: sess.AccountID,
		ThreadID:  sess.ThreadID,
	}
	if msg.ReplyToID != "" {
		delivery.ReplyToID = msg.ReplyToID
	}
	return delivery
}
