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

// --- Metric extraction ---

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

// --- LLM response parsing ---

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
