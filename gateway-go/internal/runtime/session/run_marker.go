package session

// Package-level file: run marker store.
//
// On-disk markers record "this session had an active agent run at moment T."
// The marker is created when a session transitions into StatusRunning and
// deleted on any terminal transition. If a marker survives a gateway
// restart, that run was interrupted (crash, OOM, SIGKILL, host reboot).
//
// Markers live at <baseDir>/<sanitizedKey>.json with content:
//
//	{
//	  "sessionKey":     "telegram:1234567890",
//	  "startedAt":      1700000000000,
//	  "lastActivityAt": 1700000000500,
//	  "channel":        "telegram",
//	  "resumeAttempts": 0
//	}
//
// Atomicity: writes go through a temp file + rename so a crash during
// serialization never leaves a partial/corrupt marker that would block
// subsequent resumes.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RunMarker is the on-disk record of a session run that was in progress.
// Fields are deliberately minimal: everything else is reconstructed from
// the session key + transcript + in-memory manager after restore.
type RunMarker struct {
	SessionKey     string `json:"sessionKey"`
	StartedAt      int64  `json:"startedAt"`         // UnixMilli at PhaseStart.
	LastActivityAt int64  `json:"lastActivityAt"`    // UnixMilli of latest tool/text activity.
	Channel        string `json:"channel,omitempty"` // "telegram", "cron", "btw", …
	ResumeAttempts int    `json:"resumeAttempts"`    // Increments every time autoresume touches this session.
}

// RunMarkerStore persists RunMarker records to a filesystem directory.
// Safe for concurrent use; each marker file is locked only via the
// store-wide mutex (we expect O(10) concurrent sessions max — not a
// contention point).
type RunMarkerStore struct {
	baseDir string
	mu      sync.Mutex
}

// NewRunMarkerStore returns a store rooted at baseDir. The directory is
// created lazily on first write; callers may pass a path that does not
// yet exist.
func NewRunMarkerStore(baseDir string) *RunMarkerStore {
	return &RunMarkerStore{baseDir: baseDir}
}

// BaseDir returns the directory the store was constructed with.
// Exposed so tests and observability can locate the marker directory.
func (s *RunMarkerStore) BaseDir() string { return s.baseDir }

// sanitizeKey maps an arbitrary session key to a filesystem-safe filename.
// Session keys like "telegram:7074071666" are allowed as-is on most filesystems
// (colons are fine on Linux) but we replace path separators and "." runs
// defensively so no caller-provided suffix can escape baseDir.
func sanitizeKey(key string) string {
	// Disallow path traversal components.
	k := strings.ReplaceAll(key, "/", "_")
	k = strings.ReplaceAll(k, "\\", "_")
	for strings.Contains(k, "..") {
		k = strings.ReplaceAll(k, "..", "_")
	}
	return k
}

func (s *RunMarkerStore) pathFor(key string) string {
	return filepath.Join(s.baseDir, sanitizeKey(key)+".json")
}

// Write atomically persists a marker. The previous marker for the same
// session is replaced.
//
// Atomicity is enforced via temp-file-and-rename so that a crash during
// serialization never leaves a partial JSON blob on disk.
func (s *RunMarkerStore) Write(m RunMarker) error {
	if m.SessionKey == "" {
		return fmt.Errorf("run marker: empty session key")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return fmt.Errorf("run marker: mkdir %q: %w", s.baseDir, err)
	}

	// SessionKey is a routing id ("telegram:123456"), not a credential; gosec
	// G117 flags the "Key" suffix defensively. Confirmed non-secret.
	data, err := json.Marshal(m) //nolint:gosec // SessionKey is a routing id, not a credential
	if err != nil {
		return fmt.Errorf("run marker: marshal: %w", err)
	}

	final := s.pathFor(m.SessionKey)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil { //nolint:gosec // user-owned state file
		return fmt.Errorf("run marker: write temp: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("run marker: rename: %w", err)
	}
	return nil
}

// Delete removes the marker for sessionKey. It is NOT an error if the
// marker does not exist — this is the normal case for sessions that never
// entered StatusRunning (e.g. synthetic btw: sessions that no-op).
func (s *RunMarkerStore) Delete(sessionKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.pathFor(sessionKey)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("run marker: remove: %w", err)
	}
	return nil
}

// Touch updates LastActivityAt to now for an existing marker. No-op if
// the marker does not exist. Used by the run loop so stale markers (the
// run legitimately took longer than the "max age" threshold) can still be
// distinguished from truly abandoned runs.
func (s *RunMarkerStore) Touch(sessionKey string) error {
	m, err := s.Read(sessionKey)
	if err != nil || m == nil {
		return err
	}
	m.LastActivityAt = time.Now().UnixMilli()
	return s.Write(*m)
}

// Read returns the marker for sessionKey, or (nil, nil) if not present.
func (s *RunMarkerStore) Read(sessionKey string) (*RunMarker, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked(sessionKey)
}

func (s *RunMarkerStore) readLocked(sessionKey string) (*RunMarker, error) {
	path := s.pathFor(sessionKey)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("run marker: read: %w", err)
	}
	var m RunMarker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("run marker: unmarshal: %w", err)
	}
	return &m, nil
}

// List returns every marker currently present in the store. Corrupt JSON
// files are skipped with the returned error collecting their names, so
// one bad file does not block resume of the rest.
func (s *RunMarkerStore) List() ([]RunMarker, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("run marker: readdir: %w", err)
	}
	var markers []RunMarker
	var badFiles []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		// Skip partial temp files from a crash mid-Write.
		if strings.HasSuffix(entry.Name(), ".tmp.json") || strings.HasSuffix(entry.Name(), ".json.tmp") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(s.baseDir, entry.Name()))
		if readErr != nil {
			badFiles = append(badFiles, entry.Name())
			continue
		}
		var m RunMarker
		if uErr := json.Unmarshal(data, &m); uErr != nil {
			badFiles = append(badFiles, entry.Name())
			continue
		}
		if m.SessionKey == "" {
			// Legacy or hand-edited marker without the session key — derive from filename.
			m.SessionKey = strings.TrimSuffix(entry.Name(), ".json")
		}
		markers = append(markers, m)
	}
	if len(badFiles) > 0 {
		return markers, fmt.Errorf("run marker: %d corrupt files skipped: %s",
			len(badFiles), strings.Join(badFiles, ", "))
	}
	return markers, nil
}

// IncrementResumeAttempts loads-modifies-writes atomically under the store lock.
// Returns the new attempt count after increment.
func (s *RunMarkerStore) IncrementResumeAttempts(sessionKey string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readLocked(sessionKey)
	if err != nil || m == nil {
		return 0, err
	}
	m.ResumeAttempts++
	if err := s.writeLocked(*m); err != nil {
		return m.ResumeAttempts, err
	}
	return m.ResumeAttempts, nil
}

// writeLocked is Write but requires the caller already hold s.mu.
func (s *RunMarkerStore) writeLocked(m RunMarker) error {
	if m.SessionKey == "" {
		return fmt.Errorf("run marker: empty session key")
	}
	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return fmt.Errorf("run marker: mkdir %q: %w", s.baseDir, err)
	}
	data, err := json.Marshal(m) //nolint:gosec // SessionKey is a routing id, not a credential
	if err != nil {
		return fmt.Errorf("run marker: marshal: %w", err)
	}
	final := s.pathFor(m.SessionKey)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil { //nolint:gosec // user-owned state file
		return fmt.Errorf("run marker: write temp: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("run marker: rename: %w", err)
	}
	return nil
}
