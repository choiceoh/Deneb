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
func NewWorktreeManager(baseDir string, logger *slog.Logger) *WorktreeManager {
	if baseDir == "" {
		home, _ := os.UserHomeDir()
		baseDir = filepath.Join(home, ".deneb", "worktrees")
	}
	return &WorktreeManager{
		worktrees: make(map[string]*ThreadWorkspace),
		baseDir:   baseDir,
		logger:    logger,
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

	// Clean up any stale worktree at this path.
	if _, err := os.Stat(wtDir); err == nil {
		m.runGit(parentDir, "worktree", "remove", "--force", wtDir)
		os.RemoveAll(wtDir)
	}

	// Create worktree with a new branch from current HEAD.
	if out, err := m.runGitOutput(parentDir, "worktree", "add", "-b", branch, wtDir); err != nil {
		// If branch already exists, try without -b.
		if strings.Contains(out, "already exists") {
			m.runGit(parentDir, "branch", "-D", branch)
			if _, err2 := m.runGitOutput(parentDir, "worktree", "add", "-b", branch, wtDir); err2 != nil {
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
func (m *WorktreeManager) Remove(threadID string) {
	m.mu.Lock()
	ws, ok := m.worktrees[threadID]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.worktrees, threadID)
	m.mu.Unlock()

	// Remove the worktree.
	if err := m.runGit(ws.ParentDir, "worktree", "remove", "--force", ws.Dir); err != nil {
		m.logger.Warn("discord: failed to remove worktree via git, removing directory",
			"threadId", threadID, "dir", ws.Dir, "error", err)
		os.RemoveAll(ws.Dir)
	}

	// Prune stale worktree references.
	m.runGit(ws.ParentDir, "worktree", "prune")

	// Delete the branch (best-effort).
	m.runGit(ws.ParentDir, "branch", "-D", ws.Branch)

	m.logger.Info("discord: removed thread worktree",
		"threadId", threadID, "branch", ws.Branch)
}

// Count returns the number of active worktrees.
func (m *WorktreeManager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.worktrees)
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
