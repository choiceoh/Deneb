package checkpoint

import (
	"fmt"
	"os"
	"path/filepath"
)

// RemoveSession deletes the on-disk state for this Manager's session.
// Idempotent: calling on a session that does not exist or was already
// removed is a no-op and returns nil.
//
// Safe to call while other Manager instances (different sessionIDs) are
// active — each session's blob dir is independent.
//
// NOT safe to call while a concurrent Snapshot is in progress on the same
// session. Callers must coordinate with the session's own lifecycle (only
// invoke after the session's agent run has finished and no further Snapshot
// calls will be made). Internally the call grabs m.mu for defense, so if a
// Snapshot happens to be running on this same Manager instance we will wait
// for it — but the result is a removed store the Snapshot just wrote to, so
// don't rely on that.
func (m *Manager) RemoveSession() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return removeSessionDir(m.SessionDir())
}

// RemoveSessionByID deletes the on-disk state for a session under `root`
// without requiring a live Manager instance. Useful for lifecycle hooks
// in code paths that do not maintain Manager instances after session end.
//
// Session IDs are sanitized identically to Manager construction, so the
// same ID that was passed to New() will find the correct directory.
//
// Idempotent. Safe to call concurrently with CleanupStaleSessions — each
// targets its own directory subtree.
func RemoveSessionByID(root, sessionID string) error {
	if root == "" {
		return nil
	}
	dir := filepath.Join(root, sanitizeSession(sessionID))
	return removeSessionDir(dir)
}

// removeSessionDir deletes the given session directory and all contents.
// Returns nil if the directory does not exist. Any other error is wrapped
// with context for the caller to log.
func removeSessionDir(dir string) error {
	if dir == "" {
		return nil
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("checkpoint: stat session dir: %w", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("checkpoint: remove session dir %s: %w", dir, err)
	}
	return nil
}
