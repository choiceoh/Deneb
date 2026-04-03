package autoresearch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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

// RunBaseline executes the metric command once to establish a baseline value.
// Called during init before any modifications are made.
func RunBaseline(ctx context.Context, workdir string, cfg *Config) (float64, error) {
	cfg.Params.applyDefaults()
	timeout := time.Duration(cfg.TimeBudgetSec) * time.Second
	grace := time.Duration(cfg.Params.GracePeriodSec) * time.Second
	expCtx, cancel := context.WithTimeout(ctx, timeout+grace)
	defer cancel()

	cmd := exec.CommandContext(expCtx, "bash", "-c", cfg.MetricCmd)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), fmt.Sprintf("TIME_BUDGET=%d", cfg.TimeBudgetSec))

	// Provide cache directory for baseline run too, so expensive results
	// computed during baseline are available for subsequent iterations.
	if cacheDir := cfg.ResolveCacheDir(workdir); cacheDir != "" {
		if mkErr := os.MkdirAll(cacheDir, 0o755); mkErr == nil {
			cmd.Env = append(cmd.Env, "AUTORESEARCH_CACHE_DIR="+cacheDir)
			cmd.Env = append(cmd.Env,
				"GOCACHE="+filepath.Join(cacheDir, "go-build"),
			)
		}
	}
	cmd.Env = append(cmd.Env, "AUTORESEARCH_ITERATION=0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("baseline command failed: %w\nOutput: %s", err, string(output))
	}

	metric, err := extractMetricSmart(string(output), cfg.MetricPattern)
	if err != nil {
		return 0, fmt.Errorf("baseline metric extraction failed: %w", err)
	}

	// Save baseline output.
	if err := SaveExperimentOutput(workdir, 0, string(output), ""); err != nil {
		slog.Warn("failed to save baseline output", "error", err)
	}

	// Record baseline as iteration 0 in results.
	if err := AppendResult(workdir, ResultRow{
		Iteration:   0,
		Timestamp:   time.Now(),
		Hypothesis:  "baseline",
		MetricValue: metric,
		Kept:        true,
		DurationSec: int(timeout.Seconds()),
		BestSoFar:   metric,
	}); err != nil {
		slog.Warn("failed to append baseline result", "error", err)
	}

	return metric, nil
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

	// Record original branch.
	currentBranch, _ := gitCurrentBranch(context.Background(), workdir)
	cfg.OriginalBranch = currentBranch
	if err := SaveConfig(workdir, cfg); err != nil {
		return fmt.Errorf("save config with original branch: %w", err)
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

// sendCompletionReport generates and sends the final experiment summary
// and chart to the notifier (Telegram). Called from the loop's defer on
// any exit — max iterations, manual stop, cancel, or panic.
func (r *Runner) sendCompletionReport(workdir string, reason string) {
	ctx := context.Background()

	cfg, err := LoadConfig(workdir)
	if err != nil {
		r.notify(ctx, fmt.Sprintf("Autoresearch %s (no config found for report).", reason))
		return
	}

	// Skip report if no iterations were run.
	if cfg.TotalIterations == 0 {
		r.notify(ctx, fmt.Sprintf("Autoresearch %s (no iterations completed).", reason))
		return
	}

	// Send text summary.
	summary := Summary(workdir, cfg)
	r.notify(ctx, fmt.Sprintf("Autoresearch %s\n\n%s", reason, summary))

	// Generate and send chart.
	rows, err := ParseResults(workdir)
	if err != nil || len(rows) == 0 {
		r.logger.Warn("skipping chart: no results to render", "error", err)
		return
	}
	png, err := RenderChart(rows, cfg)
	if err != nil {
		r.logger.Error("failed to render completion chart", "error", err)
		return
	}
	// Also save to disk for later reference.
	if _, saveErr := SaveChart(workdir, rows, cfg); saveErr != nil {
		r.logger.Warn("failed to save chart to disk", "error", saveErr)
	}

	caption := fmt.Sprintf("%s — %d iterations", cfg.MetricName, cfg.TotalIterations)
	if cfg.BestMetric != nil {
		caption = fmt.Sprintf("%s — %d iterations, best: %.6f",
			cfg.MetricName, cfg.TotalIterations, *cfg.BestMetric)
		if cfg.BaselineMetric != nil && *cfg.BaselineMetric != 0 {
			improvement := (*cfg.BaselineMetric - *cfg.BestMetric) / *cfg.BaselineMetric * 100
			if cfg.MetricDirection == "maximize" {
				improvement = (*cfg.BestMetric - *cfg.BaselineMetric) / *cfg.BaselineMetric * 100
			}
			caption += fmt.Sprintf(" (%.2f%%)", improvement)
		}
	}
	r.notifyPhoto(ctx, png, caption)

	// Inject completion summary into the triggering session's transcript
	// so the LLM has context about the results on its next turn.
	r.injectResultToTranscript(reason, summary)
}

// injectResultToTranscript appends the autoresearch completion summary to the
// triggering session's transcript as a system note.
func (r *Runner) injectResultToTranscript(reason, summary string) {
	r.mu.Lock()
	fn := r.transcriptAppendFn
	key := r.sessionKey
	r.mu.Unlock()

	if fn == nil || key == "" {
		return
	}

	note := fmt.Sprintf("[Autoresearch %s]\n\n%s", reason, summary)
	// Truncate to avoid bloating the transcript.
	const maxLen = 4000
	if len(note) > maxLen {
		note = note[:maxLen] + "\n... (truncated)"
	}

	if err := fn(key, note); err != nil {
		r.logger.Warn("failed to inject autoresearch result into transcript",
			"sessionKey", key,
			"error", err,
		)
	} else {
		r.logger.Info("injected autoresearch result into session transcript",
			"sessionKey", key,
			"summaryLen", len(note),
		)
	}
}

// runOneIteration performs a single modify-verify-decide cycle.
func (r *Runner) runOneIteration(ctx context.Context, workdir string) error {
	cfg, err := LoadConfig(workdir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	iteration := cfg.TotalIterations + 1

	// Step 1: Read target files for context.
	fileContents := make(map[string]string)
	for _, f := range cfg.TargetFiles {
		path := filepath.Join(workdir, f)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read target file %s: %w", f, err)
		}
		fileContents[f] = string(data)
	}

	// Step 2: Read results history.
	resultsHistory, _ := ReadResults(workdir)

	// Constants override mode: delegate to separate iteration path.
	if cfg.IsConstantsMode() {
		return r.runConstantsIteration(ctx, workdir, cfg, fileContents, resultsHistory, iteration)
	}

	// Step 3: Ask LLM for a hypothesis and code modification.
	prompt := r.buildPrompt(cfg, fileContents, resultsHistory, iteration)

	r.mu.Lock()
	client := r.client
	model := r.resolveModel(cfg)
	r.mu.Unlock()

	llmResp, err := client.Complete(ctx, llm.ChatRequest{
		Model:     model,
		System:    llm.SystemString(prompt.system),
		Messages:  []llm.Message{llm.NewTextMessage("user", prompt.user)},
		MaxTokens: cfg.Params.MaxTokens,
	})
	if err != nil {
		return fmt.Errorf("LLM call failed: %w", err)
	}

	// Step 4: Parse the LLM response for hypothesis and file changes.
	hypothesis, changes := parseLLMResponse(llmResp, cfg.TargetFiles)
	if hypothesis == "" {
		hypothesis = fmt.Sprintf("iteration %d", iteration)
	}
	if len(changes) == 0 {
		r.logger.Warn("LLM produced no code changes, skipping iteration")
		cfg.ConsecutiveFailures++
		if err := SaveConfig(workdir, cfg); err != nil {
			return err
		}
		return nil
	}

	// Step 5: Apply code changes.
	for filename, content := range changes {
		path := filepath.Join(workdir, filename)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write file %s: %w", filename, err)
		}
	}

	// Step 6: Git commit the changes.
	commitMsg := fmt.Sprintf("autoresearch #%d: %s", iteration, hypothesis)
	if err := gitCommit(ctx, workdir, commitMsg); err != nil {
		// Revert on commit failure.
		gitResetHard(ctx, workdir, cfg.KeptCommit)
		return fmt.Errorf("git commit: %w", err)
	}

	commitHash, _ := gitRevParse(ctx, workdir, "HEAD")

	// Compute content hash once for server restart + caching.
	fileHash, _ := contentHash(workdir, cfg.TargetFiles)

	// Restart persistent server if target files changed.
	if r.server != nil && fileHash != "" {
		if _, restartErr := r.server.RestartIfNeeded(fileHash); restartErr != nil {
			r.logger.Warn("server restart failed", "error", restartErr)
		}
	}

	// Step 7: Run experiment with time budget (check cache first).
	var expResult *experimentResult
	var runErr error
	var duration int
	cacheHit := false

	if cfg.CacheEnabled && fileHash != "" {
		cacheDir := cfg.ResolveCacheDir(workdir)
		if metric, ok := loadCachedMetric(cacheDir, fileHash, cfg.MetricCmd); ok {
			r.logger.Info("cache hit, skipping experiment", "hash", fileHash, "metric", metric)
			expResult = &experimentResult{metric: metric, stdout: fmt.Sprintf("cached: %.6f", metric)}
			cacheHit = true
		}
	}

	if !cacheHit {
		startTime := time.Now()
		expResult, runErr = r.runExperiment(ctx, workdir, cfg, iteration)
		duration = int(time.Since(startTime).Seconds())

		// Cache the result on success.
		if runErr == nil && cfg.CacheEnabled && fileHash != "" {
			cacheDir := cfg.ResolveCacheDir(workdir)
			if saveErr := saveCachedMetric(cacheDir, fileHash, cfg.MetricCmd, expResult.metric); saveErr != nil {
				r.logger.Warn("failed to cache metric", "error", saveErr)
			}
		}
	}

	// Save experiment output for debugging/analysis.
	if expResult != nil && !cacheHit {
		if saveErr := SaveExperimentOutput(workdir, iteration, expResult.stdout, expResult.stderr); saveErr != nil {
			r.logger.Error("failed to save experiment output", "error", saveErr)
		}
	}

	// Step 8: Evaluate and decide.
	row := ResultRow{
		Iteration:   iteration,
		Timestamp:   time.Now(),
		Hypothesis:  hypothesis,
		DurationSec: duration,
	}

	// Track the running best for the results table.
	currentBest := float64(0)
	if cfg.BestMetric != nil {
		currentBest = *cfg.BestMetric
	}

	if runErr != nil {
		// Experiment crashed — revert.
		r.logger.Warn("experiment crashed", "error", runErr, "iteration", iteration)
		gitResetHard(ctx, workdir, cfg.KeptCommit)
		row.MetricValue = 0
		row.Kept = false
		row.CommitHash = ""
		row.BestSoFar = currentBest
		row.DeltaFromBest = 0
		cfg.ConsecutiveFailures++
		r.notify(ctx, fmt.Sprintf("Iteration #%d CRASHED: %s\nHypothesis: %s", iteration, runErr, hypothesis))
	} else {
		metricValue := expResult.metric
		row.MetricValue = metricValue
		bestMetric := cfg.BestMetric
		if bestMetric == nil {
			// First successful iteration — always keep.
			row.Kept = true
			row.DeltaFromBest = 0
		} else {
			row.Kept = cfg.IsBetter(metricValue, *bestMetric)
			row.DeltaFromBest = metricValue - *bestMetric
		}

		if row.Kept {
			row.CommitHash = commitHash
			row.BestSoFar = metricValue
			cfg.BestMetric = &metricValue
			cfg.BestCommit = commitHash
			cfg.KeptCommit = commitHash
			cfg.KeptIterations++
			cfg.ConsecutiveFailures = 0

			// Build improvement info for notification.
			improvementInfo := ""
			if cfg.BaselineMetric != nil && *cfg.BaselineMetric != 0 {
				improvement := (*cfg.BaselineMetric - metricValue) / *cfg.BaselineMetric * 100
				if cfg.MetricDirection == "maximize" {
					improvement = (metricValue - *cfg.BaselineMetric) / *cfg.BaselineMetric * 100
				}
				improvementInfo = fmt.Sprintf(" (%.2f%% from baseline)", improvement)
			}
			r.notify(ctx, fmt.Sprintf("Iteration #%d KEPT: %s=%.6f%s\nHypothesis: %s",
				iteration, cfg.MetricName, metricValue, improvementInfo, hypothesis))
		} else {
			// Revert to last kept commit.
			gitResetHard(ctx, workdir, cfg.KeptCommit)
			row.CommitHash = ""
			row.BestSoFar = currentBest
			cfg.ConsecutiveFailures++
			r.notify(ctx, fmt.Sprintf("Iteration #%d DISCARDED: %s=%.6f (best=%.6f)\nHypothesis: %s",
				iteration, cfg.MetricName, metricValue, *bestMetric, hypothesis))
		}
	}

	cfg.TotalIterations = iteration

	// Persist results and config.
	if err := AppendResult(workdir, row); err != nil {
		r.logger.Error("failed to append result", "error", err)
	}
	if err := SaveConfig(workdir, cfg); err != nil {
		r.logger.Error("failed to save config", "error", err)
	}

	return nil
}

// promptParts holds the system and user prompts for the LLM.
type promptParts struct {
	system string
	user   string
}

// buildPrompt constructs the LLM prompt for hypothesis generation.
// Designed to match or exceed the depth of karpathy/autoresearch's program.md.
func (r *Runner) buildPrompt(cfg *Config, files map[string]string, results string, iteration int) promptParts {
	budget := strconv.Itoa(cfg.TimeBudgetSec)
	direction := cfg.MetricDirection // "minimize" or "maximize"
	better := "lower"
	worse := "higher"
	if direction == "maximize" {
		better = "higher"
		worse = "lower"
	}

	var sys strings.Builder

	// --- Identity and mission ---
	sys.WriteString("You are an autonomous research agent running an iterative experiment loop.\n")
	sys.WriteString("Your sole objective: " + direction + " the metric `" + cfg.MetricName + "` as much as possible.\n")
	sys.WriteString("You operate in a loop: each iteration you propose ONE change, it gets tested for " + budget + " seconds, ")
	sys.WriteString("and the result is kept if " + better + " or reverted if " + worse + " than the current best.\n\n")

	// --- Hard constraints ---
	sys.WriteString("=== HARD CONSTRAINTS (NEVER VIOLATE) ===\n\n")
	sys.WriteString("1. ONLY modify the target files listed below. Modifying any other file is forbidden.\n")
	sys.WriteString("2. Do NOT add new dependencies, libraries, or imports that are not already present.\n")
	sys.WriteString("3. Do NOT remove or disable the metric evaluation logic — the experiment must still produce a valid metric.\n")
	sys.WriteString("4. Do NOT add sleep/delay/busy-wait to consume the time budget without doing useful work.\n")
	sys.WriteString("5. Do NOT hardcode or fabricate metric values.\n")
	sys.WriteString("6. Each experiment has a FIXED time budget of " + budget + " seconds. Optimize for what you can achieve within this wall-clock limit.\n")
	sys.WriteString("7. Output COMPLETE file contents — not diffs, not partial snippets. Every target file you modify must be reproduced in full.\n\n")

	// --- Strategy guidance ---
	sys.WriteString("=== STRATEGY GUIDANCE ===\n\n")
	sys.WriteString("EXPLORATION vs EXPLOITATION:\n")
	sys.WriteString("- Early iterations (1-10): explore broadly. Try different approaches, architectures, hyperparameters.\n")
	sys.WriteString("- Mid iterations (11-30): exploit what works. Refine the best-performing approaches.\n")
	sys.WriteString("- Late iterations (30+): fine-tune. Make small, precise adjustments to squeeze out gains.\n\n")

	sys.WriteString("CHANGE GRANULARITY:\n")
	sys.WriteString("- Make ONE focused, atomic change per iteration. Never combine multiple unrelated changes.\n")
	sys.WriteString("- If a change doesn't improve the metric, the ENTIRE change is reverted. There is no partial credit.\n")
	sys.WriteString("- Smaller changes are easier to evaluate. A 2-line change that improves the metric is better than a 50-line rewrite that doesn't.\n\n")

	sys.WriteString("LEARNING FROM HISTORY:\n")
	sys.WriteString("- Study the experiment history carefully. Identify which TYPES of changes tend to work.\n")
	sys.WriteString("- If increasing X worked before, try increasing X further (but watch for diminishing returns).\n")
	sys.WriteString("- If a direction (e.g., larger model) consistently fails, STOP trying that direction.\n")
	sys.WriteString("- Look for interactions: if change A worked and change B worked, they may conflict or compound.\n")
	sys.WriteString("- When a kept change improved the metric, understand WHY it worked before building on it.\n\n")

	sys.WriteString("TIME BUDGET AWARENESS:\n")
	sys.WriteString("- The experiment runs for exactly " + budget + " seconds. Changes that require longer are wasted.\n")
	sys.WriteString("- A smaller but faster configuration may beat a larger one simply by doing more optimization steps.\n")
	sys.WriteString("- Consider the computational cost: a 2x model size means ~2x time per step, halving the number of steps.\n\n")

	// --- Stuck recovery (progressive) ---
	if cfg.ConsecutiveFailures >= cfg.Params.StuckThresholdCritical {
		sys.WriteString(fmt.Sprintf("=== CRITICAL: %d+ CONSECUTIVE FAILURES ===\n", cfg.Params.StuckThresholdCritical))
		sys.WriteString("You are deeply stuck. Drastic measures required:\n")
		sys.WriteString("- Revert to the SIMPLEST possible configuration that is known to work.\n")
		sys.WriteString("- Discard all complex hypotheses. Start from first principles.\n")
		sys.WriteString("- Consider whether the metric command or evaluation setup has issues.\n")
		sys.WriteString("- Try a change that is the OPPOSITE of your recent failed attempts.\n\n")
	} else if cfg.ConsecutiveFailures >= cfg.Params.StuckThresholdModerate {
		sys.WriteString(fmt.Sprintf("=== WARNING: %d+ CONSECUTIVE FAILURES ===\n", cfg.Params.StuckThresholdModerate))
		sys.WriteString("Your current strategy is not working. Required changes:\n")
		sys.WriteString("- Abandon the current approach entirely and try a fundamentally different direction.\n")
		sys.WriteString("- Review the KEPT experiments in history — what made them succeed? Return to those principles.\n")
		sys.WriteString("- Consider reverting to a well-known, simpler architecture or configuration.\n")
		sys.WriteString("- The definition of insanity is trying the same thing and expecting different results.\n\n")
	} else if cfg.ConsecutiveFailures >= cfg.Params.StuckThresholdMild {
		sys.WriteString(fmt.Sprintf("=== NOTE: %d+ CONSECUTIVE FAILURES ===\n", cfg.Params.StuckThresholdMild))
		sys.WriteString("Recent changes are not yielding improvements. Consider:\n")
		sys.WriteString("- Changing strategy (e.g., if you've been tuning hyperparameters, try architectural changes instead).\n")
		sys.WriteString("- Making a smaller, more conservative change.\n")
		sys.WriteString("- Re-reading the experiment history to identify unexplored directions.\n\n")
	}

	// --- Response format ---
	sys.WriteString("=== RESPONSE FORMAT (STRICT) ===\n\n")
	sys.WriteString("You MUST follow this exact format:\n\n")
	sys.WriteString("HYPOTHESIS: <one-line description of what you're changing and why you expect it to improve " + cfg.MetricName + ">\n\n")
	sys.WriteString("Then for EACH file you are modifying (you may modify multiple target files):\n\n")
	sys.WriteString("--- FILE: <filename> ---\n")
	sys.WriteString("<complete file content — every single line>\n")
	sys.WriteString("--- END FILE ---\n\n")
	sys.WriteString("IMPORTANT:\n")
	sys.WriteString("- The HYPOTHESIS line must come FIRST, before any file content.\n")
	sys.WriteString("- The hypothesis should explain your REASONING, not just describe the change.\n")
	sys.WriteString("  Bad:  'HYPOTHESIS: change learning rate to 0.002'\n")
	sys.WriteString("  Good: 'HYPOTHESIS: double learning rate because the loss curve shows slow convergence in early steps'\n")
	sys.WriteString("- You MUST output complete file contents. Incomplete files will cause errors.\n")
	sys.WriteString("- Do not include explanatory text between or after file blocks.\n")

	// --- User prompt: context ---
	var usr strings.Builder

	usr.WriteString("=== TARGET FILES (you may ONLY modify these) ===\n\n")
	for name, content := range files {
		lineCount := strings.Count(content, "\n") + 1
		usr.WriteString(fmt.Sprintf("--- %s (%d lines) ---\n", name, lineCount))
		usr.WriteString(content)
		usr.WriteString("\n--- end " + name + " ---\n\n")
	}

	if results != "" {
		usr.WriteString("=== EXPERIMENT HISTORY ===\n")
		usr.WriteString("Format: iteration | timestamp | hypothesis | metric_value | kept | commit | duration | best_so_far | delta\n\n")
		usr.WriteString(results)
		usr.WriteString("\n")
	}

	// Add trend analysis to help the LLM learn from patterns.
	rows, _ := ParseResults(r.workdir)
	if len(rows) > 0 {
		usr.WriteString("=== TREND ANALYSIS ===\n")
		usr.WriteString(TrendAnalysis(rows, cfg))
		usr.WriteString("\n")

		// Add kept-only history for easier pattern recognition.
		var keptSummary strings.Builder
		for _, row := range rows {
			if row.Kept && row.Iteration > 0 {
				keptSummary.WriteString(fmt.Sprintf("  #%d: %s=%.6f — %s\n",
					row.Iteration, cfg.MetricName, row.MetricValue, row.Hypothesis))
			}
		}
		if keptSummary.Len() > 0 {
			usr.WriteString("=== SUCCESSFUL CHANGES (kept only) ===\n")
			usr.WriteString(keptSummary.String())
			usr.WriteString("\n")
		}

		// Add recent failed hypotheses to avoid repetition.
		var failedRecent strings.Builder
		start := len(rows) - cfg.Params.RecentFailedWindow
		if start < 0 {
			start = 0
		}
		for _, row := range rows[start:] {
			if !row.Kept && row.Iteration > 0 {
				failedRecent.WriteString(fmt.Sprintf("  #%d: %s=%.6f — %s\n",
					row.Iteration, cfg.MetricName, row.MetricValue, row.Hypothesis))
			}
		}
		if failedRecent.Len() > 0 {
			usr.WriteString("=== RECENT FAILURES (do NOT repeat these) ===\n")
			usr.WriteString(failedRecent.String())
			usr.WriteString("\n")
		}
	}

	// --- Task ---
	usr.WriteString(fmt.Sprintf("=== YOUR TASK: ITERATION %d ===\n\n", iteration))
	if cfg.BaselineMetric != nil {
		usr.WriteString(fmt.Sprintf("Baseline %s: %.6f\n", cfg.MetricName, *cfg.BaselineMetric))
	}
	if cfg.BestMetric != nil {
		usr.WriteString(fmt.Sprintf("Current best %s: %.6f", cfg.MetricName, *cfg.BestMetric))
		if cfg.BaselineMetric != nil && *cfg.BaselineMetric != 0 {
			improvement := (*cfg.BaselineMetric - *cfg.BestMetric) / *cfg.BaselineMetric * 100
			if cfg.MetricDirection == "maximize" {
				improvement = (*cfg.BestMetric - *cfg.BaselineMetric) / *cfg.BaselineMetric * 100
			}
			usr.WriteString(fmt.Sprintf(" (%.2f%% improvement from baseline)", improvement))
		}
		usr.WriteString("\n")
	}
	usr.WriteString(fmt.Sprintf("Consecutive failures: %d\n\n", cfg.ConsecutiveFailures))

	if iteration <= cfg.Params.PhaseEarlyEnd {
		usr.WriteString("You are in the EARLY phase. Explore broadly — try different approaches to understand the landscape.\n")
	} else if iteration <= cfg.Params.PhaseExplorationEnd {
		usr.WriteString("You are in the EXPLORATION phase. Balance between trying new ideas and refining what works.\n")
	} else if iteration <= cfg.Params.PhaseExploitationEnd {
		usr.WriteString("You are in the EXPLOITATION phase. Focus on refining the approaches that have produced the best results.\n")
	} else {
		usr.WriteString("You are in the FINE-TUNING phase. Make small, precise adjustments. The easy gains are likely behind you.\n")
	}

	usr.WriteString("\nPropose ONE atomic change. Explain your reasoning in the HYPOTHESIS line. Output the complete modified file(s).")

	return promptParts{system: sys.String(), user: usr.String()}
}

// buildConstantsPrompt constructs the LLM prompt for constants override mode.
// The agent can only propose new values for named constants — not rewrite files.
func (r *Runner) buildConstantsPrompt(cfg *Config, files map[string]string,
	currentValues map[string]string, results string, iteration int) promptParts {

	budget := strconv.Itoa(cfg.TimeBudgetSec)
	direction := cfg.MetricDirection
	better := "lower"
	worse := "higher"
	if direction == "maximize" {
		better = "higher"
		worse = "lower"
	}

	var sys strings.Builder

	// --- Identity and mission ---
	sys.WriteString("You are an autonomous research agent running an iterative experiment loop.\n")
	sys.WriteString("Your sole objective: " + direction + " the metric `" + cfg.MetricName + "` as much as possible.\n")
	sys.WriteString("You operate in CONSTANTS OVERRIDE mode: each iteration you propose new values for a set of named constants, ")
	sys.WriteString("the values are applied, the experiment runs for " + budget + " seconds, ")
	sys.WriteString("and the result is kept if " + better + " or reverted if " + worse + " than the current best.\n")
	sys.WriteString("You CANNOT modify any source code — only the listed constants.\n\n")

	// --- Hard constraints ---
	sys.WriteString("=== HARD CONSTRAINTS (NEVER VIOLATE) ===\n\n")
	sys.WriteString("1. You may ONLY change the listed constants below. You cannot modify any other code.\n")
	sys.WriteString("2. Each constant has a type (float/int/string) and optional min/max bounds. Respect them.\n")
	sys.WriteString("3. Do NOT propose values that would break the experiment or produce invalid metrics.\n")
	sys.WriteString("4. Each experiment has a FIXED time budget of " + budget + " seconds.\n")
	sys.WriteString("5. You do NOT need to output file contents — only constant values.\n\n")

	// --- Strategy guidance ---
	sys.WriteString("=== STRATEGY GUIDANCE ===\n\n")
	sys.WriteString("EXPLORATION vs EXPLOITATION:\n")
	sys.WriteString("- Early iterations (1-10): explore the constant space broadly. Try different scales and combinations.\n")
	sys.WriteString("- Mid iterations (11-30): exploit what works. Refine the best-performing value ranges.\n")
	sys.WriteString("- Late iterations (30+): fine-tune. Make small, precise adjustments.\n\n")

	sys.WriteString("SEARCH STRATEGY:\n")
	sys.WriteString("- Change ONE or TWO constants per iteration. Isolated changes are easier to evaluate.\n")
	sys.WriteString("- If increasing X improved the metric, try increasing X further (watch for diminishing returns).\n")
	sys.WriteString("- If a direction consistently fails, STOP trying that direction.\n")
	sys.WriteString("- Consider interactions: constants may have nonlinear effects on each other.\n\n")

	sys.WriteString("TIME BUDGET AWARENESS:\n")
	sys.WriteString("- The experiment runs for exactly " + budget + " seconds. Constants that increase computation time may reduce steps.\n")
	sys.WriteString("- A smaller/faster configuration may beat a larger one by doing more optimization steps.\n\n")

	// --- Stuck recovery (same as buildPrompt) ---
	if cfg.ConsecutiveFailures >= cfg.Params.StuckThresholdCritical {
		sys.WriteString(fmt.Sprintf("=== CRITICAL: %d+ CONSECUTIVE FAILURES ===\n", cfg.Params.StuckThresholdCritical))
		sys.WriteString("You are deeply stuck. Drastic measures required:\n")
		sys.WriteString("- Revert ALL constants to their original values.\n")
		sys.WriteString("- Try a single, small change from the baseline.\n")
		sys.WriteString("- Try the OPPOSITE direction of your recent failed attempts.\n\n")
	} else if cfg.ConsecutiveFailures >= cfg.Params.StuckThresholdModerate {
		sys.WriteString(fmt.Sprintf("=== WARNING: %d+ CONSECUTIVE FAILURES ===\n", cfg.Params.StuckThresholdModerate))
		sys.WriteString("Your current strategy is not working. Required changes:\n")
		sys.WriteString("- Abandon the current search direction entirely.\n")
		sys.WriteString("- Review the KEPT experiments — what value ranges worked? Return to those.\n")
		sys.WriteString("- Consider changing a DIFFERENT constant than the one you've been tuning.\n\n")
	} else if cfg.ConsecutiveFailures >= cfg.Params.StuckThresholdMild {
		sys.WriteString(fmt.Sprintf("=== NOTE: %d+ CONSECUTIVE FAILURES ===\n", cfg.Params.StuckThresholdMild))
		sys.WriteString("Recent changes are not yielding improvements. Consider:\n")
		sys.WriteString("- Changing a different constant.\n")
		sys.WriteString("- Making a smaller, more conservative change.\n")
		sys.WriteString("- Re-reading the experiment history to identify unexplored ranges.\n\n")
	}

	// --- Response format ---
	sys.WriteString("=== RESPONSE FORMAT (STRICT) ===\n\n")
	sys.WriteString("You MUST follow this exact format:\n\n")
	sys.WriteString("HYPOTHESIS: <one-line description of what you're changing and why>\n\n")
	sys.WriteString("<CONSTANT_NAME> = <new_value>\n")
	sys.WriteString("<CONSTANT_NAME> = <new_value>\n\n")
	sys.WriteString("IMPORTANT:\n")
	sys.WriteString("- The HYPOTHESIS line must come FIRST.\n")
	sys.WriteString("- Only list constants you want to CHANGE. Omitted constants keep their current values.\n")
	sys.WriteString("- The hypothesis should explain your REASONING, not just describe the change.\n")
	sys.WriteString("  Bad:  'HYPOTHESIS: change learning rate to 0.002'\n")
	sys.WriteString("  Good: 'HYPOTHESIS: double learning rate because the loss curve shows slow convergence'\n")
	sys.WriteString("- Do not output any other text, code, or file contents.\n")

	// --- User prompt ---
	var usr strings.Builder

	usr.WriteString("=== TUNABLE CONSTANTS ===\n\n")
	for _, cd := range cfg.Constants {
		val := currentValues[cd.Name]
		usr.WriteString(fmt.Sprintf("%s = %s  (type: %s", cd.Name, val, cd.Type))
		if cd.Min != nil {
			usr.WriteString(fmt.Sprintf(", min: %v", *cd.Min))
		}
		if cd.Max != nil {
			usr.WriteString(fmt.Sprintf(", max: %v", *cd.Max))
		}
		usr.WriteString(fmt.Sprintf(", file: %s)\n", cd.File))
	}
	usr.WriteString("\n")

	// Source files as read-only context.
	usr.WriteString("=== SOURCE FILES (read-only context — do NOT modify) ===\n\n")
	for name, content := range files {
		lineCount := strings.Count(content, "\n") + 1
		usr.WriteString(fmt.Sprintf("--- %s (%d lines) ---\n", name, lineCount))
		usr.WriteString(content)
		usr.WriteString("\n--- end " + name + " ---\n\n")
	}

	if results != "" {
		usr.WriteString("=== EXPERIMENT HISTORY ===\n")
		usr.WriteString("Format: iteration | timestamp | hypothesis | metric_value | kept | commit | duration | best_so_far | delta\n\n")
		usr.WriteString(results)
		usr.WriteString("\n")
	}

	// Trend analysis and history (same as buildPrompt).
	rows, _ := ParseResults(r.workdir)
	if len(rows) > 0 {
		usr.WriteString("=== TREND ANALYSIS ===\n")
		usr.WriteString(TrendAnalysis(rows, cfg))
		usr.WriteString("\n")

		var keptSummary strings.Builder
		for _, row := range rows {
			if row.Kept && row.Iteration > 0 {
				keptSummary.WriteString(fmt.Sprintf("  #%d: %s=%.6f — %s\n",
					row.Iteration, cfg.MetricName, row.MetricValue, row.Hypothesis))
			}
		}
		if keptSummary.Len() > 0 {
			usr.WriteString("=== SUCCESSFUL CHANGES (kept only) ===\n")
			usr.WriteString(keptSummary.String())
			usr.WriteString("\n")
		}

		var failedRecent strings.Builder
		start := len(rows) - cfg.Params.RecentFailedWindow
		if start < 0 {
			start = 0
		}
		for _, row := range rows[start:] {
			if !row.Kept && row.Iteration > 0 {
				failedRecent.WriteString(fmt.Sprintf("  #%d: %s=%.6f — %s\n",
					row.Iteration, cfg.MetricName, row.MetricValue, row.Hypothesis))
			}
		}
		if failedRecent.Len() > 0 {
			usr.WriteString("=== RECENT FAILURES (do NOT repeat these) ===\n")
			usr.WriteString(failedRecent.String())
			usr.WriteString("\n")
		}
	}

	// Task section.
	usr.WriteString(fmt.Sprintf("=== YOUR TASK: ITERATION %d ===\n\n", iteration))
	if cfg.BaselineMetric != nil {
		usr.WriteString(fmt.Sprintf("Baseline %s: %.6f\n", cfg.MetricName, *cfg.BaselineMetric))
	}
	if cfg.BestMetric != nil {
		usr.WriteString(fmt.Sprintf("Current best %s: %.6f", cfg.MetricName, *cfg.BestMetric))
		if cfg.BaselineMetric != nil && *cfg.BaselineMetric != 0 {
			improvement := (*cfg.BaselineMetric - *cfg.BestMetric) / *cfg.BaselineMetric * 100
			if cfg.MetricDirection == "maximize" {
				improvement = (*cfg.BestMetric - *cfg.BaselineMetric) / *cfg.BaselineMetric * 100
			}
			usr.WriteString(fmt.Sprintf(" (%.2f%% improvement from baseline)", improvement))
		}
		usr.WriteString("\n")
	}
	usr.WriteString(fmt.Sprintf("Consecutive failures: %d\n\n", cfg.ConsecutiveFailures))

	if iteration <= cfg.Params.PhaseEarlyEnd {
		usr.WriteString("You are in the EARLY phase. Explore the constant space broadly.\n")
	} else if iteration <= cfg.Params.PhaseExplorationEnd {
		usr.WriteString("You are in the EXPLORATION phase. Balance between trying new ranges and refining what works.\n")
	} else if iteration <= cfg.Params.PhaseExploitationEnd {
		usr.WriteString("You are in the EXPLOITATION phase. Focus on refining the value ranges that produced the best results.\n")
	} else {
		usr.WriteString("You are in the FINE-TUNING phase. Make small, precise adjustments to constants.\n")
	}

	usr.WriteString("\nPropose new values for one or more constants. Explain your reasoning in the HYPOTHESIS line.")

	return promptParts{system: sys.String(), user: usr.String()}
}

// parseLLMResponse extracts hypothesis and file changes from the LLM output.
func parseLLMResponse(resp string, targetFiles []string) (string, map[string]string) {
	var hypothesis string
	changes := make(map[string]string)

	// Extract hypothesis.
	lines := strings.SplitN(resp, "\n", 2)
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "HYPOTHESIS:") {
		hypothesis = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[0]), "HYPOTHESIS:"))
	}

	// Extract file contents between --- FILE: <name> --- and --- END FILE ---.
	filePattern := regexp.MustCompile(`(?s)---\s*FILE:\s*(\S+)\s*---\n(.*?)---\s*END FILE\s*---`)
	matches := filePattern.FindAllStringSubmatch(resp, -1)
	for _, m := range matches {
		filename := strings.TrimSpace(m[1])
		content := m[2]
		// Only accept changes to target files.
		for _, tf := range targetFiles {
			if filename == tf {
				changes[filename] = content
				break
			}
		}
	}

	return hypothesis, changes
}

// experimentResult holds the full output of an experiment run.
type experimentResult struct {
	metric float64
	stdout string
	stderr string
}

// runExperiment executes the metric command with a time budget.
func (r *Runner) runExperiment(ctx context.Context, workdir string, cfg *Config, iteration int) (*experimentResult, error) {
	timeout := time.Duration(cfg.TimeBudgetSec) * time.Second
	grace := time.Duration(cfg.Params.GracePeriodSec) * time.Second
	expCtx, cancel := context.WithTimeout(ctx, timeout+grace)
	defer cancel()

	cmd := exec.CommandContext(expCtx, "bash", "-c", cfg.MetricCmd)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), fmt.Sprintf("TIME_BUDGET=%d", cfg.TimeBudgetSec))

	// Warm-start env vars: metric scripts can use these for early termination
	// or adaptive quality checks without any script changes required.
	cmd.Env = append(cmd.Env, fmt.Sprintf("AUTORESEARCH_ITERATION=%d", iteration))
	if cfg.BestMetric != nil {
		cmd.Env = append(cmd.Env, fmt.Sprintf("AUTORESEARCH_BEST_METRIC=%.6f", *cfg.BestMetric))
	}

	// Pass persistent server URL if a server is running.
	if r.server != nil && r.server.IsRunning() {
		cmd.Env = append(cmd.Env, "AUTORESEARCH_SERVER_URL="+r.server.URL())
	}

	// Provide a persistent cache directory for expensive operations
	// (LLM inference, embeddings, etc.) that don't change across iterations.
	if cacheDir := cfg.ResolveCacheDir(workdir); cacheDir != "" {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			r.logger.Warn("failed to create cache dir", "path", cacheDir, "error", err)
		}
		cmd.Env = append(cmd.Env, "AUTORESEARCH_CACHE_DIR="+cacheDir)

		// Build cache: share Go build cache across iterations for incremental builds.
		cmd.Env = append(cmd.Env,
			"GOCACHE="+filepath.Join(cacheDir, "go-build"),
			"GOMODCACHE="+filepath.Join(cacheDir, "go-mod"),
		)
	}

	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()

	if err != nil {
		return &experimentResult{stdout: stdout, stderr: stderr},
			fmt.Errorf("experiment command failed: %w", err)
	}

	// Parse metric from stdout using pattern or heuristic.
	metric, mErr := extractMetricSmart(stdout, cfg.MetricPattern)
	if mErr != nil {
		return &experimentResult{stdout: stdout, stderr: stderr},
			fmt.Errorf("metric extraction failed: %w\nStdout tail: %s", mErr, tailLines(stdout, 5))
	}

	return &experimentResult{metric: metric, stdout: stdout, stderr: stderr}, nil
}

// tailLines returns the last n lines of s.
func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// extractMetricWithPattern extracts a metric using an explicit regex pattern.
// The pattern must have exactly one capture group for the numeric value.
func extractMetricWithPattern(output, pattern string) (float64, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return 0, fmt.Errorf("invalid metric_pattern %q: %w", pattern, err)
	}
	// Search all lines bottom-up for the pattern.
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		m := re.FindStringSubmatch(lines[i])
		if len(m) >= 2 {
			val, err := strconv.ParseFloat(strings.TrimSpace(m[1]), 64)
			if err != nil {
				continue
			}
			return val, nil
		}
	}
	return 0, fmt.Errorf("metric_pattern %q matched nothing in output", pattern)
}

// extractMetric finds a floating-point number in the last non-empty line of output.
// This is the default heuristic when no metric_pattern is configured.
func extractMetric(output string) (float64, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		// Try to parse the whole line as a number first.
		if val, err := strconv.ParseFloat(line, 64); err == nil {
			return val, nil
		}
		// Try to find a number in the line (e.g. "val_bpb: 1.087").
		numPattern := regexp.MustCompile(`[-+]?\d+\.?\d*(?:[eE][-+]?\d+)?`)
		matches := numPattern.FindAllString(line, -1)
		if len(matches) > 0 {
			// Take the last number on the line.
			if val, err := strconv.ParseFloat(matches[len(matches)-1], 64); err == nil {
				return val, nil
			}
		}
	}
	return 0, fmt.Errorf("no numeric metric found in output")
}

// extractMetricSmart dispatches to pattern-based or heuristic extraction,
// then validates the result for plausibility (NaN, Inf).
func extractMetricSmart(output, pattern string) (float64, error) {
	var val float64
	var err error
	if pattern != "" {
		val, err = extractMetricWithPattern(output, pattern)
	} else {
		val, err = extractMetric(output)
	}
	if err != nil {
		return 0, err
	}
	// Validate plausibility.
	if math.IsNaN(val) {
		return 0, fmt.Errorf("metric value is NaN")
	}
	if math.IsInf(val, 0) {
		return 0, fmt.Errorf("metric value is Inf")
	}
	return val, nil
}

// --- Metric caching ---

// contentHash computes a deterministic SHA-256 hash of all target files.
// Identical file contents produce the same hash regardless of iteration.
func contentHash(workdir string, targetFiles []string) (string, error) {
	sorted := make([]string, len(targetFiles))
	copy(sorted, targetFiles)
	sort.Strings(sorted)

	h := sha256.New()
	for _, f := range sorted {
		data, err := os.ReadFile(filepath.Join(workdir, f))
		if err != nil {
			return "", fmt.Errorf("read %s for hash: %w", f, err)
		}
		h.Write([]byte(f))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// metricCacheEntry is the on-disk format for cached metric results.
type metricCacheEntry struct {
	Metric    float64 `json:"metric"`
	MetricCmd string  `json:"metric_cmd"`
	Timestamp string  `json:"timestamp"`
}

// loadCachedMetric checks if a metric result is cached for the given content hash.
// Returns the cached metric and true if found and the metric_cmd matches.
func loadCachedMetric(cacheDir, hash, metricCmd string) (float64, bool) {
	path := filepath.Join(cacheDir, "results", hash+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	var entry metricCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return 0, false
	}
	// Invalidate if the metric command changed.
	if entry.MetricCmd != metricCmd {
		return 0, false
	}
	return entry.Metric, true
}

// saveCachedMetric persists a metric result for the given content hash.
func saveCachedMetric(cacheDir, hash, metricCmd string, metric float64) error {
	dir := filepath.Join(cacheDir, "results")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entry := metricCacheEntry{
		Metric:    metric,
		MetricCmd: metricCmd,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, hash+".json"), data, 0o644)
}

// overrideHash computes a deterministic hash for constants-mode overrides.
func overrideHash(base string, overrides map[string]string) string {
	h := sha256.New()
	h.Write([]byte(base))
	h.Write([]byte{0})

	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte("="))
		h.Write([]byte(overrides[k]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// notify sends a message via the notifier if one is configured.
func (r *Runner) notify(ctx context.Context, msg string) {
	r.mu.Lock()
	n := r.notifier
	r.mu.Unlock()
	if n != nil {
		if err := n.Notify(ctx, msg); err != nil {
			r.logger.Error("notification failed", "error", err)
		}
	}
}

func (r *Runner) notifyPhoto(ctx context.Context, png []byte, caption string) {
	r.mu.Lock()
	n := r.notifier
	r.mu.Unlock()
	if n != nil {
		if err := n.NotifyPhoto(ctx, png, caption); err != nil {
			r.logger.Error("photo notification failed", "error", err)
		}
	}
}

// runConstantsIteration performs a single constants-override iteration:
// extract → LLM proposes values → apply overrides → experiment → restore → evaluate.
func (r *Runner) runConstantsIteration(ctx context.Context, workdir string, cfg *Config,
	fileContents map[string]string, resultsHistory string, iteration int) error {

	// Step 1: Extract current constant values from original files.
	currentValues, err := ExtractConstants(workdir, cfg.Constants)
	if err != nil {
		return fmt.Errorf("extract constants: %w", err)
	}

	// Step 2: Build constants-specific prompt.
	prompt := r.buildConstantsPrompt(cfg, fileContents, currentValues, resultsHistory, iteration)

	r.mu.Lock()
	client := r.client
	model := r.resolveModel(cfg)
	r.mu.Unlock()

	llmResp, err := client.Complete(ctx, llm.ChatRequest{
		Model:     model,
		System:    llm.SystemString(prompt.system),
		Messages:  []llm.Message{llm.NewTextMessage("user", prompt.user)},
		MaxTokens: cfg.Params.MaxTokens,
	})
	if err != nil {
		return fmt.Errorf("LLM call failed: %w", err)
	}

	// Step 3: Parse override values from LLM response.
	hypothesis, overrides := parseConstantsLLMResponse(llmResp, cfg.Constants)
	if hypothesis == "" {
		hypothesis = fmt.Sprintf("iteration %d", iteration)
	}
	if len(overrides) == 0 {
		r.logger.Warn("LLM produced no constant overrides, skipping iteration")
		cfg.ConsecutiveFailures++
		if err := SaveConfig(workdir, cfg); err != nil {
			return err
		}
		return nil
	}

	// Step 4: Apply overrides temporarily.
	restore, err := ApplyOverrides(workdir, cfg.Constants, overrides)
	if err != nil {
		return fmt.Errorf("apply overrides: %w", err)
	}
	// Panic guard: restore files if experiment panics. The explicit
	// restore() below handles the normal path. ApplyOverrides uses
	// sync.Once internally so double-calls are safe.
	defer func() { restore() }()

	// Step 5: Run experiment with overridden files (check cache first).
	var expResult *experimentResult
	var runErr error
	var duration int
	cacheHit := false

	if cfg.CacheEnabled {
		baseHash, _ := contentHash(workdir, cfg.TargetFiles)
		hash := overrideHash(baseHash, overrides)
		cacheDir := cfg.ResolveCacheDir(workdir)
		if metric, ok := loadCachedMetric(cacheDir, hash, cfg.MetricCmd); ok {
			r.logger.Info("cache hit (constants), skipping experiment", "hash", hash, "metric", metric)
			expResult = &experimentResult{metric: metric, stdout: fmt.Sprintf("cached: %.6f", metric)}
			cacheHit = true
		}
	}

	if !cacheHit {
		startTime := time.Now()
		expResult, runErr = r.runExperiment(ctx, workdir, cfg, iteration)
		duration = int(time.Since(startTime).Seconds())

		// Cache the result on success.
		if runErr == nil && cfg.CacheEnabled {
			baseHash, _ := contentHash(workdir, cfg.TargetFiles)
			hash := overrideHash(baseHash, overrides)
			cacheDir := cfg.ResolveCacheDir(workdir)
			if saveErr := saveCachedMetric(cacheDir, hash, cfg.MetricCmd, expResult.metric); saveErr != nil {
				r.logger.Warn("failed to cache metric", "error", saveErr)
			}
		}
	}

	if expResult != nil && !cacheHit {
		if saveErr := SaveExperimentOutput(workdir, iteration, expResult.stdout, expResult.stderr); saveErr != nil {
			r.logger.Error("failed to save experiment output", "error", saveErr)
		}
	}

	// Step 6: Restore originals BEFORE evaluating (files must be clean for git).
	restore()

	// Step 7: Evaluate and decide.
	row := ResultRow{
		Iteration:   iteration,
		Timestamp:   time.Now(),
		Hypothesis:  hypothesis,
		DurationSec: duration,
	}

	currentBest := float64(0)
	if cfg.BestMetric != nil {
		currentBest = *cfg.BestMetric
	}

	if runErr != nil {
		r.logger.Warn("experiment crashed", "error", runErr, "iteration", iteration)
		row.MetricValue = 0
		row.Kept = false
		row.BestSoFar = currentBest
		row.DeltaFromBest = 0
		cfg.ConsecutiveFailures++
		r.notify(ctx, fmt.Sprintf("Iteration #%d CRASHED: %s\nHypothesis: %s", iteration, runErr, hypothesis))
	} else {
		metricValue := expResult.metric
		row.MetricValue = metricValue
		bestMetric := cfg.BestMetric
		if bestMetric == nil {
			row.Kept = true
			row.DeltaFromBest = 0
		} else {
			row.Kept = cfg.IsBetter(metricValue, *bestMetric)
			row.DeltaFromBest = metricValue - *bestMetric
		}

		if row.Kept {
			row.BestSoFar = metricValue

			// Save best overrides to overrides.json.
			ov := &OverrideSet{Values: overrides}
			if saveErr := SaveOverrides(workdir, ov); saveErr != nil {
				r.logger.Error("failed to save overrides", "error", saveErr)
			}

			// Commit overrides.json (not modified source files).
			// Only update BestMetric/BestCommit after a successful commit
			// to avoid inconsistent state when commit fails.
			commitMsg := fmt.Sprintf("autoresearch #%d: %s", iteration, hypothesis)
			if commitErr := gitCommit(ctx, workdir, commitMsg); commitErr != nil {
				r.logger.Error("failed to commit overrides, treating as discarded", "error", commitErr)
				row.Kept = false
				row.BestSoFar = currentBest
				row.DeltaFromBest = 0
				cfg.ConsecutiveFailures++
				r.notify(ctx, fmt.Sprintf("Iteration #%d DISCARD (commit failed): %s=%.6f\nHypothesis: %s",
					iteration, cfg.MetricName, metricValue, hypothesis))
			} else {
				commitHash, _ := gitRevParse(ctx, workdir, "HEAD")
				row.CommitHash = commitHash
				cfg.BestMetric = &metricValue
				cfg.BestCommit = commitHash
				cfg.KeptCommit = commitHash
				cfg.KeptIterations++
				cfg.ConsecutiveFailures = 0

				improvementInfo := ""
				if cfg.BaselineMetric != nil && *cfg.BaselineMetric != 0 {
					improvement := (*cfg.BaselineMetric - metricValue) / *cfg.BaselineMetric * 100
					if cfg.MetricDirection == "maximize" {
						improvement = (metricValue - *cfg.BaselineMetric) / *cfg.BaselineMetric * 100
					}
					improvementInfo = fmt.Sprintf(" (%.2f%% from baseline)", improvement)
				}
				r.notify(ctx, fmt.Sprintf("Iteration #%d KEPT: %s=%.6f%s\nHypothesis: %s\nOverrides: %v",
					iteration, cfg.MetricName, metricValue, improvementInfo, hypothesis, overrides))
			}
		} else {
			row.BestSoFar = currentBest
			cfg.ConsecutiveFailures++
			r.notify(ctx, fmt.Sprintf("Iteration #%d DISCARDED: %s=%.6f (best=%.6f)\nHypothesis: %s",
				iteration, cfg.MetricName, metricValue, *bestMetric, hypothesis))
		}
	}

	cfg.TotalIterations = iteration

	if err := AppendResult(workdir, row); err != nil {
		r.logger.Error("failed to append result", "error", err)
	}
	if err := SaveConfig(workdir, cfg); err != nil {
		r.logger.Error("failed to save config", "error", err)
	}

	return nil
}

// --- Parallel experiments ---

// hypothesisResult holds a parsed hypothesis from a multi-hypothesis LLM response.
type hypothesisResult struct {
	hypothesis string
	changes    map[string]string // filename -> content (file mode)
	overrides  map[string]string // constant name -> value (constants mode)
}

// parseMultiHypothesisResponse parses N hypotheses from a single LLM response.
// Expected format:
//
//	=== HYPOTHESIS 1 ===
//	HYPOTHESIS: ...
//	--- FILE: path ---
//	...
//	--- END FILE ---
//	=== HYPOTHESIS 2 ===
//	...
//
// Falls back to single-hypothesis parsing if no multi markers found.
func parseMultiHypothesisResponse(resp string, n int, targetFiles []string) []hypothesisResult {
	// Split on hypothesis markers.
	marker := regexp.MustCompile(`(?m)^=== HYPOTHESIS \d+ ===\s*$`)
	indices := marker.FindAllStringIndex(resp, -1)

	if len(indices) == 0 {
		// Fallback: parse as single hypothesis.
		hyp, changes := parseLLMResponse(resp, targetFiles)
		if hyp == "" && len(changes) == 0 {
			return nil
		}
		return []hypothesisResult{{hypothesis: hyp, changes: changes}}
	}

	var results []hypothesisResult
	for i, idx := range indices {
		var section string
		if i+1 < len(indices) {
			section = resp[idx[1]:indices[i+1][0]]
		} else {
			section = resp[idx[1]:]
		}
		hyp, changes := parseLLMResponse(section, targetFiles)
		if hyp == "" && len(changes) == 0 {
			continue
		}
		results = append(results, hypothesisResult{hypothesis: hyp, changes: changes})
	}

	// Cap to requested count.
	if len(results) > n {
		results = results[:n]
	}
	return results
}

// parseMultiConstantsResponse parses N constant-override hypotheses from a single LLM response.
func parseMultiConstantsResponse(resp string, n int, constants []ConstantDef) []hypothesisResult {
	marker := regexp.MustCompile(`(?m)^=== HYPOTHESIS \d+ ===\s*$`)
	indices := marker.FindAllStringIndex(resp, -1)

	if len(indices) == 0 {
		hyp, overrides := parseConstantsLLMResponse(resp, constants)
		if hyp == "" && len(overrides) == 0 {
			return nil
		}
		return []hypothesisResult{{hypothesis: hyp, overrides: overrides}}
	}

	var results []hypothesisResult
	for i, idx := range indices {
		var section string
		if i+1 < len(indices) {
			section = resp[idx[1]:indices[i+1][0]]
		} else {
			section = resp[idx[1]:]
		}
		hyp, overrides := parseConstantsLLMResponse(section, constants)
		if hyp == "" && len(overrides) == 0 {
			continue
		}
		results = append(results, hypothesisResult{hypothesis: hyp, overrides: overrides})
	}

	if len(results) > n {
		results = results[:n]
	}
	return results
}

// parallelExpResult holds the outcome of one parallel experiment slot.
type parallelExpResult struct {
	variant    int
	hypothesis string
	metric     float64
	commitHash string
	expResult  *experimentResult
	err        error
	duration   int
}

// runParallelIteration runs N hypotheses in parallel across multiple worktrees.
// This is the core parallel speedup: metric execution time stays constant
// while evaluating N alternatives.
func (r *Runner) runParallelIteration(ctx context.Context) error {
	// Load config from any worktree (all symlink to same state dir).
	primaryWt := r.worktreeDirs[0]
	cfg, err := LoadConfig(primaryWt)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	iteration := cfg.TotalIterations + 1
	n := len(r.worktreeDirs)

	// Step 1: Read target files from the primary worktree.
	fileContents := make(map[string]string)
	for _, f := range cfg.TargetFiles {
		data, err := os.ReadFile(filepath.Join(primaryWt, f))
		if err != nil {
			return fmt.Errorf("read target file %s: %w", f, err)
		}
		fileContents[f] = string(data)
	}

	resultsHistory, _ := ReadResults(primaryWt)

	// Step 2: Generate N hypotheses via a single LLM call.
	r.mu.Lock()
	client := r.client
	model := r.resolveModel(cfg)
	r.mu.Unlock()

	var hypotheses []hypothesisResult

	if cfg.IsConstantsMode() {
		currentValues, err := ExtractConstants(primaryWt, cfg.Constants)
		if err != nil {
			return fmt.Errorf("extract constants: %w", err)
		}
		prompt := r.buildConstantsPrompt(cfg, fileContents, currentValues, resultsHistory, iteration)
		// Inject parallel instruction into the system prompt.
		parallelSys := prompt.system + fmt.Sprintf("\n\n=== PARALLEL MODE ===\nGenerate exactly %d DIFFERENT hypotheses in this iteration.\nEach hypothesis MUST be clearly distinct from the others.\nUse this format:\n\n=== HYPOTHESIS 1 ===\nHYPOTHESIS: ...\nCONSTANT_NAME = value\n...\n=== HYPOTHESIS 2 ===\n...\n\nDiversity is critical: if all hypotheses are similar, they waste parallel slots.\n", n)

		llmResp, err := client.Complete(ctx, llm.ChatRequest{
			Model:     model,
			System:    llm.SystemString(parallelSys),
			Messages:  []llm.Message{llm.NewTextMessage("user", prompt.user)},
			MaxTokens: cfg.Params.MaxTokens * n, // scale tokens for N hypotheses
		})
		if err != nil {
			return fmt.Errorf("LLM call failed: %w", err)
		}
		hypotheses = parseMultiConstantsResponse(llmResp, n, cfg.Constants)
	} else {
		prompt := r.buildPrompt(cfg, fileContents, resultsHistory, iteration)
		parallelSys := prompt.system + fmt.Sprintf("\n\n=== PARALLEL MODE ===\nGenerate exactly %d DIFFERENT hypotheses in this iteration.\nEach hypothesis MUST be clearly distinct from the others.\nUse this format:\n\n=== HYPOTHESIS 1 ===\nHYPOTHESIS: ...\n--- FILE: path ---\n...\n--- END FILE ---\n=== HYPOTHESIS 2 ===\n...\n\nDiversity is critical: if all hypotheses are similar, they waste parallel slots.\n", n)

		llmResp, err := client.Complete(ctx, llm.ChatRequest{
			Model:     model,
			System:    llm.SystemString(parallelSys),
			Messages:  []llm.Message{llm.NewTextMessage("user", prompt.user)},
			MaxTokens: cfg.Params.MaxTokens * n,
		})
		if err != nil {
			return fmt.Errorf("LLM call failed: %w", err)
		}
		hypotheses = parseMultiHypothesisResponse(llmResp, n, cfg.TargetFiles)
	}

	if len(hypotheses) == 0 {
		r.logger.Warn("LLM produced no hypotheses for parallel iteration")
		cfg.ConsecutiveFailures++
		if err := SaveConfig(primaryWt, cfg); err != nil {
			return err
		}
		return nil
	}
	r.logger.Info("generated parallel hypotheses", "requested", n, "received", len(hypotheses))

	// Step 3: Apply each hypothesis to its own worktree + git commit.
	// If we got fewer hypotheses than worktrees, only use that many slots.
	activeSlots := len(hypotheses)
	commitHashes := make([]string, activeSlots)
	restoreFns := make([]func(), activeSlots) // constants mode restore functions
	slotReady := make([]bool, activeSlots)

	for i, hyp := range hypotheses {
		wt := r.worktreeDirs[i]

		if cfg.IsConstantsMode() {
			restore, err := ApplyOverrides(wt, cfg.Constants, hyp.overrides)
			if err != nil {
				r.logger.Warn("failed to apply overrides", "variant", i, "error", err)
				continue
			}
			restoreFns[i] = restore
			slotReady[i] = true
		} else {
			for filename, content := range hyp.changes {
				path := filepath.Join(wt, filename)
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					r.logger.Warn("failed to write file", "variant", i, "file", filename, "error", err)
				}
			}
			commitMsg := fmt.Sprintf("autoresearch #%d/%d: %s", iteration, i, hyp.hypothesis)
			if err := gitCommit(ctx, wt, commitMsg); err != nil {
				r.logger.Warn("failed to commit", "variant", i, "error", err)
				gitResetHard(ctx, wt, cfg.KeptCommit)
				continue
			}
			hash, _ := gitRevParse(ctx, wt, "HEAD")
			commitHashes[i] = hash
			slotReady[i] = true
		}
	}

	// Step 4: Run N metrics in parallel — the core speedup.
	// Check cache first for each slot to avoid redundant metric runs.
	results := make([]parallelExpResult, activeSlots)
	cacheDir := cfg.ResolveCacheDir(primaryWt)

	var wg sync.WaitGroup
	for i := range activeSlots {
		if !slotReady[i] {
			results[i].err = fmt.Errorf("slot not ready")
			continue
		}

		// Cache check: skip metric run if we've seen this exact state before.
		if cfg.CacheEnabled && cacheDir != "" {
			wt := r.worktreeDirs[i]
			var hash string
			if cfg.IsConstantsMode() {
				baseHash, _ := contentHash(wt, cfg.TargetFiles)
				hash = overrideHash(baseHash, hypotheses[i].overrides)
			} else {
				hash, _ = contentHash(wt, cfg.TargetFiles)
			}
			if hash != "" {
				if metric, ok := loadCachedMetric(cacheDir, hash, cfg.MetricCmd); ok {
					r.logger.Info("parallel cache hit", "variant", i, "hash", hash, "metric", metric)
					results[i] = parallelExpResult{
						variant:    i,
						hypothesis: hypotheses[i].hypothesis,
						metric:     metric,
						expResult:  &experimentResult{metric: metric, stdout: fmt.Sprintf("cached: %.6f", metric)},
						commitHash: commitHashes[i],
					}
					continue
				}
			}
		}

		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			start := time.Now()
			expResult, runErr := r.runExperiment(ctx, r.worktreeDirs[idx], cfg, iteration)
			results[idx] = parallelExpResult{
				variant:    idx,
				hypothesis: hypotheses[idx].hypothesis,
				expResult:  expResult,
				err:        runErr,
				duration:   int(time.Since(start).Seconds()),
			}
			if runErr == nil && expResult != nil {
				results[idx].metric = expResult.metric
			}
			results[idx].commitHash = commitHashes[idx]

			// Cache on success.
			if runErr == nil && cfg.CacheEnabled && cacheDir != "" {
				wt := r.worktreeDirs[idx]
				var hash string
				if cfg.IsConstantsMode() {
					baseHash, _ := contentHash(wt, cfg.TargetFiles)
					hash = overrideHash(baseHash, hypotheses[idx].overrides)
				} else {
					hash, _ = contentHash(wt, cfg.TargetFiles)
				}
				if hash != "" {
					saveCachedMetric(cacheDir, hash, cfg.MetricCmd, expResult.metric)
				}
			}
		}(i)
	}
	wg.Wait()

	// Constants mode: restore all worktrees to original state.
	for i, restore := range restoreFns {
		if restore != nil {
			restore()
			restoreFns[i] = nil
		}
	}

	// Step 5: Evaluate all results, pick best.
	currentBest := float64(0)
	if cfg.BestMetric != nil {
		currentBest = *cfg.BestMetric
	}

	bestIdx := -1
	var bestMetric float64
	for i, res := range results {
		if res.err != nil {
			continue
		}
		if bestIdx == -1 || cfg.IsBetter(res.metric, bestMetric) {
			bestIdx = i
			bestMetric = res.metric
		}
	}

	// Step 6: Record all results + decide keep/discard.
	// The kept variant's row is deferred until after commit (Bug fix:
	// constants mode needs the commit hash before recording the row).
	keptAny := false
	var keptRow *ResultRow
	for i, res := range results {
		row := ResultRow{
			Iteration:   iteration,
			Timestamp:   time.Now(),
			Hypothesis:  hypotheses[i].hypothesis,
			MetricValue: res.metric,
			DurationSec: res.duration,
			Variant:     i,
		}

		if res.err != nil {
			row.Kept = false
			row.BestSoFar = currentBest
			row.DeltaFromBest = 0
		} else if i == bestIdx && (cfg.BestMetric == nil || cfg.IsBetter(bestMetric, *cfg.BestMetric)) {
			row.Kept = true
			row.CommitHash = res.commitHash
			row.BestSoFar = bestMetric
			row.DeltaFromBest = bestMetric - currentBest
			keptAny = true
		} else {
			row.Kept = false
			row.BestSoFar = currentBest
			if cfg.BestMetric != nil {
				row.DeltaFromBest = res.metric - *cfg.BestMetric
			}
		}

		// Save experiment output.
		if res.expResult != nil {
			logFile := fmt.Sprintf("%04d_%d.log", iteration, i)
			dir := filepath.Join(primaryWt, configDir, "runs")
			os.MkdirAll(dir, 0o755)
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("=== ITERATION %d VARIANT %d ===\n", iteration, i))
			sb.WriteString(fmt.Sprintf("Hypothesis: %s\n\n", hypotheses[i].hypothesis))
			if res.expResult.stdout != "" {
				sb.WriteString("=== STDOUT ===\n")
				sb.WriteString(res.expResult.stdout)
			}
			if res.expResult.stderr != "" {
				sb.WriteString("\n=== STDERR ===\n")
				sb.WriteString(res.expResult.stderr)
			}
			os.WriteFile(filepath.Join(dir, logFile), []byte(sb.String()), 0o644)
		}

		// Defer the kept row until after commit so it gets the correct hash.
		if row.Kept {
			keptRowCopy := row
			keptRow = &keptRowCopy
		} else {
			if err := AppendResult(primaryWt, row); err != nil {
				r.logger.Error("failed to append result", "variant", i, "error", err)
			}
		}
	}

	// Step 7: Update config state.
	if keptAny && bestIdx >= 0 {
		cfg.BestMetric = &bestMetric
		cfg.BestCommit = results[bestIdx].commitHash
		cfg.KeptCommit = results[bestIdx].commitHash
		cfg.KeptIterations++
		cfg.ConsecutiveFailures = 0

		// Constants mode: save best overrides and commit.
		if cfg.IsConstantsMode() {
			ov := &OverrideSet{Values: hypotheses[bestIdx].overrides}
			if saveErr := SaveOverrides(primaryWt, ov); saveErr != nil {
				r.logger.Error("failed to save overrides", "error", saveErr)
			}
			commitMsg := fmt.Sprintf("autoresearch #%d: %s", iteration, hypotheses[bestIdx].hypothesis)
			if commitErr := gitCommit(ctx, primaryWt, commitMsg); commitErr == nil {
				hash, _ := gitRevParse(ctx, primaryWt, "HEAD")
				cfg.BestCommit = hash
				cfg.KeptCommit = hash
				if keptRow != nil {
					keptRow.CommitHash = hash
				}
			}
		}
	} else {
		// All hypotheses failed: increment by the number of active slots
		// so stuck recovery thresholds trigger at the right pace.
		cfg.ConsecutiveFailures += activeSlots
	}

	// Append the deferred kept row (now has correct commit hash).
	if keptRow != nil {
		if err := AppendResult(primaryWt, *keptRow); err != nil {
			r.logger.Error("failed to append kept result", "error", err)
		}
	}

	cfg.TotalIterations = iteration
	if err := SaveConfig(primaryWt, cfg); err != nil {
		r.logger.Error("failed to save config", "error", err)
	}

	// Step 8: Sync all worktrees to the kept commit.
	syncRef := cfg.KeptCommit
	if syncRef != "" {
		for i, wt := range r.worktreeDirs {
			if i < activeSlots {
				gitResetHard(ctx, wt, syncRef)
			}
		}
	}

	// Step 9: Notify with batch summary.
	var notifyMsg strings.Builder
	notifyMsg.WriteString(fmt.Sprintf("Iteration #%d (%d parallel):", iteration, activeSlots))
	if keptAny && bestIdx >= 0 {
		notifyMsg.WriteString(fmt.Sprintf(" BEST=variant %d, %s=%.6f\n", bestIdx, cfg.MetricName, bestMetric))
	} else {
		notifyMsg.WriteString(" ALL DISCARDED\n")
	}
	for i, res := range results {
		status := "discarded"
		if i == bestIdx && keptAny {
			status = "KEPT"
		}
		if res.err != nil {
			status = "CRASHED"
		}
		notifyMsg.WriteString(fmt.Sprintf("  V%d: %s=%.6f (%s) — %s\n",
			i, cfg.MetricName, res.metric, status, hypotheses[i].hypothesis))
	}
	r.notify(ctx, notifyMsg.String())

	return nil
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

func gitCheckoutNewBranch(ctx context.Context, dir, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "checkout", "-b", branch)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout -b %s: %s", branch, string(out))
	}
	return nil
}

func gitCheckout(ctx context.Context, dir, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "checkout", branch)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout %s: %s", branch, string(out))
	}
	return nil
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
