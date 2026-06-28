// Package code manages git worktrees for Deneb's coding mode.
//
// Each coding task runs in its own auto-generated worktree — a fresh branch and
// directory created off a base clone of a GitHub repo. This isolation is what
// makes the mode safe for a non-coder ("바이브코더"): the agent's edits never
// touch the main checkout, "되돌리기" is just removing the worktree, and
// independent tasks run in parallel without colliding. It mirrors how Deneb's
// own repository uses .claude/worktrees/ for agent isolation.
//
// The Manager only orchestrates git through the injected Runner; it holds no
// network or auth state. GitHub auth for clone/push of private repos is the
// caller's concern (supplied via the git credential environment), never here.
package code

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runner abstracts git execution so tests inject fakes instead of shelling out.
// It mirrors the gateway tool's CommandRunner (same shape) on purpose.
type Runner interface {
	Run(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

// Repo identifies a GitHub repository as owner/name.
type Repo struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

// CloneURL is the HTTPS clone URL. Auth is supplied out of band (a git
// credential helper or token in the environment), never embedded in the URL.
func (r Repo) CloneURL() string {
	return fmt.Sprintf("https://github.com/%s/%s.git", r.Owner, r.Name)
}

func (r Repo) valid() bool {
	return validSegment(r.Owner) && validSegment(r.Name)
}

// validSegment matches GitHub's owner/repo charset (alphanumerics, dash, dot,
// underscore; no leading dash or dot) so a segment can never carry a path
// traversal, separator, or shell-surprising character into the clone URL or the
// worktree path.
func validSegment(s string) bool {
	// Block traversal (".", "..") and leading dash, but allow a leading dot so a
	// real repo like ".github" still works — "a..b" is a literal name, not a path.
	if s == "" || s == "." || s == ".." || s[0] == '-' {
		return false
	}
	for _, c := range s {
		switch {
		case c == '-' || c == '.' || c == '_':
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

// Task is one isolated unit of work living in its own worktree + branch.
type Task struct {
	ID     string // short slug; forms the branch and directory name
	Repo   Repo
	Branch string // deneb/<ID>
	Dir    string // worktree path — the agent's workspace for this task
}

// Manager owns the on-disk layout for coding projects:
//
//	<Root>/<owner>/<repo>/base         ← base clone (tracks origin)
//	<Root>/<owner>/<repo>/wt/<taskID>  ← per-task worktree on branch deneb/<taskID>
//
// DefaultBranch is the base branch new worktrees fork from. Real default-branch
// detection is a later refinement; "main" covers the common case.
type Manager struct {
	Root          string
	DefaultBranch string
	Runner        Runner
}

// NewManager wires a Manager with the real git exec runner.
func NewManager(root string) *Manager {
	return &Manager{Root: root, DefaultBranch: "main", Runner: execRunner{}}
}

const branchPrefix = "deneb/"

func branchName(taskID string) string { return branchPrefix + taskID }

func (m *Manager) defaultBranch() string {
	if m.DefaultBranch == "" {
		return "main"
	}
	return m.DefaultBranch
}

func (m *Manager) basePath(r Repo) string {
	return filepath.Join(m.Root, r.Owner, r.Name, "base")
}

func (m *Manager) worktreePath(r Repo, taskID string) string {
	return filepath.Join(m.Root, r.Owner, r.Name, "wt", taskID)
}

// EnsureBase clones the repo on first use, or fetches the latest if the base
// clone already exists. Idempotent.
func (m *Manager) EnsureBase(ctx context.Context, r Repo) error {
	if !r.valid() {
		return fmt.Errorf("invalid repo %q/%q", r.Owner, r.Name)
	}
	base := m.basePath(r)
	if m.baseExists(ctx, base) {
		_, err := m.Runner.Run(ctx, base, "git", "fetch", "origin")
		return err
	}
	if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
		return fmt.Errorf("create project dir: %w", err)
	}
	_, err := m.Runner.Run(ctx, "", "git", "clone", r.CloneURL(), base)
	return err
}

// baseExists reports whether base is already a git working tree. Routed through
// the Runner (not os.Stat) so the clone-vs-fetch decision stays testable.
func (m *Manager) baseExists(ctx context.Context, base string) bool {
	_, err := m.Runner.Run(ctx, base, "git", "rev-parse", "--git-dir")
	return err == nil
}

// StartTask creates a fresh worktree + branch off the default branch and returns
// the Task whose Dir is the agent's workspace for this unit of work.
func (m *Manager) StartTask(ctx context.Context, r Repo, taskID string) (Task, error) {
	if err := validateTaskID(taskID); err != nil {
		return Task{}, err
	}
	if err := m.EnsureBase(ctx, r); err != nil {
		return Task{}, fmt.Errorf("ensure base: %w", err)
	}
	base := m.basePath(r)
	dir := m.worktreePath(r, taskID)
	branch := branchName(taskID)
	if _, err := m.Runner.Run(ctx, base, "git", "worktree", "add", "-b", branch, dir, "origin/"+m.defaultBranch()); err != nil {
		return Task{}, fmt.Errorf("worktree add: %w", err)
	}
	return Task{ID: taskID, Repo: r, Branch: branch, Dir: dir}, nil
}

// Commit stages everything and commits in the task worktree — one checkpoint.
func (m *Manager) Commit(ctx context.Context, t Task, message string) error {
	if strings.TrimSpace(message) == "" {
		message = "deneb: update"
	}
	if _, err := m.Runner.Run(ctx, t.Dir, "git", "add", "-A"); err != nil {
		return fmt.Errorf("stage changes: %w", err)
	}
	if _, err := m.Runner.Run(ctx, t.Dir, "git", "commit", "-m", message); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Push publishes the task branch to origin (GitHub). The branch — never main —
// is the push target, so the user's primary line is untouched until they merge.
func (m *Manager) Push(ctx context.Context, t Task) error {
	if _, err := m.Runner.Run(ctx, t.Dir, "git", "push", "-u", "origin", t.Branch); err != nil {
		return fmt.Errorf("push %s: %w", t.Branch, err)
	}
	return nil
}

// Discard removes the worktree and deletes its branch — the total "되돌리기".
// The base clone and main checkout are left untouched.
func (m *Manager) Discard(ctx context.Context, t Task) error {
	base := m.basePath(t.Repo)
	if _, err := m.Runner.Run(ctx, base, "git", "worktree", "remove", "--force", t.Dir); err != nil {
		return fmt.Errorf("worktree remove: %w", err)
	}
	if _, err := m.Runner.Run(ctx, base, "git", "branch", "-D", t.Branch); err != nil {
		return fmt.Errorf("delete branch %s: %w", t.Branch, err)
	}
	return nil
}

// Undo reverts the last step in a task worktree — the "되돌리기". If there are
// uncommitted edits it discards them (back to the last checkpoint) and returns
// false: no checkpoint commit was dropped, so the caller must NOT pop a checkpoint
// record. Otherwise it drops the last checkpoint commit and returns true. Two-level,
// one step.
func (m *Manager) Undo(ctx context.Context, t Task) (poppedCheckpoint bool, err error) {
	dirty, err := m.hasUncommitted(ctx, t.Dir)
	if err != nil {
		return false, err
	}
	if dirty {
		if _, err := m.Runner.Run(ctx, t.Dir, "git", "reset", "--hard", "HEAD"); err != nil {
			return false, fmt.Errorf("discard changes: %w", err)
		}
		if _, err := m.Runner.Run(ctx, t.Dir, "git", "clean", "-fd"); err != nil {
			return false, fmt.Errorf("clean untracked: %w", err)
		}
		return false, nil // uncommitted edits discarded; the checkpoint commit stays
	}
	// Clean tree → drop the last checkpoint, but never below the fork point: with
	// no checkpoints there is nothing to undo, and HEAD~1 would walk the branch
	// into upstream history (origin/<default>).
	ahead, err := m.Runner.Run(ctx, t.Dir, "git", "rev-list", "--count", "origin/"+m.defaultBranch()+"..HEAD")
	if err != nil {
		return false, fmt.Errorf("count checkpoints: %w", err)
	}
	if strings.TrimSpace(string(ahead)) == "0" {
		return false, fmt.Errorf("더 되돌릴 변경이 없습니다")
	}
	if _, err := m.Runner.Run(ctx, t.Dir, "git", "reset", "--hard", "HEAD~1"); err != nil {
		return false, fmt.Errorf("revert checkpoint: %w", err)
	}
	return true, nil
}

func (m *Manager) hasUncommitted(ctx context.Context, dir string) (bool, error) {
	out, err := m.Runner.Run(ctx, dir, "git", "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// HeadSHA returns the worktree's current commit SHA — recorded as a checkpoint id.
func (m *Manager) HeadSHA(ctx context.Context, t Task) (string, error) {
	out, err := m.Runner.Run(ctx, t.Dir, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// validateTaskID keeps the id slug-safe so it can form a branch and directory
// name without escaping the worktree tree.
func validateTaskID(id string) error {
	if id == "" {
		return fmt.Errorf("task id is required")
	}
	for _, c := range id {
		if c == '-' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			continue
		}
		return fmt.Errorf("task id %q must be lowercase alphanumeric or dash", id)
	}
	return nil
}

// execRunner is the real Runner: it shells out to git with a per-call working
// directory. CombinedOutput keeps git's stderr (where its errors land).
type execRunner struct{}

func (execRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.CombinedOutput()
}
