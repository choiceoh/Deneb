package autoresearch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// Notifier delivers progress updates to the user (e.g., via Telegram).
type Notifier interface {
	Notify(ctx context.Context, message string) error
	// NotifyPhoto sends a PNG image with an optional caption.
	// Implementations that don't support photos should fall back to Notify.
	NotifyPhoto(ctx context.Context, png []byte, caption string) error
}

// TranscriptAppendFn appends a system note to a session transcript.
// Used to inject completion reports into the triggering session's context.
type TranscriptAppendFn func(sessionKey, text string) error

// Runner manages the autoresearch experiment loop.
type Runner struct {
	mu           sync.Mutex
	cancel       context.CancelFunc
	running      bool
	workdir      string   // original repo directory (state lives here)
	worktreeDirs []string // isolated worktrees for experiments (empty when not running)
	client       *llm.Client
	model        string
	defaultModel string // server-injected default (e.g., lightweight model); overrides Params.DefaultModel
	params       Params // snapshot of tunable params from config at Start() time
	notifier     Notifier
	server       *ServerManager
	logger       *slog.Logger
	// transcriptAppendFn injects results into the triggering session's transcript.
	transcriptAppendFn TranscriptAppendFn
	// sessionKey is the session that started this autoresearch run.
	sessionKey string
	// dedupHint is set when the last hypothesis was a duplicate.
	// The next prompt includes this hint to steer the LLM away from repetition.
	dedupHint string
}

// NewRunner creates an autoresearch runner.
func NewRunner(logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		logger: logger.With("pkg", "autoresearch"),
	}
}

// SetLLMClient sets the LLM client for hypothesis generation.
func (r *Runner) SetLLMClient(client *llm.Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.client = client
}

// SetDefaultModel overrides the default model name used when Config.Model is
// empty. This allows the server to wire the lightweight (local) model for
// autoresearch without changing Config or Params defaults.
func (r *Runner) SetDefaultModel(model string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultModel = model
}

// resolveModel returns the model to use for an LLM call.
// Priority: per-experiment Config.Model > server-injected defaultModel > Params.DefaultModel.
// Must be called with r.mu held.
func (r *Runner) resolveModel(cfg *Config) string {
	if r.model != "" {
		return r.model
	}
	if r.defaultModel != "" {
		return r.defaultModel
	}
	return cfg.Params.DefaultModel
}

// SetNotifier sets the notifier for progress updates.
func (r *Runner) SetNotifier(n Notifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notifier = n
}

// SetTranscriptAppendFn sets the callback for injecting results into the
// session transcript. When set, completion reports are appended as system
// notes to the triggering session so the LLM sees them on the next turn.
func (r *Runner) SetTranscriptAppendFn(fn TranscriptAppendFn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transcriptAppendFn = fn
}

// IsRunning returns whether the loop is currently active.
func (r *Runner) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// Workdir returns the most recently used experiment workspace.
func (r *Runner) Workdir() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.workdir
}

// SetWorkdir records the experiment workspace directory. Called on init
// so that /chart can find the experiment even before start.
func (r *Runner) SetWorkdir(dir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workdir = dir
}

// SetSessionKey records which chat session triggered this autoresearch run.
// When a TranscriptAppendFn is set, completion reports are injected into
// this session's transcript.
func (r *Runner) SetSessionKey(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionKey = key
}

// StatusSnapshot is a point-in-time view of the runner's state, suitable for
// JSON serialization and RPC responses.
type StatusSnapshot struct {
	Running       bool   `json:"running"`
	Workdir       string `json:"workdir,omitempty"`
	WorktreeCount int    `json:"worktree_count"`
	Model         string `json:"model,omitempty"`
}

// Status returns a structured snapshot of the runner's current state.
func (r *Runner) Status() StatusSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := StatusSnapshot{
		Running:       r.running,
		Workdir:       r.workdir,
		WorktreeCount: len(r.worktreeDirs),
	}
	if r.running {
		if r.model != "" {
			snap.Model = r.model
		} else {
			snap.Model = r.defaultModel
		}
	}
	return snap
}

// worktreeSubdir is the directory name for the isolated experiment worktree.
// Stored next to .autoresearch/ in the repo root, gitignored separately.
const worktreeSubdir = ".autoresearch-wt"

// Start launches the autonomous experiment loop in a background goroutine.
// The experiment runs in an isolated git worktree so the main working tree
// is never switched to a different branch or modified.
func (r *Runner) Start(workdir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running {
		return fmt.Errorf("autoresearch already running in %s", r.workdir)
	}

	cfg, err := LoadConfig(workdir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	if r.client == nil {
		return fmt.Errorf("LLM client not configured")
	}

	// Ensure working tree is clean before starting.
	if err := gitEnsureClean(context.Background(), workdir); err != nil {
		return err
	}

	resuming := cfg.Resume && cfg.TotalIterations > 0

	if resuming {
		// Verify the experiment branch exists.
		branchName := "autoresearch/" + cfg.BranchTag
		if !gitBranchExists(context.Background(), workdir, branchName) {
			return fmt.Errorf("resume failed: branch %s not found", branchName)
		}
		// Verify KeptCommit exists in git history.
		if cfg.KeptCommit != "" {
			if _, err := gitRevParse(context.Background(), workdir, cfg.KeptCommit); err != nil {
				return fmt.Errorf("resume failed: kept commit %s not found", cfg.KeptCommit)
			}
		}
		r.logger.Info("resuming autoresearch",
			"from_iteration", cfg.TotalIterations,
			"best_metric", cfg.BestMetric,
			"kept_commit", cfg.KeptCommit)
		// Clear resume flag so subsequent restarts don't auto-resume.
		cfg.Resume = false
		if err := SaveConfig(workdir, cfg); err != nil {
			return fmt.Errorf("save config after resume: %w", err)
		}
	} else {
		// Record original branch only on fresh start.
		currentBranch, _ := gitCurrentBranch(context.Background(), workdir)
		cfg.OriginalBranch = currentBranch
		if err := SaveConfig(workdir, cfg); err != nil {
			return fmt.Errorf("save config with original branch: %w", err)
		}
	}

	// Create isolated worktrees for experiments.
	parallelism := cfg.Params.Parallelism
	worktrees := make([]string, parallelism)

	for i := range parallelism {
		var suffix string
		var branchName string
		if parallelism == 1 {
			suffix = ""
			branchName = "autoresearch/" + cfg.BranchTag
		} else {
			suffix = fmt.Sprintf("-%d", i)
			branchName = fmt.Sprintf("autoresearch/%s-wt%d", cfg.BranchTag, i)
		}

		wtPath := filepath.Join(workdir, worktreeSubdir+suffix)

		// Clean up stale worktree from a previous run if it exists.
		if _, statErr := os.Stat(wtPath); statErr == nil {
			r.logger.Info("cleaning up stale worktree", "path", wtPath)
			_ = gitWorktreeRemove(context.Background(), workdir, wtPath)
			os.RemoveAll(wtPath)
		}

		// Create worktree with experiment branch.
		createBranch := !gitBranchExists(context.Background(), workdir, branchName)
		if err := gitWorktreeAdd(context.Background(), workdir, wtPath, branchName, createBranch); err != nil {
			// Clean up any worktrees we already created.
			for j := range i {
				_ = gitWorktreeRemove(context.Background(), workdir, worktrees[j])
				os.RemoveAll(worktrees[j])
			}
			return fmt.Errorf("create experiment worktree %d: %w", i, err)
		}
		if createBranch {
			r.logger.Info("created experiment branch in worktree", "branch", branchName, "worktree", wtPath)
		} else {
			r.logger.Info("using existing experiment branch in worktree", "branch", branchName, "worktree", wtPath)
		}

		// Symlink .autoresearch/ state from source dir into worktree.
		stateDir := filepath.Join(workdir, configDir)
		wtStateLink := filepath.Join(wtPath, configDir)
		if err := os.Symlink(stateDir, wtStateLink); err != nil {
			r.logger.Warn("symlink failed, copying config instead", "error", err)
			os.MkdirAll(filepath.Join(wtPath, configDir), 0o755)
			if data, readErr := os.ReadFile(configPath(workdir)); readErr == nil {
				os.WriteFile(configPath(wtPath), data, 0o644)
			}
		}

		worktrees[i] = wtPath
	}

	// Record the kept commit as the current HEAD on the primary experiment branch.
	if cfg.KeptCommit == "" {
		headSHA, _ := gitRevParse(context.Background(), worktrees[0], "HEAD")
		cfg.KeptCommit = headSHA
		if err := SaveConfig(workdir, cfg); err != nil {
			r.logger.Warn("failed to save experiment config", "error", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.running = true
	r.workdir = workdir
	r.worktreeDirs = worktrees
	r.model = cfg.Model
	r.params = cfg.Params

	// Start persistent server if configured.
	if cfg.Params.ServerCmd != "" {
		sm := NewServerManager(r.logger)
		if err := sm.Start(worktrees[0], cfg.Params.ServerCmd, cfg.Params.ServerHealthURL, cfg.Params.ServerStartupSec); err != nil {
			r.logger.Error("failed to start persistent server", "error", err)
			// Non-fatal: continue without persistent server.
		} else {
			// Record initial content hash.
			if hash, err := contentHash(worktrees[0], cfg.TargetFiles); err == nil {
				sm.SetHash(hash)
			}
			r.server = sm
		}
	}

	// Primary worktree is used for sequential mode loop.
	go r.loop(ctx, worktrees[0])
	r.logger.Info("autoresearch started", "workdir", workdir,
		"worktrees", len(worktrees), "metric", cfg.MetricName,
		"parallelism", parallelism)
	return nil
}

// Stop halts the running experiment loop. Worktree cleanup happens in the
// loop's defer after the completion report is sent.
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.running = false
	r.logger.Info("autoresearch stopped")
}

// cleanupWorktree removes all experiment worktrees. Called from the loop's
// defer after the completion report has been sent.
func (r *Runner) cleanupWorktree() {
	r.mu.Lock()
	wtDirs := r.worktreeDirs
	srcDir := r.workdir
	sm := r.server
	r.worktreeDirs = nil
	r.server = nil
	r.mu.Unlock()

	// Stop persistent server before removing worktrees.
	if sm != nil {
		sm.Stop()
	}

	if srcDir == "" {
		return
	}

	for _, wtDir := range wtDirs {
		if wtDir == "" {
			continue
		}
		// Remove the .autoresearch symlink first so git worktree remove
		// doesn't follow it and delete the source state.
		os.Remove(filepath.Join(wtDir, configDir))

		if err := gitWorktreeRemove(context.Background(), srcDir, wtDir); err != nil {
			r.logger.Error("failed to remove worktree", "path", wtDir, "error", err)
			os.RemoveAll(wtDir)
		}
		r.logger.Info("cleaned up experiment worktree", "path", wtDir)
	}
}

// loop is the main experiment loop. It runs until ctx is cancelled or
// MaxIterations is reached (if > 0). On exit — regardless of reason
// (max iterations, manual stop, context cancel, panic) — it sends a
// completion report with summary and chart to the notifier.
func (r *Runner) loop(ctx context.Context, workdir string) {
	stopReason := "stopped"

	defer func() {
		if rv := recover(); rv != nil {
			r.logger.Error("autoresearch panic recovered", "panic", rv)
			stopReason = fmt.Sprintf("crashed (panic: %v)", rv)
		}
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()

		// Always send a completion report on exit, regardless of stop reason.
		r.sendCompletionReport(workdir, stopReason)

		// Clean up the isolated worktree after reporting.
		r.cleanupWorktree()
	}()

	for {
		select {
		case <-ctx.Done():
			stopReason = "manually stopped"
			return
		default:
		}

		// Check if we've reached the max iteration limit.
		if r.params.MaxIterations > 0 {
			cfg, err := LoadConfig(workdir)
			if err == nil && cfg.TotalIterations >= r.params.MaxIterations {
				r.logger.Info("autoresearch reached max iterations",
					"total", cfg.TotalIterations, "max", r.params.MaxIterations)
				stopReason = fmt.Sprintf("completed (%d/%d iterations)", cfg.TotalIterations, r.params.MaxIterations)
				return
			}
		}

		var err error
		if r.params.Parallelism > 1 {
			err = r.runParallelIteration(ctx)
		} else {
			err = r.runOneIteration(ctx, workdir)
		}
		if err != nil {
			if ctx.Err() != nil {
				stopReason = "cancelled"
				return
			}
			r.logger.Error("autoresearch iteration failed", "error", err)
			// Brief pause before retrying after error.
			select {
			case <-ctx.Done():
				stopReason = "cancelled"
				return
			case <-time.After(time.Duration(r.params.RetryPauseSec) * time.Second):
			}
		}
	}
}

// --- Git helpers ---

func gitCommit(ctx context.Context, dir, message string) error {
	// Stage all changes.
	add := exec.CommandContext(ctx, "git", "add", "-A")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %s", string(out))
	}
	// Commit.
	commit := exec.CommandContext(ctx, "git", "commit", "-m", message)
	commit.Dir = dir
	if out, err := commit.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %s", string(out))
	}
	return nil
}

func gitResetHard(ctx context.Context, dir, ref string) {
	if ref == "" {
		return
	}
	cmd := exec.CommandContext(ctx, "git", "reset", "--hard", ref)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Error("git reset --hard failed", "error", err, "output", string(out))
	}
}

func gitRevParse(ctx context.Context, dir, ref string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--short", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitWorktreeAdd creates an isolated git worktree at wtPath on the given branch.
// If createBranch is true, the branch is created from the current HEAD.
func gitWorktreeAdd(ctx context.Context, repoDir, wtPath, branch string, createBranch bool) error {
	args := []string{"worktree", "add"}
	if createBranch {
		args = append(args, "-b", branch, wtPath)
	} else {
		args = append(args, wtPath, branch)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add %s (%s): %s", wtPath, branch, string(out))
	}
	return nil
}

// gitWorktreeRemove removes a git worktree. It first tries a normal remove,
// then falls back to --force if needed.
func gitWorktreeRemove(ctx context.Context, repoDir, wtPath string) error {
	cmd := exec.CommandContext(ctx, "git", "worktree", "remove", wtPath)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		// Force remove on failure (e.g., dirty worktree).
		cmd2 := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", wtPath)
		cmd2.Dir = repoDir
		if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
			return fmt.Errorf("git worktree remove --force %s: %s (first attempt: %s)", wtPath, string(out2), string(out))
		}
	}
	return nil
}

func gitCurrentBranch(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitBranchExists(ctx context.Context, dir, branch string) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "refs/heads/"+branch)
	cmd.Dir = dir
	return cmd.Run() == nil
}

// gitEnsureClean checks that the working tree is clean (no uncommitted changes).
func gitEnsureClean(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		return fmt.Errorf("working tree is dirty — commit or stash changes before starting autoresearch")
	}
	return nil
}
