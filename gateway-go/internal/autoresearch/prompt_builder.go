package autoresearch

import (
	"fmt"
	"strconv"
	"strings"
)

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
