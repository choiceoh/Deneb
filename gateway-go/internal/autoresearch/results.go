package autoresearch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ResultRow represents one experiment iteration.
type ResultRow struct {
	Iteration   int       `json:"iteration"`
	Timestamp   time.Time `json:"timestamp"`
	Hypothesis  string    `json:"hypothesis"`
	MetricValue float64   `json:"metric_value"`
	Kept        bool      `json:"kept"`
	CommitHash  string    `json:"commit_hash"`
	DurationSec int       `json:"duration_sec"`
}

// tsvHeader is the header line for the results TSV file.
const tsvHeader = "iteration\ttimestamp\thypothesis\tmetric_value\tkept\tcommit_hash\tduration_sec"

// resultsPath returns the path to results.tsv inside the workspace.
func resultsPath(workdir string) string {
	return filepath.Join(workdir, configDir, "results.tsv")
}

// AppendResult appends a single result row to the results.tsv file.
func AppendResult(workdir string, row ResultRow) error {
	path := resultsPath(workdir)

	// Create file with header if it doesn't exist.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		dir := filepath.Join(workdir, configDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create results dir: %w", err)
		}
		if err := os.WriteFile(path, []byte(tsvHeader+"\n"), 0o644); err != nil {
			return fmt.Errorf("write results header: %w", err)
		}
	}

	kept := "false"
	if row.Kept {
		kept = "true"
	}
	commitHash := row.CommitHash
	if commitHash == "" {
		commitHash = "-"
	}
	// Sanitize hypothesis: replace tabs/newlines with spaces.
	hypothesis := strings.ReplaceAll(row.Hypothesis, "\t", " ")
	hypothesis = strings.ReplaceAll(hypothesis, "\n", " ")

	line := fmt.Sprintf("%d\t%s\t%s\t%.6f\t%s\t%s\t%d\n",
		row.Iteration,
		row.Timestamp.UTC().Format(time.RFC3339),
		hypothesis,
		row.MetricValue,
		kept,
		commitHash,
		row.DurationSec,
	)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open results file: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("append result: %w", err)
	}
	return nil
}

// ReadResults reads the raw content of results.tsv.
func ReadResults(workdir string) (string, error) {
	data, err := os.ReadFile(resultsPath(workdir))
	if err != nil {
		return "", fmt.Errorf("read results: %w", err)
	}
	return string(data), nil
}

// Summary returns a human-readable summary of the experiment progress.
func Summary(workdir string, cfg *Config) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== Autoresearch: %s ===\n", cfg.MetricName))
	sb.WriteString(fmt.Sprintf("Direction: %s\n", cfg.MetricDirection))
	sb.WriteString(fmt.Sprintf("Target files: %s\n", strings.Join(cfg.TargetFiles, ", ")))
	sb.WriteString(fmt.Sprintf("Time budget: %ds/experiment\n", cfg.TimeBudgetSec))
	sb.WriteString(fmt.Sprintf("Branch: autoresearch/%s\n", cfg.BranchTag))
	sb.WriteString(fmt.Sprintf("Total iterations: %d\n", cfg.TotalIterations))
	sb.WriteString(fmt.Sprintf("Kept: %d, Discarded: %d\n", cfg.KeptIterations, cfg.TotalIterations-cfg.KeptIterations))
	if cfg.BaselineMetric != nil {
		sb.WriteString(fmt.Sprintf("Baseline %s: %.6f\n", cfg.MetricName, *cfg.BaselineMetric))
	}
	if cfg.BestMetric != nil {
		sb.WriteString(fmt.Sprintf("Best %s: %.6f", cfg.MetricName, *cfg.BestMetric))
		if cfg.BaselineMetric != nil && *cfg.BaselineMetric != 0 {
			improvement := (*cfg.BaselineMetric - *cfg.BestMetric) / *cfg.BaselineMetric * 100
			if cfg.MetricDirection == "maximize" {
				improvement = (*cfg.BestMetric - *cfg.BaselineMetric) / *cfg.BaselineMetric * 100
			}
			sb.WriteString(fmt.Sprintf(" (%.2f%% improvement)", improvement))
		}
		sb.WriteString("\n")
	}
	if cfg.ConsecutiveFailures > 0 {
		sb.WriteString(fmt.Sprintf("Consecutive failures: %d\n", cfg.ConsecutiveFailures))
	}
	return sb.String()
}
