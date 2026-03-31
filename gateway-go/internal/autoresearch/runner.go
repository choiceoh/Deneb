package autoresearch

import (
	"context"
	"fmt"
	"log/slog"
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

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.running = true
	r.workdir = workdir
	r.model = cfg.Model

	go r.loop(ctx, workdir)
	r.logger.Info("autoresearch started", "workdir", workdir, "metric", cfg.MetricName)
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
	metricValue, runErr := r.runExperiment(ctx, workdir, cfg)
	duration := int(time.Since(startTime).Seconds())

	// Step 8: Evaluate and decide.
	row := ResultRow{
		Iteration:   iteration,
		Timestamp:   time.Now(),
		Hypothesis:  hypothesis,
		DurationSec: duration,
	}

	if runErr != nil {
		// Experiment crashed — revert.
		r.logger.Warn("experiment crashed", "error", runErr, "iteration", iteration)
		gitResetHard(ctx, workdir, cfg.KeptCommit)
		row.MetricValue = 0
		row.Kept = false
		row.CommitHash = ""
		cfg.ConsecutiveFailures++
		r.notify(ctx, fmt.Sprintf("Iteration #%d CRASHED: %s\nHypothesis: %s", iteration, runErr, hypothesis))
	} else {
		row.MetricValue = metricValue
		bestMetric := cfg.BestMetric
		if bestMetric == nil {
			// First successful iteration — always keep.
			row.Kept = true
		} else {
			row.Kept = cfg.IsBetter(metricValue, *bestMetric)
		}

		if row.Kept {
			row.CommitHash = commitHash
			cfg.BestMetric = &metricValue
			cfg.BestCommit = commitHash
			cfg.KeptCommit = commitHash
			cfg.KeptIterations++
			cfg.ConsecutiveFailures = 0
			r.notify(ctx, fmt.Sprintf("Iteration #%d KEPT: %s=%.6f\nHypothesis: %s",
				iteration, cfg.MetricName, metricValue, hypothesis))
		} else {
			// Revert to last kept commit.
			gitResetHard(ctx, workdir, cfg.KeptCommit)
			row.CommitHash = ""
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
func (r *Runner) buildPrompt(cfg *Config, files map[string]string, results string, iteration int) promptParts {
	var sys strings.Builder
	sys.WriteString("You are an autonomous research agent conducting iterative experiments.\n")
	sys.WriteString("Your goal: optimize " + cfg.MetricName + " (" + cfg.MetricDirection + ").\n\n")
	sys.WriteString("RULES:\n")
	sys.WriteString("- You may ONLY modify the target files listed below.\n")
	sys.WriteString("- Do NOT add new dependencies or imports that aren't already available.\n")
	sys.WriteString("- Make ONE focused change per iteration.\n")
	sys.WriteString("- Each experiment runs for exactly " + strconv.Itoa(cfg.TimeBudgetSec) + " seconds.\n")
	sys.WriteString("- Think carefully about what has worked and what hasn't before proposing changes.\n\n")

	if cfg.ConsecutiveFailures >= 5 {
		sys.WriteString("WARNING: 5+ consecutive failures. Try a fundamentally different approach.\n")
		sys.WriteString("Consider reverting to a simpler architecture or well-known configuration.\n\n")
	} else if cfg.ConsecutiveFailures >= 3 {
		sys.WriteString("NOTE: 3+ consecutive failures. Consider changing strategy.\n\n")
	}

	sys.WriteString("RESPONSE FORMAT:\n")
	sys.WriteString("First line: HYPOTHESIS: <one-line description of what you're changing and why>\n")
	sys.WriteString("Then for each file you want to modify:\n")
	sys.WriteString("--- FILE: <filename> ---\n")
	sys.WriteString("<complete file content>\n")
	sys.WriteString("--- END FILE ---\n")

	var usr strings.Builder
	usr.WriteString("=== CURRENT TARGET FILES ===\n\n")
	for name, content := range files {
		usr.WriteString("--- " + name + " ---\n")
		usr.WriteString(content)
		usr.WriteString("\n--- end " + name + " ---\n\n")
	}

	if results != "" {
		usr.WriteString("=== EXPERIMENT HISTORY ===\n")
		usr.WriteString(results)
		usr.WriteString("\n")
	}

	usr.WriteString(fmt.Sprintf("\n=== ITERATION %d ===\n", iteration))
	usr.WriteString("Propose ONE change to improve " + cfg.MetricName + ". ")
	if cfg.BestMetric != nil {
		usr.WriteString(fmt.Sprintf("Current best: %.6f. ", *cfg.BestMetric))
	}
	usr.WriteString("Output the hypothesis and complete modified file(s).")

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

// runExperiment executes the metric command with a time budget.
func (r *Runner) runExperiment(ctx context.Context, workdir string, cfg *Config) (float64, error) {
	timeout := time.Duration(cfg.TimeBudgetSec) * time.Second
	expCtx, cancel := context.WithTimeout(ctx, timeout+30*time.Second) // grace period
	defer cancel()

	cmd := exec.CommandContext(expCtx, "bash", "-c", cfg.MetricCmd)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), fmt.Sprintf("TIME_BUDGET=%d", cfg.TimeBudgetSec))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("experiment command failed: %w\nOutput: %s", err, string(output))
	}

	// Parse metric from last non-empty line of output.
	metric, err := extractMetric(string(output))
	if err != nil {
		return 0, fmt.Errorf("metric extraction failed: %w\nOutput: %s", err, string(output))
	}

	return metric, nil
}

// extractMetric finds a floating-point number in the last non-empty line of output.
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
