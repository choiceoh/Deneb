package autoresearch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
				TimeBudgetSec:   0, // should default to 300
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
	min := &Config{MetricDirection: "minimize"}
	max := &Config{MetricDirection: "maximize"}

	if !min.IsBetter(0.9, 1.0) {
		t.Error("minimize: 0.9 should be better than 1.0")
	}
	if min.IsBetter(1.1, 1.0) {
		t.Error("minimize: 1.1 should not be better than 1.0")
	}
	if !max.IsBetter(1.1, 1.0) {
		t.Error("maximize: 1.1 should be better than 1.0")
	}
	if max.IsBetter(0.9, 1.0) {
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

	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

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
		Iteration:   1,
		Timestamp:   time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
		Hypothesis:  "increase lr",
		MetricValue: 1.087,
		Kept:        true,
		CommitHash:  "abc1234",
		DurationSec: 300,
	}
	row2 := ResultRow{
		Iteration:   2,
		Timestamp:   time.Date(2026, 3, 31, 0, 5, 0, 0, time.UTC),
		Hypothesis:  "add layer norm",
		MetricValue: 1.095,
		Kept:        false,
		CommitHash:  "",
		DurationSec: 300,
	}

	if err := AppendResult(dir, row1); err != nil {
		t.Fatalf("AppendResult row1: %v", err)
	}
	if err := AppendResult(dir, row2); err != nil {
		t.Fatalf("AppendResult row2: %v", err)
	}

	content, err := ReadResults(dir)
	if err != nil {
		t.Fatalf("ReadResults: %v", err)
	}

	// Should have header + 2 data rows.
	lines := splitNonEmpty(content)
	if len(lines) != 3 {
		t.Errorf("expected 3 lines (header + 2 rows), got %d:\n%s", len(lines), content)
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
	cfg := &Config{
		MetricName:          "loss",
		MetricDirection:     "minimize",
		TimeBudgetSec:       60,
		ConsecutiveFailures: 5,
	}
	runner := NewRunner(nil)
	p := runner.buildPrompt(cfg, nil, "", 10)

	if !contains(p.system, "fundamentally different") {
		t.Error("system prompt should include stuck recovery for 5+ failures")
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
