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
}

// RunBaseline executes the metric command once to establish a baseline value.
// Called during init before any modifications are made.
func RunBaseline(ctx context.Context, workdir string, cfg *Config) (float64, error) {
	timeout := time.Duration(cfg.TimeBudgetSec) * time.Second
	expCtx, cancel := context.WithTimeout(ctx, timeout+30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(expCtx, "bash", "-c", cfg.MetricCmd)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), fmt.Sprintf("TIME_BUDGET=%d", cfg.TimeBudgetSec))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("baseline command failed: %w\nOutput: %s", err, string(output))
	}

	metric, err := extractMetricSmart(string(output), cfg.MetricPattern)
	if err != nil {
		return 0, fmt.Errorf("baseline metric extraction failed: %w", err)
	}

	// Save baseline output.
	_ = SaveExperimentOutput(workdir, 0, string(output), "")

	// Record baseline as iteration 0 in results.
	_ = AppendResult(workdir, ResultRow{
		Iteration:   0,
		Timestamp:   time.Now(),
		Hypothesis:  "baseline",
		MetricValue: metric,
		Kept:        true,
		DurationSec: int(timeout.Seconds()),
		BestSoFar:   metric,
	})

	return metric, nil
}

// Runner manages the autoresearch experiment loop.
type Runner struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	running  bool
	workdir  string
	client   *llm.Client
	model    string
	notifier Notifier
	logger   *slog.Logger
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

// Workdir returns the current experiment workspace.
func (r *Runner) Workdir() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.workdir
}

// Start launches the autonomous experiment loop in a background goroutine.
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

	// Create isolated experiment branch if not already on one.
	branchName := "autoresearch/" + cfg.BranchTag
	currentBranch, _ := gitCurrentBranch(context.Background(), workdir)
	if currentBranch != branchName {
		// Record original branch for potential return.
		cfg.OriginalBranch = currentBranch
		if err := SaveConfig(workdir, cfg); err != nil {
			return fmt.Errorf("save config with original branch: %w", err)
		}

		// Check if the experiment branch already exists.
		if gitBranchExists(context.Background(), workdir, branchName) {
			// Switch to existing experiment branch.
			if err := gitCheckout(context.Background(), workdir, branchName); err != nil {
				return fmt.Errorf("switch to experiment branch: %w", err)
			}
			r.logger.Info("switched to existing experiment branch", "branch", branchName)
		} else {
			// Create new experiment branch from current HEAD.
			if err := gitCheckoutNewBranch(context.Background(), workdir, branchName); err != nil {
				return fmt.Errorf("create experiment branch: %w", err)
			}
			r.logger.Info("created experiment branch", "branch", branchName)
		}
	}

	// Record the kept commit as the current HEAD on the experiment branch.
	if cfg.KeptCommit == "" {
		headSHA, _ := gitRevParse(context.Background(), workdir, "HEAD")
		cfg.KeptCommit = headSHA
		_ = SaveConfig(workdir, cfg)
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.running = true
	r.workdir = workdir
	r.model = cfg.Model

	go r.loop(ctx, workdir)
	r.logger.Info("autoresearch started", "workdir", workdir, "metric", cfg.MetricName,
		"branch", branchName)
	return nil
}

// Stop halts the running experiment loop.
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

// loop is the main experiment loop. It runs until ctx is cancelled.
func (r *Runner) loop(ctx context.Context, workdir string) {
	defer func() {
		if rv := recover(); rv != nil {
			r.logger.Error("autoresearch panic recovered", "panic", rv)
		}
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			r.notify(context.Background(), "Autoresearch stopped.")
			return
		default:
		}

		err := r.runOneIteration(ctx, workdir)
		if err != nil {
			if ctx.Err() != nil {
				return // context cancelled, exit cleanly
			}
			r.logger.Error("autoresearch iteration failed", "error", err)
			// Brief pause before retrying after error.
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
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

	// Step 3: Ask LLM for a hypothesis and code modification.
	prompt := r.buildPrompt(cfg, fileContents, resultsHistory, iteration)

	r.mu.Lock()
	client := r.client
	model := r.model
	r.mu.Unlock()

	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	llmResp, err := client.Complete(ctx, llm.ChatRequest{
		Model:     model,
		System:    llm.SystemString(prompt.system),
		Messages:  []llm.Message{llm.NewTextMessage("user", prompt.user)},
		MaxTokens: 8192,
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
	if cfg.ConsecutiveFailures >= 8 {
		sys.WriteString("=== CRITICAL: 8+ CONSECUTIVE FAILURES ===\n")
		sys.WriteString("You are deeply stuck. Drastic measures required:\n")
		sys.WriteString("- Revert to the SIMPLEST possible configuration that is known to work.\n")
		sys.WriteString("- Discard all complex hypotheses. Start from first principles.\n")
		sys.WriteString("- Consider whether the metric command or evaluation setup has issues.\n")
		sys.WriteString("- Try a change that is the OPPOSITE of your recent failed attempts.\n\n")
	} else if cfg.ConsecutiveFailures >= 5 {
		sys.WriteString("=== WARNING: 5+ CONSECUTIVE FAILURES ===\n")
		sys.WriteString("Your current strategy is not working. Required changes:\n")
		sys.WriteString("- Abandon the current approach entirely and try a fundamentally different direction.\n")
		sys.WriteString("- Review the KEPT experiments in history — what made them succeed? Return to those principles.\n")
		sys.WriteString("- Consider reverting to a well-known, simpler architecture or configuration.\n")
		sys.WriteString("- The definition of insanity is trying the same thing and expecting different results.\n\n")
	} else if cfg.ConsecutiveFailures >= 3 {
		sys.WriteString("=== NOTE: 3+ CONSECUTIVE FAILURES ===\n")
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
		start := len(rows) - 5
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

	if iteration <= 3 {
		usr.WriteString("You are in the EARLY phase. Explore broadly — try different approaches to understand the landscape.\n")
	} else if iteration <= 15 {
		usr.WriteString("You are in the EXPLORATION phase. Balance between trying new ideas and refining what works.\n")
	} else if iteration <= 30 {
		usr.WriteString("You are in the EXPLOITATION phase. Focus on refining the approaches that have produced the best results.\n")
	} else {
		usr.WriteString("You are in the FINE-TUNING phase. Make small, precise adjustments. The easy gains are likely behind you.\n")
	}

	usr.WriteString("\nPropose ONE atomic change. Explain your reasoning in the HYPOTHESIS line. Output the complete modified file(s).")

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
	expCtx, cancel := context.WithTimeout(ctx, timeout+30*time.Second) // grace period
	defer cancel()

	cmd := exec.CommandContext(expCtx, "bash", "-c", cfg.MetricCmd)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), fmt.Sprintf("TIME_BUDGET=%d", cfg.TimeBudgetSec))

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
