package autoresearch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

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
