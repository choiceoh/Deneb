package autoresearch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

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

	metric, err := extractMetricWithMode(string(output), cfg.MetricPattern, cfg.MetricExtractMode)
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

	// Step 4b: Check for duplicate hypothesis.
	if skip, err := r.checkAndRecordDedup(workdir, HashFileChanges(changes), iteration, cfg); err != nil {
		return err
	} else if skip {
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

	// Step 7: Run experiment with cache.
	exp := r.runExperimentWithCache(ctx, workdir, cfg, iteration, fileHash)

	// Step 8: Evaluate and decide.
	eval := evaluateExperiment(cfg, iteration, hypothesis, exp.result, exp.err, exp.duration)
	cfg.TotalIterations = iteration

	if exp.err != nil {
		r.logger.Warn("experiment crashed", "error", exp.err, "iteration", iteration)
		gitResetHard(ctx, workdir, cfg.KeptCommit)
		cfg.ConsecutiveFailures++
		r.notify(ctx, fmt.Sprintf("Iteration #%d CRASHED: %s\nHypothesis: %s", iteration, exp.err, hypothesis))
	} else if eval.row.Kept {
		eval.row.CommitHash = commitHash
		markKept(cfg, eval.metricValue, commitHash)
		r.notify(ctx, fmt.Sprintf("Iteration #%d KEPT: %s=%.6f%s\nHypothesis: %s",
			iteration, cfg.MetricName, eval.metricValue, improvementFromBaseline(cfg, eval.metricValue), hypothesis))
	} else {
		gitResetHard(ctx, workdir, cfg.KeptCommit)
		cfg.ConsecutiveFailures++
		r.notify(ctx, fmt.Sprintf("Iteration #%d DISCARDED: %s=%.6f (best=%.6f)\nHypothesis: %s",
			iteration, cfg.MetricName, eval.metricValue, *cfg.BestMetric, hypothesis))
	}

	r.persistIterationResult(workdir, cfg, eval.row)
	return nil
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

	// Step 3b: Check for duplicate hypothesis.
	if skip, err := r.checkAndRecordDedup(workdir, HashOverrideChanges(overrides), iteration, cfg); err != nil {
		return err
	} else if skip {
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

	// Step 5: Run experiment with cache.
	baseHash, _ := contentHash(workdir, cfg.TargetFiles)
	cacheHash := overrideHash(baseHash, overrides)
	exp := r.runExperimentWithCache(ctx, workdir, cfg, iteration, cacheHash)

	// Step 6: Restore originals BEFORE evaluating (files must be clean for git).
	restore()

	// Step 7: Evaluate and decide.
	eval := evaluateExperiment(cfg, iteration, hypothesis, exp.result, exp.err, exp.duration)
	cfg.TotalIterations = iteration

	if exp.err != nil {
		r.logger.Warn("experiment crashed", "error", exp.err, "iteration", iteration)
		cfg.ConsecutiveFailures++
		r.notify(ctx, fmt.Sprintf("Iteration #%d CRASHED: %s\nHypothesis: %s", iteration, exp.err, hypothesis))
	} else if eval.row.Kept {
		// Save best overrides and commit. If commit fails, flip to discarded.
		ov := &OverrideSet{Values: overrides}
		if saveErr := SaveOverrides(workdir, ov); saveErr != nil {
			r.logger.Error("failed to save overrides", "error", saveErr)
		}

		commitMsg := fmt.Sprintf("autoresearch #%d: %s", iteration, hypothesis)
		if commitErr := gitCommit(ctx, workdir, commitMsg); commitErr != nil {
			r.logger.Error("failed to commit overrides, treating as discarded", "error", commitErr)
			eval.row.Kept = false
			eval.row.BestSoFar = eval.currentBest
			eval.row.DeltaFromBest = 0
			cfg.ConsecutiveFailures++
			r.notify(ctx, fmt.Sprintf("Iteration #%d DISCARD (commit failed): %s=%.6f\nHypothesis: %s",
				iteration, cfg.MetricName, eval.metricValue, hypothesis))
		} else {
			commitHash, _ := gitRevParse(ctx, workdir, "HEAD")
			eval.row.CommitHash = commitHash
			markKept(cfg, eval.metricValue, commitHash)
			r.notify(ctx, fmt.Sprintf("Iteration #%d KEPT: %s=%.6f%s\nHypothesis: %s\nOverrides: %v",
				iteration, cfg.MetricName, eval.metricValue, improvementFromBaseline(cfg, eval.metricValue), hypothesis, overrides))
		}
	} else {
		cfg.ConsecutiveFailures++
		r.notify(ctx, fmt.Sprintf("Iteration #%d DISCARDED: %s=%.6f (best=%.6f)\nHypothesis: %s",
			iteration, cfg.MetricName, eval.metricValue, *cfg.BestMetric, hypothesis))
	}

	r.persistIterationResult(workdir, cfg, eval.row)
	return nil
}

// --- Shared iteration helpers ---

// checkAndRecordDedup checks if the given change hash duplicates a previous
// iteration. Returns true if duplicate (config already saved). On non-duplicate,
// records the hash for future checks.
func (r *Runner) checkAndRecordDedup(workdir string, hash string, iteration int, cfg *Config) (skip bool, err error) {
	tracker := LoadHypothesisTracker(workdir)
	if dupIter, isDup := tracker.IsDuplicate(hash); isDup {
		r.logger.Warn("duplicate hypothesis detected, skipping",
			"iteration", iteration, "duplicate_of", dupIter)
		r.dedupHint = fmt.Sprintf("Your proposal was identical to iteration #%d. Try something substantially different.", dupIter)
		cfg.ConsecutiveFailures++
		cfg.TotalIterations = iteration
		if err := SaveConfig(workdir, cfg); err != nil {
			return true, err
		}
		return true, nil
	}
	tracker.Record(hash, iteration)
	if err := SaveHypothesisTracker(workdir, tracker); err != nil {
		r.logger.Warn("failed to save dedup hashes", "error", err)
	}
	r.dedupHint = ""
	return false, nil
}

// cachedExperimentResult bundles experiment output with cache/timing metadata.
type cachedExperimentResult struct {
	result   *experimentResult
	duration int
	cacheHit bool
	err      error
}

// runExperimentWithCache runs the metric command, checking and populating cache.
// Also persists experiment stdout/stderr for debugging.
func (r *Runner) runExperimentWithCache(ctx context.Context, workdir string, cfg *Config, iteration int, cacheHash string) cachedExperimentResult {
	if cfg.CacheEnabled && cacheHash != "" {
		cacheDir := cfg.ResolveCacheDir(workdir)
		if metric, ok := loadCachedMetric(cacheDir, cacheHash, cfg.MetricCmd); ok {
			r.logger.Info("cache hit, skipping experiment", "hash", cacheHash, "metric", metric)
			return cachedExperimentResult{
				result:   &experimentResult{metric: metric, stdout: fmt.Sprintf("cached: %.6f", metric)},
				cacheHit: true,
			}
		}
	}

	startTime := time.Now()
	expResult, runErr := r.runExperiment(ctx, workdir, cfg, iteration)
	duration := int(time.Since(startTime).Seconds())

	if runErr == nil && cfg.CacheEnabled && cacheHash != "" {
		cacheDir := cfg.ResolveCacheDir(workdir)
		if saveErr := saveCachedMetric(cacheDir, cacheHash, cfg.MetricCmd, expResult.metric); saveErr != nil {
			r.logger.Warn("failed to cache metric", "error", saveErr)
		}
	}

	if expResult != nil {
		if saveErr := SaveExperimentOutput(workdir, iteration, expResult.stdout, expResult.stderr); saveErr != nil {
			r.logger.Error("failed to save experiment output", "error", saveErr)
		}
	}

	return cachedExperimentResult{result: expResult, duration: duration, err: runErr}
}

// iterationEval holds the pure evaluation result (no side effects applied).
type iterationEval struct {
	row         ResultRow
	currentBest float64
	metricValue float64
}

// evaluateExperiment determines kept/discarded and builds the ResultRow.
// Pure function — does not modify cfg, persist, or notify.
func evaluateExperiment(cfg *Config, iteration int, hypothesis string, expResult *experimentResult, runErr error, duration int) iterationEval {
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
		row.Kept = false
		row.BestSoFar = currentBest
		return iterationEval{row: row, currentBest: currentBest}
	}

	metricValue := expResult.metric
	row.MetricValue = metricValue
	if cfg.BestMetric == nil {
		row.Kept = true
	} else {
		row.Kept = cfg.IsBetter(metricValue, *cfg.BestMetric)
		row.DeltaFromBest = metricValue - *cfg.BestMetric
	}

	if row.Kept {
		row.BestSoFar = metricValue
	} else {
		row.BestSoFar = currentBest
	}

	return iterationEval{row: row, currentBest: currentBest, metricValue: metricValue}
}

// markKept updates cfg fields for a kept iteration.
func markKept(cfg *Config, metricValue float64, commitHash string) {
	cfg.BestMetric = &metricValue
	cfg.BestCommit = commitHash
	cfg.KeptCommit = commitHash
	cfg.KeptIterations++
	cfg.ConsecutiveFailures = 0
}

// improvementFromBaseline returns a formatted improvement percentage, or "".
func improvementFromBaseline(cfg *Config, metricValue float64) string {
	if cfg.BaselineMetric == nil || *cfg.BaselineMetric == 0 {
		return ""
	}
	improvement := (*cfg.BaselineMetric - metricValue) / *cfg.BaselineMetric * 100
	if cfg.MetricDirection == "maximize" {
		improvement = (metricValue - *cfg.BaselineMetric) / *cfg.BaselineMetric * 100
	}
	return fmt.Sprintf(" (%.2f%% from baseline)", improvement)
}

// persistIterationResult saves the result row and updated config.
func (r *Runner) persistIterationResult(workdir string, cfg *Config, row ResultRow) {
	if err := AppendResult(workdir, row); err != nil {
		r.logger.Error("failed to append result", "error", err)
	}
	if err := SaveConfig(workdir, cfg); err != nil {
		r.logger.Error("failed to save config", "error", err)
	}
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

	// Parse metric from stdout using configured extraction mode.
	metric, mErr := extractMetricWithMode(stdout, cfg.MetricPattern, cfg.MetricExtractMode)
	if mErr != nil {
		return &experimentResult{stdout: stdout, stderr: stderr},
			fmt.Errorf("metric extraction failed: %w", mErr)
	}

	return &experimentResult{metric: metric, stdout: stdout, stderr: stderr}, nil
}
