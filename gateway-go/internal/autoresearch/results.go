package autoresearch

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ResultRow represents one experiment iteration.
type ResultRow struct {
	Iteration     int       `json:"iteration"`
	Timestamp     time.Time `json:"timestamp"`
	Hypothesis    string    `json:"hypothesis"`
	MetricValue   float64   `json:"metric_value"`
	Kept          bool      `json:"kept"`
	CommitHash    string    `json:"commit_hash"`
	DurationSec   int       `json:"duration_sec"`
	BestSoFar     float64   `json:"best_so_far"`       // running best at this point
	DeltaFromBest float64   `json:"delta_from_best"`   // difference from best (positive = improved)
	Variant       int       `json:"variant,omitempty"` // parallel variant index (0-based), omitted in sequential mode
}

// tsvHeader is the header line for the results TSV file.
// The variant column (10th) is always present in the header for forward
// compatibility but may be empty in sequential mode rows.
const tsvHeader = "iteration\ttimestamp\thypothesis\tmetric_value\tkept\tcommit_hash\tduration_sec\tbest_so_far\tdelta_from_best\tvariant"

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

	variantStr := ""
	if row.Variant > 0 {
		variantStr = fmt.Sprintf("%d", row.Variant)
	}

	line := fmt.Sprintf("%d\t%s\t%s\t%.6f\t%s\t%s\t%d\t%.6f\t%.6f\t%s\n",
		row.Iteration,
		row.Timestamp.UTC().Format(time.RFC3339),
		hypothesis,
		row.MetricValue,
		kept,
		commitHash,
		row.DurationSec,
		row.BestSoFar,
		row.DeltaFromBest,
		variantStr,
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

// ParseResults parses results.tsv into structured rows.
func ParseResults(workdir string) ([]ResultRow, error) {
	data, err := os.ReadFile(resultsPath(workdir))
	if err != nil {
		return nil, fmt.Errorf("read results: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) <= 1 {
		return nil, nil // header only
	}

	var rows []ResultRow
	for _, line := range lines[1:] { // skip header
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			continue
		}
		iter := 0
		fmt.Sscanf(fields[0], "%d", &iter)
		ts, _ := time.Parse(time.RFC3339, fields[1])
		var metric float64
		fmt.Sscanf(fields[3], "%f", &metric)
		var dur int
		fmt.Sscanf(fields[6], "%d", &dur)
		var bestSoFar, delta float64
		if len(fields) >= 9 {
			fmt.Sscanf(fields[7], "%f", &bestSoFar)
			fmt.Sscanf(fields[8], "%f", &delta)
		}

		var variant int
		if len(fields) >= 10 && fields[9] != "" {
			fmt.Sscanf(fields[9], "%d", &variant)
		}

		rows = append(rows, ResultRow{
			Iteration:     iter,
			Timestamp:     ts,
			Hypothesis:    fields[2],
			MetricValue:   metric,
			Kept:          fields[4] == "true",
			CommitHash:    fields[5],
			DurationSec:   dur,
			BestSoFar:     bestSoFar,
			DeltaFromBest: delta,
			Variant:       variant,
		})
	}
	return rows, nil
}

// TrendAnalysis returns a structured analysis of recent experiment trends.
func TrendAnalysis(rows []ResultRow, cfg *Config) string {
	if len(rows) == 0 {
		return "No experiment data yet."
	}

	var sb strings.Builder

	// Recent window: last N iterations (configurable via Params.TrendWindowSize).
	window := rows
	if len(window) > cfg.Params.TrendWindowSize {
		window = window[len(window)-cfg.Params.TrendWindowSize:]
	}

	// Count kept/discarded in recent window.
	recentKept := 0
	for _, r := range window {
		if r.Kept {
			recentKept++
		}
	}
	sb.WriteString(fmt.Sprintf("Recent %d iterations: %d kept, %d discarded (%.0f%% success rate)\n",
		len(window), recentKept, len(window)-recentKept,
		float64(recentKept)/float64(len(window))*100))

	// Improvement velocity: how fast is the metric improving?
	if len(rows) >= 2 {
		first := rows[0]
		last := rows[len(rows)-1]
		elapsed := last.Timestamp.Sub(first.Timestamp)
		if elapsed > 0 && first.MetricValue != 0 {
			totalChange := last.BestSoFar - first.MetricValue
			changePerHour := totalChange / elapsed.Hours()
			sb.WriteString(fmt.Sprintf("Improvement velocity: %.6f %s/hour\n",
				absFloat(changePerHour), cfg.MetricName))
		}
	}

	// Plateau detection: if last N+ iterations all discarded, flag it.
	consecutiveDiscarded := 0
	for i := len(rows) - 1; i >= 0; i-- {
		if !rows[i].Kept {
			consecutiveDiscarded++
		} else {
			break
		}
	}
	if consecutiveDiscarded >= cfg.Params.PlateauThreshold {
		sb.WriteString(fmt.Sprintf("⚠ PLATEAU: %d consecutive iterations discarded. Strategy change recommended.\n",
			consecutiveDiscarded))
	}

	// Best improvements: top 3 kept iterations by delta.
	var keptRows []ResultRow
	for _, r := range rows {
		if r.Kept && r.Iteration > 0 { // skip baseline
			keptRows = append(keptRows, r)
		}
	}
	if len(keptRows) > 0 {
		// Sort by absolute delta descending.
		sortByDelta(keptRows)
		sb.WriteString("Top improvements:\n")
		limit := 3
		if len(keptRows) < limit {
			limit = len(keptRows)
		}
		for i := 0; i < limit; i++ {
			r := keptRows[i]
			sb.WriteString(fmt.Sprintf("  #%d: %s (delta=%.6f) — %s\n",
				r.Iteration, fmtMetric(r.MetricValue), r.DeltaFromBest, r.Hypothesis))
		}
	}

	return sb.String()
}

func sortByDelta(rows []ResultRow) {
	// Simple insertion sort — small slice.
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && absFloat(rows[j].DeltaFromBest) > absFloat(rows[j-1].DeltaFromBest); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

func absFloat(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func fmtMetric(v float64) string {
	return fmt.Sprintf("%.6f", v)
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

	if cfg.TotalIterations > 0 {
		successRate := float64(cfg.KeptIterations) / float64(cfg.TotalIterations) * 100
		sb.WriteString(fmt.Sprintf("Kept: %d, Discarded: %d (%.1f%% success rate)\n",
			cfg.KeptIterations, cfg.TotalIterations-cfg.KeptIterations, successRate))
	} else {
		sb.WriteString(fmt.Sprintf("Kept: %d, Discarded: %d\n", cfg.KeptIterations, cfg.TotalIterations-cfg.KeptIterations))
	}

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

	// Append trend analysis if results data exists.
	rows, err := ParseResults(workdir)
	if err == nil && len(rows) > 0 {
		sb.WriteString("\n")
		sb.WriteString(TrendAnalysis(rows, cfg))
	}

	return sb.String()
}

// SaveExperimentOutput persists the stdout/stderr of an experiment run
// for later debugging and analysis.
func SaveExperimentOutput(workdir string, iteration int, stdout, stderr string) error {
	dir := filepath.Join(workdir, configDir, "runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create runs dir: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%04d.log", iteration))

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== ITERATION %d ===\n", iteration))
	sb.WriteString(fmt.Sprintf("Timestamp: %s\n\n", time.Now().UTC().Format(time.RFC3339)))
	if stdout != "" {
		sb.WriteString("=== STDOUT ===\n")
		sb.WriteString(stdout)
		if !strings.HasSuffix(stdout, "\n") {
			sb.WriteString("\n")
		}
	}
	if stderr != "" {
		sb.WriteString("\n=== STDERR ===\n")
		sb.WriteString(stderr)
		if !strings.HasSuffix(stderr, "\n") {
			sb.WriteString("\n")
		}
	}

	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// --- Run archival and comparison ---

// RunSummary is a summary of an archived autoresearch run.
type RunSummary struct {
	Tag             string   `json:"tag"`
	Path            string   `json:"path"`
	MetricName      string   `json:"metric_name"`
	Direction       string   `json:"direction"`
	TotalIterations int      `json:"total_iterations"`
	KeptIterations  int      `json:"kept_iterations"`
	BaselineMetric  *float64 `json:"baseline_metric,omitempty"`
	BestMetric      *float64 `json:"best_metric,omitempty"`
	ArchivedAt      string   `json:"archived_at"`
}

// archiveDir returns the path to the archive directory.
func archiveDir(workdir string) string {
	return filepath.Join(workdir, configDir, "archive")
}

// ArchiveRun copies the current experiment state to an archive directory.
// Returns the archive path.
func ArchiveRun(workdir, tag string) (string, error) {
	timestamp := time.Now().UTC().Format("20060102-150405")
	archiveName := tag + "-" + timestamp
	dst := filepath.Join(archiveDir(workdir), archiveName)

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return "", fmt.Errorf("create archive dir: %w", err)
	}

	// Copy config, results, chart, and overrides (if they exist).
	filesToCopy := []string{"config.json", "results.tsv", "chart.png", "overrides.json"}
	for _, f := range filesToCopy {
		src := filepath.Join(workdir, configDir, f)
		if _, err := os.Stat(src); err != nil {
			continue // skip missing files
		}
		if err := copyFile(src, filepath.Join(dst, f)); err != nil {
			return "", fmt.Errorf("copy %s: %w", f, err)
		}
	}

	return dst, nil
}

// ListRuns scans the archive directory and returns summaries of all runs.
func ListRuns(workdir string) ([]RunSummary, error) {
	dir := archiveDir(workdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read archive dir: %w", err)
	}

	var runs []RunSummary
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		cfgPath := filepath.Join(dir, entry.Name(), "config.json")
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			continue
		}
		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}

		info, _ := entry.Info()
		archivedAt := ""
		if info != nil {
			archivedAt = info.ModTime().UTC().Format(time.RFC3339)
		}

		runs = append(runs, RunSummary{
			Tag:             cfg.BranchTag,
			Path:            filepath.Join(dir, entry.Name()),
			MetricName:      cfg.MetricName,
			Direction:       cfg.MetricDirection,
			TotalIterations: cfg.TotalIterations,
			KeptIterations:  cfg.KeptIterations,
			BaselineMetric:  cfg.BaselineMetric,
			BestMetric:      cfg.BestMetric,
			ArchivedAt:      archivedAt,
		})
	}

	// Sort by archive time descending (newest first).
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].ArchivedAt > runs[j].ArchivedAt
	})

	return runs, nil
}

// CompareRuns compares two archived runs side-by-side.
func CompareRuns(workdir, runA, runB string) (string, error) {
	loadRun := func(path string) (*Config, error) {
		data, err := os.ReadFile(filepath.Join(path, "config.json"))
		if err != nil {
			return nil, err
		}
		var cfg Config
		return &cfg, json.Unmarshal(data, &cfg)
	}

	cfgA, err := loadRun(runA)
	if err != nil {
		return "", fmt.Errorf("load run A: %w", err)
	}
	cfgB, err := loadRun(runB)
	if err != nil {
		return "", fmt.Errorf("load run B: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("=== Run Comparison ===\n\n")
	sb.WriteString(fmt.Sprintf("%-20s | %-25s | %-25s\n", "", "Run A", "Run B"))
	sb.WriteString(strings.Repeat("-", 73) + "\n")
	sb.WriteString(fmt.Sprintf("%-20s | %-25s | %-25s\n", "Metric", cfgA.MetricName, cfgB.MetricName))
	sb.WriteString(fmt.Sprintf("%-20s | %-25s | %-25s\n", "Direction", cfgA.MetricDirection, cfgB.MetricDirection))
	sb.WriteString(fmt.Sprintf("%-20s | %-25d | %-25d\n", "Iterations", cfgA.TotalIterations, cfgB.TotalIterations))
	sb.WriteString(fmt.Sprintf("%-20s | %-25d | %-25d\n", "Kept", cfgA.KeptIterations, cfgB.KeptIterations))

	fmtMetricPtr := func(p *float64) string {
		if p == nil {
			return "N/A"
		}
		return fmt.Sprintf("%.6f", *p)
	}

	sb.WriteString(fmt.Sprintf("%-20s | %-25s | %-25s\n", "Baseline", fmtMetricPtr(cfgA.BaselineMetric), fmtMetricPtr(cfgB.BaselineMetric)))
	sb.WriteString(fmt.Sprintf("%-20s | %-25s | %-25s\n", "Best", fmtMetricPtr(cfgA.BestMetric), fmtMetricPtr(cfgB.BestMetric)))

	if cfgA.TotalIterations > 0 {
		sb.WriteString(fmt.Sprintf("%-20s | %-25.1f | ", "Success Rate %", float64(cfgA.KeptIterations)/float64(cfgA.TotalIterations)*100))
	} else {
		sb.WriteString(fmt.Sprintf("%-20s | %-25s | ", "Success Rate %", "N/A"))
	}
	if cfgB.TotalIterations > 0 {
		sb.WriteString(fmt.Sprintf("%-25.1f\n", float64(cfgB.KeptIterations)/float64(cfgB.TotalIterations)*100))
	} else {
		sb.WriteString("N/A\n")
	}

	return sb.String(), nil
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
