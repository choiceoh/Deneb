// commands_session_store.go — Session entry persistence helpers.
// Mirrors src/auto-reply/reply/commands-session-store.ts (53 LOC).
package autoreply

import (
	"time"
)

// SessionEntry represents a session's persistent state.
type SessionEntry struct {
	SessionKey    string `json:"sessionKey"`
	CreatedAt     int64  `json:"createdAt"`
	UpdatedAt     int64  `json:"updatedAt"`
	AbortedLastRun bool  `json:"abortedLastRun,omitempty"`
	// AbortCutoff fields applied by applyAbortCutoff.
	AbortCutoffAt *int64 `json:"abortCutoffAt,omitempty"`
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

// AbortCutoff specifies how to truncate a session on abort.
type AbortCutoff struct {
	At int64 `json:"at"`
}

// PersistAbortTargetEntry marks a session entry as aborted, applies the
// cutoff, and persists it to the store.
func PersistAbortTargetEntry(store SessionStore, key string, entry *SessionEntry, cutoff *AbortCutoff) bool {
	if store == nil || key == "" || entry == nil {
		return false
	}
	entry.AbortedLastRun = true
	if cutoff != nil {
		entry.AbortCutoffAt = &cutoff.At
	}
	entry.UpdatedAt = time.Now().UnixMilli()
	store.Set(key, entry)
	_ = store.Persist(key, entry)
	return true
}
