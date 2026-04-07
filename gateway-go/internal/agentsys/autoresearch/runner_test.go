package autoresearch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid minimize",
			cfg: Config{
				TargetFiles:     []string{"train.py"},
				MetricCmd:       "python train.py",
				MetricName:      "loss",
				MetricDirection: "minimize",
				BranchTag:       "test1",
				TimeBudgetSec:   60,
			},
		},
		{
			name: "valid maximize",
			cfg: Config{
				TargetFiles:     []string{"main.go"},
				MetricCmd:       "go test -bench .",
				MetricName:      "ops/sec",
				MetricDirection: "maximize",
				BranchTag:       "bench",
				TimeBudgetSec:   120,
			},
		},
		{
			name:    "missing target files",
			cfg:     Config{MetricCmd: "echo 1", MetricName: "m", MetricDirection: "minimize", BranchTag: "t"},
			wantErr: true,
		},
		{
			name:    "missing metric cmd",
			cfg:     Config{TargetFiles: []string{"a.py"}, MetricName: "m", MetricDirection: "minimize", BranchTag: "t"},
			wantErr: true,
		},
		{
			name:    "invalid direction",
			cfg:     Config{TargetFiles: []string{"a.py"}, MetricCmd: "echo 1", MetricName: "m", MetricDirection: "middle", BranchTag: "t"},
			wantErr: true,
		},
		{
			name: "default time budget",
			cfg: Config{
				TargetFiles:     []string{"a.py"},
				MetricCmd:       "echo 1",
				MetricName:      "m",
				MetricDirection: "minimize",
				BranchTag:       "t",
				TimeBudgetSec:   0, // should default to 3600
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && tt.cfg.TimeBudgetSec <= 0 {
				t.Error("expected TimeBudgetSec to be set to default")
			}
		})
	}
}

func TestIsBetter(t *testing.T) {
	minCfg := &Config{MetricDirection: "minimize"}
	maxCfg := &Config{MetricDirection: "maximize"}

	if !minCfg.IsBetter(0.9, 1.0) {
		t.Error("minimize: 0.9 should be better than 1.0")
	}
	if minCfg.IsBetter(1.1, 1.0) {
		t.Error("minimize: 1.1 should not be better than 1.0")
	}
	if !maxCfg.IsBetter(1.1, 1.0) {
		t.Error("maximize: 1.1 should be better than 1.0")
	}
	if maxCfg.IsBetter(0.9, 1.0) {
		t.Error("maximize: 0.9 should not be better than 1.0")
	}
}

func TestConfigSaveLoad(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		TargetFiles:     []string{"train.py", "model.py"},
		MetricCmd:       "python train.py",
		MetricName:      "val_loss",
		MetricDirection: "minimize",
		TimeBudgetSec:   300,
		BranchTag:       "test",
	}

	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	loaded := testutil.Must(LoadConfig(dir))

	if loaded.MetricName != cfg.MetricName {
		t.Errorf("MetricName = %q, want %q", loaded.MetricName, cfg.MetricName)
	}
	if len(loaded.TargetFiles) != 2 {
		t.Errorf("TargetFiles len = %d, want 2", len(loaded.TargetFiles))
	}
}

func TestLoadConfigMissing(t *testing.T) {
	_, err := LoadConfig(t.TempDir())
	if err == nil {
		t.Error("expected error for missing config")
	}
}

func TestAppendAndReadResults(t *testing.T) {
	dir := t.TempDir()

	row1 := ResultRow{
		Iteration:     1,
		Timestamp:     time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
		Hypothesis:    "increase lr",
		MetricValue:   1.087,
		Kept:          true,
		CommitHash:    "abc1234",
		DurationSec:   300,
		BestSoFar:     1.087,
		DeltaFromBest: 0,
	}
	row2 := ResultRow{
		Iteration:     2,
		Timestamp:     time.Date(2026, 3, 31, 0, 5, 0, 0, time.UTC),
		Hypothesis:    "add layer norm",
		MetricValue:   1.095,
		Kept:          false,
		CommitHash:    "",
		DurationSec:   300,
		BestSoFar:     1.087,
		DeltaFromBest: 0.008,
	}

	if err := AppendResult(dir, row1); err != nil {
		t.Fatalf("AppendResult row1: %v", err)
	}
	if err := AppendResult(dir, row2); err != nil {
		t.Fatalf("AppendResult row2: %v", err)
	}

	content := testutil.Must(ReadResults(dir))

	// Should have header + 2 data rows.
	lines := splitNonEmpty(content)
	if len(lines) != 3 {
		t.Errorf("expected 3 lines (header + 2 rows), got %d:\n%s", len(lines), content)
	}
}

func TestParseResults(t *testing.T) {
	dir := t.TempDir()

	rows := []ResultRow{
		{Iteration: 0, Timestamp: time.Now(), Hypothesis: "baseline", MetricValue: 1.1, Kept: true, BestSoFar: 1.1},
		{Iteration: 1, Timestamp: time.Now(), Hypothesis: "double lr", MetricValue: 1.05, Kept: true, CommitHash: "abc", BestSoFar: 1.05, DeltaFromBest: -0.05},
		{Iteration: 2, Timestamp: time.Now(), Hypothesis: "add norm", MetricValue: 1.12, Kept: false, BestSoFar: 1.05, DeltaFromBest: 0.07},
	}
	for _, r := range rows {
		if err := AppendResult(dir, r); err != nil {
			t.Fatal(err)
		}
	}

	parsed := testutil.Must(ParseResults(dir))
	if len(parsed) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(parsed))
	}
	if parsed[0].Hypothesis != "baseline" {
		t.Errorf("first row hypothesis = %q, want baseline", parsed[0].Hypothesis)
	}
	if parsed[1].MetricValue != 1.05 {
		t.Errorf("second row metric = %f, want 1.05", parsed[1].MetricValue)
	}
	if parsed[2].Kept {
		t.Error("third row should not be kept")
	}
}

func TestTrendAnalysis(t *testing.T) {
	cfg := &Config{MetricName: "loss", MetricDirection: "minimize"}
	cfg.Params.applyDefaults()
	now := time.Now()
	rows := []ResultRow{
		{Iteration: 0, Timestamp: now, MetricValue: 1.0, Kept: true, BestSoFar: 1.0},
		{Iteration: 1, Timestamp: now.Add(5 * time.Minute), MetricValue: 0.95, Kept: true, BestSoFar: 0.95, DeltaFromBest: -0.05},
		{Iteration: 2, Timestamp: now.Add(10 * time.Minute), MetricValue: 1.1, Kept: false, BestSoFar: 0.95, DeltaFromBest: 0.15},
		{Iteration: 3, Timestamp: now.Add(15 * time.Minute), MetricValue: 0.90, Kept: true, BestSoFar: 0.90, DeltaFromBest: -0.05},
	}

	analysis := TrendAnalysis(rows, cfg)
	if analysis == "" {
		t.Fatal("expected non-empty trend analysis")
	}
	if !contains(analysis, "success rate") {
		t.Error("should mention success rate")
	}
	if !contains(analysis, "Top improvements") {
		t.Error("should mention top improvements")
	}
}

func TestSaveExperimentOutput(t *testing.T) {
	dir := t.TempDir()
	err := SaveExperimentOutput(dir, 5, "metric: 1.05\n", "warning: slow\n")
	testutil.NoError(t, err)

	// Verify file exists.
	path := dir + "/.autoresearch/runs/0005.log"
	data := testutil.Must(os.ReadFile(path))
	content := string(data)
	if !contains(content, "metric: 1.05") {
		t.Error("stdout not saved")
	}
	if !contains(content, "warning: slow") {
		t.Error("stderr not saved")
	}
}

func TestSummary(t *testing.T) {
	dir := t.TempDir()
	baseline := 1.087
	best := 1.042
	cfg := &Config{
		TargetFiles:     []string{"train.py"},
		MetricCmd:       "python train.py",
		MetricName:      "val_bpb",
		MetricDirection: "minimize",
		TimeBudgetSec:   300,
		BranchTag:       "mar31",
		BaselineMetric:  &baseline,
		BestMetric:      &best,
		TotalIterations: 10,
		KeptIterations:  4,
	}

	summary := Summary(dir, cfg)
	if summary == "" {
		t.Error("expected non-empty summary")
	}
	// Check key info is present.
	for _, want := range []string{"val_bpb", "minimize", "10", "4", "1.042"} {
		if !contains(summary, want) {
			t.Errorf("summary missing %q:\n%s", want, summary)
		}
	}
}

func TestExtractMetric(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    float64
		wantErr bool
	}{
		{
			name:   "plain number",
			output: "1.087\n",
			want:   1.087,
		},
		{
			name:   "labeled value",
			output: "training...\nval_bpb: 1.065\n",
			want:   1.065,
		},
		{
			name:   "scientific notation",
			output: "loss: 2.5e-3\n",
			want:   2.5e-3,
		},
		{
			name:   "trailing empty lines",
			output: "step 100\n42.5\n\n\n",
			want:   42.5,
		},
		{
			name:    "no number",
			output:  "done\n",
			wantErr: true,
		},
		{
			name:    "empty",
			output:  "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractMetric(tt.output)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractMetric() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("extractMetric() = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestParseLLMResponse(t *testing.T) {
	resp := `HYPOTHESIS: double the learning rate for faster convergence
--- FILE: train.py ---
import torch
lr = 0.002
--- END FILE ---
`
	hypothesis, changes := parseLLMResponse(resp, []string{"train.py", "model.py"})

	if hypothesis != "double the learning rate for faster convergence" {
		t.Errorf("hypothesis = %q", hypothesis)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	content, ok := changes["train.py"]
	if !ok {
		t.Fatal("expected change for train.py")
	}
	if !contains(content, "lr = 0.002") {
		t.Errorf("unexpected content: %s", content)
	}
}

func TestParseLLMResponseNonTargetFile(t *testing.T) {
	resp := `HYPOTHESIS: hack the system
--- FILE: secret.py ---
malicious code
--- END FILE ---
`
	_, changes := parseLLMResponse(resp, []string{"train.py"})
	if len(changes) != 0 {
		t.Error("should not accept changes to non-target files")
	}
}

func TestParseLLMResponseMultipleFiles(t *testing.T) {
	resp := `HYPOTHESIS: refactor both files
--- FILE: train.py ---
new train content
--- END FILE ---
--- FILE: model.py ---
new model content
--- END FILE ---
`
	_, changes := parseLLMResponse(resp, []string{"train.py", "model.py"})
	if len(changes) != 2 {
		t.Errorf("expected 2 changes, got %d", len(changes))
	}
}

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

func TestExtractMetricWithPattern(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		pattern string
		want    float64
		wantErr bool
	}{
		{
			name:    "key-value pattern",
			output:  "epoch 10 | val_bpb: 1.087 | train_loss: 0.95",
			pattern: `val_bpb:\s*([\d.]+)`,
			want:    1.087,
		},
		{
			name:    "coverage pattern",
			output:  "ok  coverage: 85.3% of statements",
			pattern: `coverage:\s*([\d.]+)`,
			want:    85.3,
		},
		{
			name:    "multi-line finds last match",
			output:  "step 1: loss=2.0\nstep 2: loss=1.5\nstep 3: loss=1.2",
			pattern: `loss=([\d.]+)`,
			want:    1.2,
		},
		{
			name:    "no match",
			output:  "done successfully",
			pattern: `accuracy:\s*([\d.]+)`,
			wantErr: true,
		},
		{
			name:    "invalid regex",
			output:  "1.0",
			pattern: `[invalid`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractMetricWithPattern(tt.output, tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %f, want %f", got, tt.want)
			}
		})
	}
}

func TestExtractMetricSmartValidation(t *testing.T) {
	// NaN detection.
	_, err := extractMetricSmart("NaN\n", "")
	if err == nil {
		t.Error("expected error for NaN")
	}

	// Pattern mode.
	val := testutil.Must(extractMetricSmart("loss: 0.5\n", `loss:\s*([\d.]+)`))
	if val != 0.5 {
		t.Errorf("got %f, want 0.5", val)
	}

	// Heuristic mode fallback.
	val = testutil.Must(extractMetricSmart("42.0\n", ""))
	if val != 42.0 {
		t.Errorf("got %f, want 42.0", val)
	}
}

func TestDefaultParams(t *testing.T) {
	p := DefaultParams()
	// Verify all defaults match the original hard-coded values.
	checks := []struct {
		name string
		got  int
		want int
	}{
		{"MaxTokens", p.MaxTokens, 8192},
		{"GracePeriodSec", p.GracePeriodSec, 30},
		{"RetryPauseSec", p.RetryPauseSec, 5},
		{"StuckThresholdMild", p.StuckThresholdMild, 3},
		{"StuckThresholdModerate", p.StuckThresholdModerate, 5},
		{"StuckThresholdCritical", p.StuckThresholdCritical, 8},
		{"PhaseEarlyEnd", p.PhaseEarlyEnd, 3},
		{"PhaseExplorationEnd", p.PhaseExplorationEnd, 15},
		{"PhaseExploitationEnd", p.PhaseExploitationEnd, 30},
		{"RecentFailedWindow", p.RecentFailedWindow, 5},
		{"TrendWindowSize", p.TrendWindowSize, 10},
		{"PlateauThreshold", p.PlateauThreshold, 5},
		{"DefaultTimeBudgetSec", p.DefaultTimeBudgetSec, 3600},
		{"MaxIterations", p.MaxIterations, 30},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
	if p.DefaultModel != "claude-sonnet-4-20250514" {
		t.Errorf("DefaultModel = %q, want claude-sonnet-4-20250514", p.DefaultModel)
	}
}

func TestParamsApplyDefaults(t *testing.T) {
	// Zero-valued params should get all defaults.
	var p Params
	p.applyDefaults()
	d := DefaultParams()
	if p.MaxTokens != d.MaxTokens {
		t.Errorf("MaxTokens = %d, want %d", p.MaxTokens, d.MaxTokens)
	}
	if p.PhaseEarlyEnd != d.PhaseEarlyEnd {
		t.Errorf("PhaseEarlyEnd = %d, want %d", p.PhaseEarlyEnd, d.PhaseEarlyEnd)
	}
	if p.DefaultModel != d.DefaultModel {
		t.Errorf("DefaultModel = %q, want %q", p.DefaultModel, d.DefaultModel)
	}
}

func TestParamsApplyDefaultsSanity(t *testing.T) {
	// Reversed stuck thresholds should be reset to defaults.
	p := Params{
		StuckThresholdMild:     10, // > moderate
		StuckThresholdModerate: 5,
		StuckThresholdCritical: 3, // < moderate — invalid
	}
	p.applyDefaults()
	d := DefaultParams()
	if p.StuckThresholdMild != d.StuckThresholdMild {
		t.Errorf("reversed stuck thresholds not reset: mild=%d", p.StuckThresholdMild)
	}

	// Reversed phase boundaries should be reset to defaults.
	p2 := Params{
		PhaseEarlyEnd:        20, // > exploration
		PhaseExplorationEnd:  10,
		PhaseExploitationEnd: 5, // < exploration — invalid
	}
	p2.applyDefaults()
	if p2.PhaseEarlyEnd != d.PhaseEarlyEnd {
		t.Errorf("reversed phase boundaries not reset: early=%d", p2.PhaseEarlyEnd)
	}
}

func TestCustomParamsPhase(t *testing.T) {
	// Custom phase boundaries should change when phases trigger.
	cfg := &Config{
		MetricName:      "loss",
		MetricDirection: "minimize",
		TimeBudgetSec:   60,
		Params: Params{
			PhaseEarlyEnd:       10, // extended early phase
			PhaseExplorationEnd: 20,
		},
	}
	cfg.Params.applyDefaults()

	runner := NewRunner(nil)

	// Iteration 5 should still be EARLY with custom boundary (default would be EXPLORATION).
	p := runner.buildPrompt(cfg, nil, "", 5)
	if !contains(p.user, "EARLY") {
		t.Error("iteration 5 with PhaseEarlyEnd=10 should be EARLY phase")
	}

	// Iteration 15 should be EXPLORATION with custom boundary (default would be EXPLOITATION).
	p = runner.buildPrompt(cfg, nil, "", 15)
	if !contains(p.user, "EXPLORATION") {
		t.Error("iteration 15 with PhaseExplorationEnd=20 should be EXPLORATION phase")
	}
}

func TestConfigSaveLoadWithParams(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		TargetFiles:     []string{"train.py"},
		MetricCmd:       "python train.py",
		MetricName:      "loss",
		MetricDirection: "minimize",
		TimeBudgetSec:   120,
		BranchTag:       "test",
		Params: Params{
			MaxTokens:      4096,
			GracePeriodSec: 60,
			PhaseEarlyEnd:  10,
		},
	}

	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	loaded := testutil.Must(LoadConfig(dir))

	// Custom values should persist.
	if loaded.Params.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096", loaded.Params.MaxTokens)
	}
	if loaded.Params.GracePeriodSec != 60 {
		t.Errorf("GracePeriodSec = %d, want 60", loaded.Params.GracePeriodSec)
	}
	if loaded.Params.PhaseEarlyEnd != 10 {
		t.Errorf("PhaseEarlyEnd = %d, want 10", loaded.Params.PhaseEarlyEnd)
	}

	// Zero-valued fields should be zero in JSON (applyDefaults fills them at Validate time).
	if loaded.Params.RetryPauseSec != 0 {
		t.Errorf("RetryPauseSec should be 0 before Validate, got %d", loaded.Params.RetryPauseSec)
	}

	// After Validate, defaults should be applied.
	if err := loaded.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if loaded.Params.RetryPauseSec != 5 {
		t.Errorf("RetryPauseSec after Validate = %d, want 5", loaded.Params.RetryPauseSec)
	}
	// Custom values should survive Validate.
	if loaded.Params.MaxTokens != 4096 {
		t.Errorf("MaxTokens after Validate = %d, want 4096", loaded.Params.MaxTokens)
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
	for i := range len(s) {
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
