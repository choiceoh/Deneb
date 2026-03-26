// commands_session_store.go — Session entry persistence helpers.
// Mirrors src/auto-reply/reply/commands-session-store.ts (53 LOC).
package autoreply

import (
	"time"
)

// SessionEntry represents a session's persistent state.
type SessionEntry struct {
	SessionKey             string `json:"sessionKey"`
	SessionID              string `json:"sessionId,omitempty"`
	SessionFile            string `json:"sessionFile,omitempty"`
	CreatedAt              int64  `json:"createdAt"`
	UpdatedAt              int64  `json:"updatedAt"`
	AbortedLastRun         bool   `json:"abortedLastRun,omitempty"`
	AbortCutoffMessageSid  string `json:"abortCutoffMessageSid,omitempty"`
	AbortCutoffTimestamp   *int64 `json:"abortCutoffTimestamp,omitempty"`
}

// SessionStore provides read/write access to session entries.
type SessionStore interface {
	Get(key string) *SessionEntry
	Set(key string, entry *SessionEntry)
	Persist(key string, entry *SessionEntry) error
}

// PersistSessionEntry updates the session entry timestamps and persists it.
func PersistSessionEntry(store SessionStore, key string, entry *SessionEntry) bool {
	if store == nil || key == "" || entry == nil {
		return false
	}
	entry.UpdatedAt = time.Now().UnixMilli()
	store.Set(key, entry)
	_ = store.Persist(key, entry)
	return true
}

// PersistAbortTargetEntry marks a session entry as aborted, applies the
// abort cutoff from the message context, and persists it to the store.
func PersistAbortTargetEntry(store SessionStore, key string, entry *SessionEntry, cutoff *AbortCutoffContext) bool {
	if store == nil || key == "" || entry == nil {
		return false
	}
	entry.AbortedLastRun = true
	if cutoff != nil {
		entry.AbortCutoffMessageSid = cutoff.MessageSid
		entry.AbortCutoffTimestamp = cutoff.Timestamp
	}
	entry.UpdatedAt = time.Now().UnixMilli()
	store.Set(key, entry)
	_ = store.Persist(key, entry)
	return true
}

// ClearSessionAbortCutoff clears the abort cutoff fields from a session entry
// and persists it. Returns false if there was no cutoff to clear.
func ClearSessionAbortCutoff(store SessionStore, key string, entry *SessionEntry) bool {
	if store == nil || key == "" || entry == nil {
		return false
	}
	if entry.AbortCutoffMessageSid == "" && entry.AbortCutoffTimestamp == nil {
		return false
	}
	entry.AbortCutoffMessageSid = ""
	entry.AbortCutoffTimestamp = nil
	entry.UpdatedAt = time.Now().UnixMilli()
	store.Set(key, entry)
	_ = store.Persist(key, entry)
	return true
}

// SessionEntryHasAbortCutoff returns true if the entry has an active abort cutoff.
func SessionEntryHasAbortCutoff(entry *SessionEntry) bool {
	if entry == nil {
		return false
	}
	return entry.AbortCutoffMessageSid != "" || entry.AbortCutoffTimestamp != nil
}

// ShouldSkipBySessionAbortCutoff checks if a message should be skipped
// based on the session's abort cutoff.
func ShouldSkipBySessionAbortCutoff(entry *SessionEntry, messageSid string, messageTimestamp *int64) bool {
	if entry == nil || !SessionEntryHasAbortCutoff(entry) {
		return false
	}
	return ShouldSkipMessageByAbortCutoff(
		entry.AbortCutoffMessageSid,
		entry.AbortCutoffTimestamp,
		messageSid,
		messageTimestamp,
	)
}
