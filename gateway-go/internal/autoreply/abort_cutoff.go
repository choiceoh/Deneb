// abort_cutoff.go — Abort cutoff tracking for message deduplication.
// Mirrors src/auto-reply/reply/abort-cutoff.ts (138 LOC).
package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"
)

// AbortCutoffContext holds the cutoff marker for abort deduplication.
type AbortCutoffContext struct {
	MessageSid string `json:"messageSid,omitempty"`
	Timestamp  *int64 `json:"timestamp,omitempty"`
}

// ResolveAbortCutoffFromContext derives abort cutoff info from the message context.
func ResolveAbortCutoffFromContext(msg *types.MsgContext) *AbortCutoffContext {
	messageSid := strings.TrimSpace(msg.MessageSid)
	if messageSid == "" {
		return nil
	}
	return &AbortCutoffContext{MessageSid: messageSid}
}

// SessionAbortCutoffEntry holds abort cutoff fields on a session entry.
type SessionAbortCutoffEntry struct {
	AbortCutoffMessageSid string `json:"abortCutoffMessageSid,omitempty"`
	AbortCutoffTimestamp  *int64 `json:"abortCutoffTimestamp,omitempty"`
}

// ReadAbortCutoffFromSessionEntry extracts the abort cutoff from a session entry.
func ReadAbortCutoffFromSessionEntry(entry *SessionAbortCutoffEntry) *AbortCutoffContext {
	if entry == nil {
		return nil
	}
	sid := strings.TrimSpace(entry.AbortCutoffMessageSid)
	ts := entry.AbortCutoffTimestamp
	if sid == "" && ts == nil {
		return nil
	}
	return &AbortCutoffContext{MessageSid: sid, Timestamp: ts}
}

// HasAbortCutoff returns true if the entry has an active abort cutoff.
func HasAbortCutoff(entry *SessionAbortCutoffEntry) bool {
	return ReadAbortCutoffFromSessionEntry(entry) != nil
}

// ApplyAbortCutoffToSessionEntry writes cutoff fields onto a session entry.
func ApplyAbortCutoffToSessionEntry(entry *SessionAbortCutoffEntry, cutoff *AbortCutoffContext) {
	if cutoff == nil {
		entry.AbortCutoffMessageSid = ""
		entry.AbortCutoffTimestamp = nil
		return
	}
	entry.AbortCutoffMessageSid = cutoff.MessageSid
	entry.AbortCutoffTimestamp = cutoff.Timestamp
}

// ClearAbortCutoffInSession clears the abort cutoff fields on the entry struct.
// This is a pure data mutation; callers must persist the entry themselves
// (see ClearSessionAbortCutoff in commands_session_store.go for the full persist flow).
func ClearAbortCutoffInSession(entry *SessionAbortCutoffEntry) bool {
	if entry == nil || !HasAbortCutoff(entry) {
		return false
	}
	ApplyAbortCutoffToSessionEntry(entry, nil)
	return true
}

// toNumericMessageSid attempts to parse a message SID as a big integer
// for Telegram-style numeric message IDs.
func toNumericMessageSid(value string) *big.Int {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	for _, c := range trimmed {
		if c < '0' || c > '9' {
			return nil
		}
	}
	n := new(big.Int)
	if _, ok := n.SetString(trimmed, 10); !ok {
		return nil
	}
	return n
}

// ShouldSkipMessageByAbortCutoff determines whether a message should be
// skipped because it falls at or before the abort cutoff marker.
// Uses numeric SID comparison (for Telegram) with string fallback,
// then timestamp comparison.
func ShouldSkipMessageByAbortCutoff(cutoffSid string, cutoffTimestamp *int64, messageSid string, messageTimestamp *int64) bool {
	cSid := strings.TrimSpace(cutoffSid)
	mSid := strings.TrimSpace(messageSid)

	// Compare by message SID (numeric comparison when possible).
	if cSid != "" && mSid != "" {
		cutoffNum := toNumericMessageSid(cSid)
		currentNum := toNumericMessageSid(mSid)
		if cutoffNum != nil && currentNum != nil {
			return currentNum.Cmp(cutoffNum) <= 0
		}
		if mSid == cSid {
			return true
		}
	}

	// Fall back to timestamp comparison.
	if cutoffTimestamp != nil && messageTimestamp != nil {
		ct := *cutoffTimestamp
		mt := *messageTimestamp
		if isFiniteInt64(ct) && isFiniteInt64(mt) {
			return mt <= ct
		}
	}

	return false
}

// ShouldPersistAbortCutoff decides whether abort cutoff should be written
// to the session store. Only persist when command and target are the same session.
// Native targeted /stop can run from a different session key (slash/session-control),
// so cutoff must only apply to matching sessions.
func ShouldPersistAbortCutoff(commandSessionKey, targetSessionKey string) bool {
	cmd := strings.TrimSpace(commandSessionKey)
	tgt := strings.TrimSpace(targetSessionKey)
	if cmd == "" || tgt == "" {
		return true
	}
	return cmd == tgt
}

func isFiniteInt64(v int64) bool {
	return v != math.MinInt64 && v != math.MaxInt64
}

// FormatTimestampWithAge formats a millisecond timestamp as ISO 8601 with relative age.
func FormatTimestampWithAge(valueMs int64) string {
	if valueMs <= 0 {
		return "n/a"
	}
	t := time.UnixMilli(valueMs)
	elapsed := time.Since(t)
	return t.UTC().Format(time.RFC3339) + " (" + formatRelativeAge(elapsed) + " ago)"
}

func formatRelativeAge(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
