package autoresearch

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
		}
	}

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

// Runner manages the autoresearch experiment loop.
type Runner struct {
	mu          sync.Mutex
	cancel      context.CancelFunc
	running     bool
	workdir     string // original repo directory (state lives here)
	worktreeDir string // isolated worktree for experiments (empty when not running)
	client      *llm.Client
	model       string
	params      Params // snapshot of tunable params from config at Start() time
	notifier    Notifier
	logger      *slog.Logger
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

// SetNotifier sets the notifier for progress updates.
func (r *Runner) SetNotifier(n Notifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notifier = n
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

	// Create isolated worktree for the experiment.
	branchName := "autoresearch/" + cfg.BranchTag
	wtPath := filepath.Join(workdir, worktreeSubdir)

	// Clean up stale worktree from a previous run if it exists.
	if _, statErr := os.Stat(wtPath); statErr == nil {
		r.logger.Info("cleaning up stale worktree", "path", wtPath)
		_ = gitWorktreeRemove(context.Background(), workdir, wtPath)
		// If worktree remove didn't fully clean up, remove the directory.
		os.RemoveAll(wtPath)
	}

	// Create worktree with experiment branch.
	createBranch := !gitBranchExists(context.Background(), workdir, branchName)
	if err := gitWorktreeAdd(context.Background(), workdir, wtPath, branchName, createBranch); err != nil {
		return fmt.Errorf("create experiment worktree: %w", err)
	}
	if createBranch {
		r.logger.Info("created experiment branch in worktree", "branch", branchName, "worktree", wtPath)
	} else {
		r.logger.Info("using existing experiment branch in worktree", "branch", branchName, "worktree", wtPath)
	}

	// Symlink .autoresearch/ state from source dir into worktree so all
	// config/results reads and writes go to the original location.
	stateDir := filepath.Join(workdir, configDir)
	wtStateLink := filepath.Join(wtPath, configDir)
	if err := os.Symlink(stateDir, wtStateLink); err != nil {
		// Non-fatal: fall back to copying config if symlink fails.
		r.logger.Warn("symlink failed, copying config instead", "error", err)
		os.MkdirAll(filepath.Join(wtPath, configDir), 0o755)
		if data, readErr := os.ReadFile(configPath(workdir)); readErr == nil {
			os.WriteFile(configPath(wtPath), data, 0o644)
		}
	}

	// Record the kept commit as the current HEAD on the experiment branch.
	if cfg.KeptCommit == "" {
		headSHA, _ := gitRevParse(context.Background(), wtPath, "HEAD")
		cfg.KeptCommit = headSHA
		if err := SaveConfig(workdir, cfg); err != nil {
			r.logger.Warn("failed to save experiment config", "error", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.running = true
	r.workdir = workdir
	r.worktreeDir = wtPath
	r.model = cfg.Model
	r.params = cfg.Params

	go r.loop(ctx, wtPath)
	r.logger.Info("autoresearch started", "workdir", workdir, "worktree", wtPath,
		"metric", cfg.MetricName, "branch", branchName)
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

// cleanupWorktree removes the experiment worktree. Called from the loop's
// defer after the completion report has been sent.
func (r *Runner) cleanupWorktree() {
	r.mu.Lock()
	wtDir := r.worktreeDir
	srcDir := r.workdir
	r.worktreeDir = ""
	r.mu.Unlock()

	if wtDir == "" || srcDir == "" {
		return
	}

	// Remove the .autoresearch symlink first so git worktree remove
	// doesn't follow it and delete the source state.
	os.Remove(filepath.Join(wtDir, configDir))

	if err := gitWorktreeRemove(context.Background(), srcDir, wtDir); err != nil {
		r.logger.Error("failed to remove worktree", "path", wtDir, "error", err)
		// Last resort: remove the directory directly.
		os.RemoveAll(wtDir)
	}
	r.logger.Info("cleaned up experiment worktree", "path", wtDir)
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

		err := r.runOneIteration(ctx, workdir)
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
	model := r.model
	r.mu.Unlock()

	if model == "" {
		model = cfg.Params.DefaultModel
	}

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

	// Step 7: Run experiment with time budget.
	startTime := time.Now()
	expResult, runErr := r.runExperiment(ctx, workdir, cfg)
	duration := int(time.Since(startTime).Seconds())

	// Save experiment output for debugging/analysis.
	if expResult != nil {
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
func (r *Runner) runExperiment(ctx context.Context, workdir string, cfg *Config) (*experimentResult, error) {
	timeout := time.Duration(cfg.TimeBudgetSec) * time.Second
	grace := time.Duration(cfg.Params.GracePeriodSec) * time.Second
	expCtx, cancel := context.WithTimeout(ctx, timeout+grace)
	defer cancel()

	cmd := exec.CommandContext(expCtx, "bash", "-c", cfg.MetricCmd)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), fmt.Sprintf("TIME_BUDGET=%d", cfg.TimeBudgetSec))

	// Provide a persistent cache directory for expensive operations
	// (LLM inference, embeddings, etc.) that don't change across iterations.
	if cacheDir := cfg.ResolveCacheDir(workdir); cacheDir != "" {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			r.logger.Warn("failed to create cache dir", "path", cacheDir, "error", err)
		}
		cmd.Env = append(cmd.Env, "AUTORESEARCH_CACHE_DIR="+cacheDir)
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
	model := r.model
	r.mu.Unlock()

	if model == "" {
		model = cfg.Params.DefaultModel
	}

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

	// Step 4: Apply overrides temporarily. defer restore() ensures originals
	// are always restored, even on panic.
	restore, err := ApplyOverrides(workdir, cfg.Constants, overrides)
	if err != nil {
		return fmt.Errorf("apply overrides: %w", err)
	}
	defer restore()

	// Step 5: Run experiment with overridden files.
	startTime := time.Now()
	expResult, runErr := r.runExperiment(ctx, workdir, cfg)
	duration := int(time.Since(startTime).Seconds())

	if expResult != nil {
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
			cfg.BestMetric = &metricValue
			cfg.KeptIterations++
			cfg.ConsecutiveFailures = 0

			// Save best overrides to overrides.json.
			ov := &OverrideSet{Values: overrides}
			if saveErr := SaveOverrides(workdir, ov); saveErr != nil {
				r.logger.Error("failed to save overrides", "error", saveErr)
			}

			// Commit overrides.json (not modified source files).
			commitMsg := fmt.Sprintf("autoresearch #%d: %s", iteration, hypothesis)
			if commitErr := gitCommit(ctx, workdir, commitMsg); commitErr != nil {
				r.logger.Error("failed to commit overrides", "error", commitErr)
			} else {
				commitHash, _ := gitRevParse(ctx, workdir, "HEAD")
				row.CommitHash = commitHash
				cfg.BestCommit = commitHash
				cfg.KeptCommit = commitHash
			}

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
