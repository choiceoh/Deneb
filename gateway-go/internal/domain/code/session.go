package code

// session.go — the session store, the source of truth behind code.sessions.
//
// A coding session is one unit of work (= one worktree = one branch) plus the
// metadata a vibe coder sees in the rail: a human title, a working/passed/failed
// status, and the checkpoints they can step back through. `git worktree list`
// has none of that, so it lives here in a single JSON file.
//
// The store is single-process (the gateway) so an in-process mutex + temp-file
// rename gives reader-atomic writes without the cross-process flock dependency —
// keeping the package dependency-light and unit-testable on any OS.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Session lifecycle status.
const (
	StatusWorking = "working" // agent is editing / verifying
	StatusPassed  = "passed"  // last verify build/test passed
	StatusFailed  = "failed"  // last verify failed
	StatusMissing = "missing" // worktree directory is gone (reconcile)
)

// Checkpoint is one accepted change ("좋아요") — a commit on the task branch with
// a Korean summary the user can read instead of a diff.
type Checkpoint struct {
	SHA     string `json:"sha"`
	Summary string `json:"summary"` // 한국어 요약
	At      string `json:"at"`      // RFC3339
}

// Session is the rail row + its history.
type Session struct {
	ID             string       `json:"id"` // = task/worktree slug
	Repo           Repo         `json:"repo"`
	Title          string       `json:"title"`  // autotitle (tiny role)
	Status         string       `json:"status"` // working | passed | failed | missing
	Branch         string       `json:"branch"`
	Dir            string       `json:"dir"`
	ChatSessionKey string       `json:"chatSessionKey,omitempty"` // links to the chat transcript
	Checkpoints    []Checkpoint `json:"checkpoints,omitempty"`
	CreatedAt      string       `json:"createdAt"`
	UpdatedAt      string       `json:"updatedAt"`
}

// NewSession builds a working session from a started Task.
func NewSession(t Task, title, chatSessionKey string) *Session {
	return &Session{
		ID:             t.ID,
		Repo:           t.Repo,
		Title:          title,
		Status:         StatusWorking,
		Branch:         t.Branch,
		Dir:            t.Dir,
		ChatSessionKey: chatSessionKey,
	}
}

// Store persists sessions to <root>/sessions.json.
type Store struct {
	path     string
	now      func() time.Time
	mu       sync.RWMutex
	sessions map[string]*Session // by ID
}

// NewStore loads the store at root (empty if the file does not exist yet).
func NewStore(root string) (*Store, error) {
	s := &Store{
		path:     filepath.Join(root, "sessions.json"),
		now:      time.Now,
		sessions: map[string]*Session{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) nowStr() string { return s.now().UTC().Format(time.RFC3339) }

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read sessions: %w", err)
	}
	var list []*Session
	if err := json.Unmarshal(data, &list); err != nil {
		// A corrupt sessions.json must not disable coding mode — the worktrees on
		// disk are the real work and Reconcile rebuilds status. Preserve the bad
		// file for forensics and start empty.
		slog.Warn("code: sessions.json unreadable, starting empty", "path", s.path, "error", err)
		_ = os.Rename(s.path, s.path+".corrupt")
		return nil
	}
	for _, sess := range list {
		s.sessions[sess.ID] = sess
	}
	return nil
}

// saveLocked writes the whole store atomically. Caller must hold s.mu (write).
func (s *Store) saveLocked() error {
	list := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		list = append(list, sess)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("encode sessions: %w", err)
	}
	return atomicWrite(s.path, data)
}

// Add stores a new session and stamps its timestamps.
func (s *Store) Add(sess *Session) error {
	if sess.ID == "" {
		return fmt.Errorf("session id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[sess.ID]; ok {
		return fmt.Errorf("session %q already exists", sess.ID)
	}
	now := s.nowStr()
	sess.CreatedAt = now
	sess.UpdatedAt = now
	if sess.Status == "" {
		sess.Status = StatusWorking
	}
	s.sessions[sess.ID] = sess
	return s.saveLocked()
}

// Get returns a copy of the session (so callers cannot mutate store state).
func (s *Store) Get(id string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	if !ok {
		return Session{}, false
	}
	return *sess, true
}

// List returns all sessions, most-recently-updated first (rail order).
func (s *Store) List() []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, *sess)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}

// SetStatus updates the verify status.
func (s *Store) SetStatus(id, status string) error {
	return s.mutate(id, func(sess *Session) { sess.Status = status })
}

// SetTitle sets the human title (autotitle arrives asynchronously).
func (s *Store) SetTitle(id, title string) error {
	return s.mutate(id, func(sess *Session) { sess.Title = title })
}

// AddCheckpoint appends one accepted change.
func (s *Store) AddCheckpoint(id string, cp Checkpoint) error {
	return s.mutate(id, func(sess *Session) { sess.Checkpoints = append(sess.Checkpoints, cp) })
}

// PopCheckpoint drops the most recent checkpoint — the bookkeeping side of undo.
// No-op (not an error) when there are none, so undo of uncommitted-only edits is safe.
func (s *Store) PopCheckpoint(id string) error {
	return s.mutate(id, func(sess *Session) {
		if n := len(sess.Checkpoints); n > 0 {
			sess.Checkpoints = sess.Checkpoints[:n-1]
		}
	})
}

// Delete removes a session record (call after Discard removes the worktree).
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[id]; !ok {
		return fmt.Errorf("session %q not found", id)
	}
	delete(s.sessions, id)
	return s.saveLocked()
}

// Reconcile marks sessions whose worktree directory is gone as missing, so the
// rail reflects reality after a manual cleanup or crash. exists defaults to a
// filesystem check; it is injectable for tests.
func (s *Store) Reconcile(exists func(dir string) bool) error {
	if exists == nil {
		exists = func(dir string) bool { _, err := os.Stat(dir); return err == nil }
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for _, sess := range s.sessions {
		if sess.Dir != "" && sess.Status != StatusMissing && !exists(sess.Dir) {
			sess.Status = StatusMissing
			sess.UpdatedAt = s.nowStr()
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.saveLocked()
}

// mutate applies a pure field change under the write lock and persists. The
// callback must only set fields on the passed Session — never call Store methods
// (that would re-enter the held lock and deadlock).
func (s *Store) mutate(id string, apply func(*Session)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return fmt.Errorf("session %q not found", id)
	}
	apply(sess)
	sess.UpdatedAt = s.nowStr()
	return s.saveLocked()
}

// atomicWrite writes data via a temp file + rename so readers never see a
// partial file. Single-process; the store's mutex serializes writers.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create store dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp store: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename store: %w", err)
	}
	return nil
}
