package autoresearch

import (
	"os"
	"testing"
	"time"
)

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

	parsed, err := ParseResults(dir)
	if err != nil {
		t.Fatalf("ParseResults: %v", err)
	}
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
	if err != nil {
		t.Fatalf("SaveExperimentOutput: %v", err)
	}

	// Verify file exists.
	path := dir + "/.autoresearch/runs/0005.log"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
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
	val, err := extractMetricSmart("loss: 0.5\n", `loss:\s*([\d.]+)`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 0.5 {
		t.Errorf("got %f, want 0.5", val)
	}

	// Heuristic mode fallback.
	val, err = extractMetricSmart("42.0\n", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 42.0 {
		t.Errorf("got %f, want 42.0", val)
	}
}
