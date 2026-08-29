// Package coderepo keeps the operator's list of code repositories the agent is
// allowed to work in.
//
// This is an allowlist, and that is the point. A conversation can be pointed at
// a repository, and (later) get its own git worktree there — so the set of
// eligible paths must be something the operator chose deliberately, not
// anything the agent can reach on disk. Registration is the human step;
// everything downstream resolves against what is registered here.
package coderepo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Repo is one registered repository.
type Repo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	// AddedAtMs is when the operator registered it (epoch millis).
	AddedAtMs int64 `json:"addedAtMs"`
}

// Store persists the registry as JSON under the state dir. Safe for concurrent
// use; the whole file is rewritten on every mutation (the list is a handful of
// entries, so simplicity beats incremental writes here).
type Store struct {
	path string
	// protected paths may never be registered — see New.
	protected []string

	mu    sync.Mutex
	repos []Repo
}

// ErrProtectedPath is returned for a path the agent must never get a worktree
// in. Callers surface it as-is; the message names the reason.
var ErrProtectedPath = errors.New("protected path")

// New opens (or starts) the registry at <stateDir>/code-repos.json.
//
// protected lists repository roots that must never be registered — in practice
// the gateway's own production checkout, where CLAUDE.md forbids agent
// branches, worktrees, and manual builds because a deploy timer owns it. This
// is injected rather than detected in here so the policy is visible at the
// wiring site and the store stays testable.
func New(stateDir string, protected []string) *Store {
	s := &Store{path: filepath.Join(stateDir, "code-repos.json")}
	for _, p := range protected {
		if cleaned := cleanPath(p); cleaned != "" {
			s.protected = append(s.protected, cleaned)
		}
	}
	s.load()
	return s
}

func cleanPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	// Resolve symlinks so two names for one directory cannot slip past the
	// protected check or register the same repo twice.
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return filepath.Clean(p)
}

func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return // no registry yet — an empty list is the correct start
	}
	var repos []Repo
	if err := json.Unmarshal(data, &repos); err != nil {
		return // corrupt file: start empty rather than refuse to boot
	}
	s.repos = repos
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.repos, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	// Write-then-rename so a crash mid-write cannot truncate the registry.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// List returns the registered repositories, newest registration last.
func (s *Store) List() []Repo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Repo, len(s.repos))
	copy(out, s.repos)
	sort.SliceStable(out, func(i, j int) bool { return out[i].AddedAtMs < out[j].AddedAtMs })
	return out
}

// Lookup resolves a registered repo by id.
func (s *Store) Lookup(id string) (Repo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.repos {
		if r.ID == id {
			return r, true
		}
	}
	return Repo{}, false
}

// Register validates a path and adds it. Registering an already-registered
// path returns the existing entry rather than an error — the operator asked for
// it to be available, and it is.
func (s *Store) Register(path, name string) (Repo, error) {
	cleaned := cleanPath(path)
	if cleaned == "" {
		return Repo{}, errors.New("경로가 비어 있습니다")
	}
	if !filepath.IsAbs(cleaned) {
		return Repo{}, fmt.Errorf("절대 경로여야 합니다: %s", path)
	}
	info, err := os.Stat(cleaned)
	if err != nil || !info.IsDir() {
		return Repo{}, fmt.Errorf("디렉터리가 아닙니다: %s", cleaned)
	}
	// A worktree's .git is a FILE, not a directory — accept both, or every
	// worktree would be rejected as "not a repository".
	if _, err := os.Stat(filepath.Join(cleaned, ".git")); err != nil {
		return Repo{}, fmt.Errorf("git 저장소가 아닙니다: %s", cleaned)
	}
	for _, p := range s.protected {
		if cleaned == p {
			return Repo{}, fmt.Errorf("%w: %s 는 프로덕션 체크아웃이라 에이전트 작업 대상이 될 수 없습니다", ErrProtectedPath, cleaned)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.repos {
		if r.Path == cleaned {
			return r, nil
		}
	}
	label := strings.TrimSpace(name)
	if label == "" {
		label = filepath.Base(cleaned)
	}
	repo := Repo{
		ID:        newID(cleaned),
		Name:      label,
		Path:      cleaned,
		AddedAtMs: time.Now().UnixMilli(),
	}
	s.repos = append(s.repos, repo)
	if err := s.saveLocked(); err != nil {
		s.repos = s.repos[:len(s.repos)-1] // keep memory and disk agreeing
		return Repo{}, fmt.Errorf("저장하지 못했습니다: %w", err)
	}
	return repo, nil
}

// Unregister drops a repository from the allowlist. It does NOT touch the
// directory — removing a path from the list is a permissions change, not a
// deletion, and conflating the two would make an undo destructive.
func (s *Store) Unregister(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.repos {
		if r.ID != id {
			continue
		}
		removed := r
		s.repos = append(s.repos[:i], s.repos[i+1:]...)
		if err := s.saveLocked(); err != nil {
			// Put it back so a failed write does not silently un-register.
			s.repos = append(s.repos, removed)
			return fmt.Errorf("저장하지 못했습니다: %w", err)
		}
		return nil
	}
	return fmt.Errorf("등록되지 않은 저장소입니다: %s", id)
}

// newID derives a stable, path-derived id so re-registering the same directory
// after an unregister reuses its identity (any session still bound to it keeps
// resolving).
func newID(path string) string {
	base := filepath.Base(path)
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "repo"
	}
	return slug + "-" + shortHash(path)
}

// shortHash keeps ids unique when two registered repos share a basename
// (~/work/api and ~/side/api both slug to "api").
func shortHash(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])[:8]
}
