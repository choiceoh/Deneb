// Package monitoring implements gateway activity tracking.
//
// The channel health monitor that once lived here was retired once channel
// plugins were removed (PR #1922): the native client is the sole surface, has
// no restartable channel, and silence is normal user idle rather than a fault.
// What remains is the activity tracker the heartbeat relies on.
package monitoring

import (
	"sync/atomic"
	"time"
)

// --- Activity Tracker ---

// ActivityTracker records the timestamp and session key of the last gateway
// activity. Heartbeat uses LastSessionKey() to deliver autonomous turns into
// the most recently active user session, so the agent shares context with the
// user instead of running in an isolated stateless channel.
type ActivityTracker struct {
	lastActivityMs atomic.Int64
	lastSessionKey atomic.Value
}

// NewActivityTracker creates a new activity tracker.
func NewActivityTracker() *ActivityTracker {
	t := &ActivityTracker{}
	t.lastSessionKey.Store("")
	t.Touch()
	return t
}

// Touch updates the last activity timestamp to now.
func (t *ActivityTracker) Touch() {
	t.lastActivityMs.Store(time.Now().UnixMilli())
}

// TouchSession updates both the timestamp and the last active session key.
// Pass the session key associated with the activity (e.g. "client:main").
func (t *ActivityTracker) TouchSession(sessionKey string) {
	t.lastActivityMs.Store(time.Now().UnixMilli())
	if sessionKey != "" {
		t.lastSessionKey.Store(sessionKey)
	}
}

// LastActivityAt returns the last activity timestamp in unix milliseconds.
func (t *ActivityTracker) LastActivityAt() int64 {
	return t.lastActivityMs.Load()
}

// LastSessionKey returns the session key of the most recent activity, or "" if
// no session-tagged activity has been recorded.
func (t *ActivityTracker) LastSessionKey() string {
	v := t.lastSessionKey.Load()
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}
