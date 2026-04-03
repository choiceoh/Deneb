package autoresearch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunnerStartStopLifecycle(t *testing.T) {
	dir := t.TempDir()

	// Create a minimal config.
	cfg := &Config{
		TargetFiles:     []string{"test.txt"},
		MetricCmd:       "echo 1.0",
		MetricName:      "score",
		MetricDirection: "maximize",
		TimeBudgetSec:   5,
		BranchTag:       "test",
	}
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Create target file.
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(nil)

	// Can't start without LLM client.
	if err := runner.Start(dir); err == nil {
		t.Error("expected error without LLM client")
	}

	if runner.IsRunning() {
		t.Error("should not be running")
	}
}

func TestRunnerDoubleStart(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		TargetFiles:     []string{"test.txt"},
		MetricCmd:       "echo 1.0",
		MetricName:      "score",
		MetricDirection: "maximize",
		TimeBudgetSec:   5,
		BranchTag:       "test",
	}
	SaveConfig(dir, cfg)

	runner := NewRunner(nil)
	// Simulate running state.
	runner.mu.Lock()
	runner.running = true
	runner.mu.Unlock()

	err := runner.Start(dir)
	if err == nil {
		t.Error("expected error on double start")
	}
}

func TestBuildPrompt(t *testing.T) {
	cfg := &Config{
		MetricName:          "val_bpb",
		MetricDirection:     "minimize",
		TimeBudgetSec:       300,
		ConsecutiveFailures: 0,
	}
	cfg.Params.applyDefaults()
	files := map[string]string{
		"train.py": "import torch\nlr = 0.001",
	}

	runner := NewRunner(nil)
	p := runner.buildPrompt(cfg, files, "", 1)

	if !contains(p.system, "val_bpb") {
		t.Error("system prompt should mention metric name")
	}
	if !contains(p.system, "minimize") {
		t.Error("system prompt should mention direction")
	}
	if !contains(p.user, "train.py") {
		t.Error("user prompt should include file name")
	}
	if !contains(p.user, "lr = 0.001") {
		t.Error("user prompt should include file content")
	}
}

func TestBuildPromptStuckRecovery(t *testing.T) {
	tests := []struct {
		name     string
		failures int
		want     string
	}{
		{"3+ failures", 3, "CONSECUTIVE FAILURES"},
		{"5+ failures", 5, "WARNING"},
		{"8+ failures", 8, "CRITICAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				MetricName:          "loss",
				MetricDirection:     "minimize",
				TimeBudgetSec:       60,
				ConsecutiveFailures: tt.failures,
			}
			cfg.Params.applyDefaults()
			runner := NewRunner(nil)
			p := runner.buildPrompt(cfg, nil, "", 10)
			if !contains(p.system, tt.want) {
				t.Errorf("expected %q in system prompt for %d failures", tt.want, tt.failures)
			}
		})
	}
}

func TestBuildPromptPhases(t *testing.T) {
	cfg := &Config{
		MetricName:      "loss",
		MetricDirection: "minimize",
		TimeBudgetSec:   60,
	}
	cfg.Params.applyDefaults()
	runner := NewRunner(nil)

	tests := []struct {
		iteration int
		want      string
	}{
		{1, "EARLY"},
		{5, "EXPLORATION"},
		{20, "EXPLOITATION"},
		{35, "FINE-TUNING"},
	}
	for _, tt := range tests {
		p := runner.buildPrompt(cfg, nil, "", tt.iteration)
		if !contains(p.user, tt.want) {
			t.Errorf("iteration %d: expected phase %q in user prompt", tt.iteration, tt.want)
		}
	}
}

func TestBuildPromptHardConstraints(t *testing.T) {
	cfg := &Config{
		MetricName:      "val_bpb",
		MetricDirection: "minimize",
		TimeBudgetSec:   300,
	}
	cfg.Params.applyDefaults()
	runner := NewRunner(nil)
	p := runner.buildPrompt(cfg, nil, "", 1)

	// Check for key program.md-level constraints.
	constraints := []string{
		"HARD CONSTRAINTS",
		"ONLY modify the target files",
		"Do NOT add new dependencies",
		"Do NOT hardcode",
		"STRATEGY GUIDANCE",
		"EXPLORATION vs EXPLOITATION",
		"CHANGE GRANULARITY",
		"LEARNING FROM HISTORY",
		"TIME BUDGET AWARENESS",
		"RESPONSE FORMAT",
	}
	for _, c := range constraints {
		if !contains(p.system, c) {
			t.Errorf("system prompt missing constraint: %q", c)
		}
	}
}

func TestBuildPromptKeptAndFailedHistory(t *testing.T) {
	dir := t.TempDir()
	// Write some results to test kept/failed sections.
	rows := []ResultRow{
		{Iteration: 0, Timestamp: time.Now(), Hypothesis: "baseline", MetricValue: 1.0, Kept: true, BestSoFar: 1.0},
		{Iteration: 1, Timestamp: time.Now(), Hypothesis: "double lr", MetricValue: 0.95, Kept: true, CommitHash: "abc", BestSoFar: 0.95, DeltaFromBest: -0.05},
		{Iteration: 2, Timestamp: time.Now(), Hypothesis: "bad change", MetricValue: 1.2, Kept: false, BestSoFar: 0.95, DeltaFromBest: 0.25},
	}
	for _, r := range rows {
		AppendResult(dir, r)
	}

	cfg := &Config{
		MetricName:      "loss",
		MetricDirection: "minimize",
		TimeBudgetSec:   60,
	}
	cfg.Params.applyDefaults()
	runner := NewRunner(nil)
	runner.workdir = dir
	p := runner.buildPrompt(cfg, nil, "", 3)

	if !contains(p.user, "SUCCESSFUL CHANGES") {
		t.Error("should include successful changes section")
	}
	if !contains(p.user, "double lr") {
		t.Error("should list kept hypothesis")
	}
	if !contains(p.user, "RECENT FAILURES") {
		t.Error("should include recent failures section")
	}
	if !contains(p.user, "bad change") {
		t.Error("should list failed hypothesis")
	}
}

// --- Helpers ---

func splitNonEmpty(s string) []string {
	var lines []string
	for _, l := range splitLines(s) {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func splitLines(s string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
