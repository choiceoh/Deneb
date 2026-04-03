package autoresearch

import (
	"testing"
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

	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

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
