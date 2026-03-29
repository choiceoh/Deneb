// Package discord — per-thread git worktree management.
//
// Each Discord thread session gets its own git worktree so that concurrent
// coding tasks don't conflict with each other. Worktrees share the parent
// repo's object store, keeping disk usage low while providing full filesystem
// isolation.
package discord

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// WorktreeManager creates and removes per-thread git worktrees.
type WorktreeManager struct {
	mu        sync.Mutex
	worktrees map[string]*ThreadWorkspace // threadID → workspace
	baseDir   string                      // parent directory for worktrees (e.g. ~/.deneb/worktrees)
	logger    *slog.Logger
}

// ThreadWorkspace holds the workspace info for one thread session.
type ThreadWorkspace struct {
	ThreadID  string // Discord thread channel ID
	ParentDir string // original (parent channel) workspace directory
	Dir       string // worktree checkout path
	Branch    string // git branch name
	CreatedAt time.Time
}

// NewWorktreeManager creates a manager that stores worktrees under baseDir.
// If baseDir is empty, defaults to ~/.deneb/worktrees.
// On creation, scans the base directory for existing worktrees left over from
// a previous server run so that Get() calls work immediately after restart.
func NewWorktreeManager(baseDir string, logger *slog.Logger) *WorktreeManager {
	if baseDir == "" {
		home, _ := os.UserHomeDir()
		baseDir = filepath.Join(home, ".deneb", "worktrees")
	}
	m := &WorktreeManager{
		worktrees: make(map[string]*ThreadWorkspace),
		baseDir:   baseDir,
		logger:    logger,
	}
	m.recoverExisting()
	return m
}

// recoverExisting scans the base directory for worktrees left over from a
// previous server run. Each "thread-<id>" subdirectory is probed for a valid
// git checkout and re-registered in the worktrees map so that subsequent
// Get()/Remove() calls work without requiring a fresh Create().
func (m *WorktreeManager) recoverExisting() {
	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		return // baseDir doesn't exist yet — nothing to recover.
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "thread-") {
			continue
		}
		threadID := strings.TrimPrefix(e.Name(), "thread-")
		wtDir := filepath.Join(m.baseDir, e.Name())
		// Verify it's a valid git worktree by checking for .git file/link.
		if _, err := os.Stat(filepath.Join(wtDir, ".git")); err != nil {
			continue
		}
		// Read the current branch name.
		branch, _ := m.runGitOutput(wtDir, "rev-parse", "--abbrev-ref", "HEAD")
		if branch == "" {
			branch = "deneb/thread/" + threadID
		}
		// Best-effort parent dir: not recoverable, use the main repo.
		// The parent dir is only needed for Remove(), which can fall back to
		// direct directory removal if git commands fail.
		m.worktrees[threadID] = &ThreadWorkspace{
			ThreadID:  threadID,
			Dir:       wtDir,
			Branch:    branch,
			CreatedAt: time.Now(), // approximate
		}
		m.logger.Info("discord: recovered existing thread worktree",
			"threadId", threadID, "branch", branch, "dir", wtDir)
	}
}

// Create creates a git worktree for a thread session. The worktree branches
// from the current HEAD of parentDir. Returns the ThreadWorkspace on success.
// If a worktree already exists for the thread, returns it without recreating.
func (m *WorktreeManager) Create(threadID, parentDir string) (*ThreadWorkspace, error) {
	m.mu.Lock()
	if ws, ok := m.worktrees[threadID]; ok {
		m.mu.Unlock()
		return ws, nil
	}
	m.mu.Unlock()

	branch := "deneb/thread/" + threadID
	wtDir := filepath.Join(m.baseDir, "thread-"+threadID)

	// Ensure base directory exists.
	if err := os.MkdirAll(m.baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("create worktree base dir: %w", err)
	}

	// Ensure parentDir is a git repository. If it's a plain directory
	// (e.g. a default workspace that was never git-initialized), initialize
	// it so that worktree creation can succeed.
	if err := m.ensureGitRepo(parentDir); err != nil {
		return nil, fmt.Errorf("ensure git repo at %s: %w", parentDir, err)
	}

	// Clean up any stale worktree at this path.
	if _, err := os.Stat(wtDir); err == nil {
		m.runGit(parentDir, "worktree", "remove", "--force", wtDir)
		os.RemoveAll(wtDir)
	}

	// Create worktree with a new branch from current HEAD.
	// Explicitly pass HEAD so the starting point is deterministic even when
	// the repo is in a detached-HEAD or mid-rebase state.
	if out, err := m.runGitOutput(parentDir, "worktree", "add", "-b", branch, wtDir, "HEAD"); err != nil {
		// If branch already exists, remove it and retry.
		if strings.Contains(out, "already exists") {
			m.runGit(parentDir, "branch", "-D", branch)
			if _, err2 := m.runGitOutput(parentDir, "worktree", "add", "-b", branch, wtDir, "HEAD"); err2 != nil {
				return nil, fmt.Errorf("git worktree add: %w", err2)
			}
		} else {
			return nil, fmt.Errorf("git worktree add: %w (%s)", err, out)
		}
	}

	ws := &ThreadWorkspace{
		ThreadID:  threadID,
		ParentDir: parentDir,
		Dir:       wtDir,
		Branch:    branch,
		CreatedAt: time.Now(),
	}

	m.mu.Lock()
	m.worktrees[threadID] = ws
	m.mu.Unlock()

	m.logger.Info("discord: created thread worktree",
		"threadId", threadID, "branch", branch, "dir", wtDir)
	return ws, nil
}

// Get returns the workspace for a thread, or nil if none exists.
func (m *WorktreeManager) Get(threadID string) *ThreadWorkspace {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.worktrees[threadID]
}

// Remove removes the git worktree and branch for a thread session.
// Safe to call multiple times or with unknown thread IDs.
// Works even after server restart (recovered worktrees may lack ParentDir).
func (m *WorktreeManager) Remove(threadID string) {
	m.mu.Lock()
	ws, ok := m.worktrees[threadID]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.worktrees, threadID)
	m.mu.Unlock()

	// Try git-based cleanup if we have a parent dir reference.
	if ws.ParentDir != "" {
		if err := m.runGit(ws.ParentDir, "worktree", "remove", "--force", ws.Dir); err != nil {
			m.logger.Warn("discord: failed to remove worktree via git, removing directory",
				"threadId", threadID, "dir", ws.Dir, "error", err)
		}
		// Prune stale worktree references.
		m.runGit(ws.ParentDir, "worktree", "prune")
		// Delete the branch (best-effort).
		m.runGit(ws.ParentDir, "branch", "-D", ws.Branch)
	}

	// Always ensure the directory is gone (covers restart recovery case).
	os.RemoveAll(ws.Dir)

	m.logger.Info("discord: removed thread worktree",
		"threadId", threadID, "branch", ws.Branch)
}

// Count returns the number of active worktrees.
func (m *WorktreeManager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.worktrees)
}

// ensureGitRepo checks if dir is a git repository and initializes it if not.
// This handles the case where a workspace directory exists but was never
// git-initialized, which would cause all worktree operations to fail.
func (m *WorktreeManager) ensureGitRepo(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	// Check if already a git repo.
	if err := m.runGit(dir, "rev-parse", "--git-dir"); err == nil {
		return nil
	}
	// Initialize a new git repo with an empty initial commit so that
	// HEAD exists and worktree branching works.
	if err := m.runGit(dir, "init"); err != nil {
		return fmt.Errorf("git init: %w", err)
	}
	if err := m.runGit(dir, "commit", "--allow-empty", "-m", "initial commit"); err != nil {
		return fmt.Errorf("git initial commit: %w", err)
	}
	m.logger.Info("discord: initialized git repo for worktree parent", "dir", dir)
	return nil
}

// runGit executes a git command in the given directory. Returns error on failure.
func (m *WorktreeManager) runGit(dir string, args ...string) error {
	_, err := m.runGitOutput(dir, args...)
	return err
}

// runGitOutput executes a git command and returns combined output.
func (m *WorktreeManager) runGitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
