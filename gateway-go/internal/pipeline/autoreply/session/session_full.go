// session_full.go — Full session lifecycle management.
package session

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
)

// SessionUpdate describes a mutation to apply to a session.
type SessionUpdate struct {
	Model          *string
	Provider       *string
	FastMode       *bool
	VerboseLevel   *types.VerboseLevel
	ReasoningLevel *types.ReasoningLevel
	ElevatedLevel  *types.ElevatedLevel
	SystemPrompt   *string
	Label          *string
}

// ApplySessionUpdate applies an update to a session state.
func ApplySessionUpdate(sess *types.SessionState, update SessionUpdate) {
	if update.Model != nil {
		sess.Model = *update.Model
	}
	if update.Provider != nil {
		sess.Provider = *update.Provider
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
	sess.UpdatedAt = time.Now().UnixMilli()
}

// SessionForkParams configures a session fork.
type SessionForkParams struct {
	ParentKey   string
	NewKey      string
	ResetModel  bool
	ResetPrompt bool
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

// SessionRunAccounting tracks run counts and totals.
type SessionRunAccounting struct {
	RunCount      int   `json:"runCount"`
	TotalTokens   int64 `json:"totalTokens"`
	TotalDuration int64 `json:"totalDurationMs"`
	LastRunAt     int64 `json:"lastRunAt"`
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
