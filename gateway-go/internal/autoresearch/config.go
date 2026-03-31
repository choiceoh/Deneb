// Package autoresearch implements an autonomous experiment loop inspired by
// karpathy/autoresearch. An AI agent iteratively modifies code, runs a fixed-
// time experiment, evaluates a scalar metric, and keeps improvements or reverts
// failures — all without human intervention.
package autoresearch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// configDir is the hidden directory inside the workspace that stores
// autoresearch state (config + results).
const configDir = ".autoresearch"

// Config describes an autoresearch experiment session.
type Config struct {
	// TargetFiles lists the files the agent may edit (relative to workdir).
	TargetFiles []string `json:"target_files"`
	// MetricCmd is the shell command that runs the experiment and prints the metric.
	MetricCmd string `json:"metric_cmd"`
	// MetricName is a human-readable name for the metric (e.g. "val_bpb").
	MetricName string `json:"metric_name"`
	// MetricDirection is "minimize" or "maximize".
	MetricDirection string `json:"metric_direction"`
	// TimeBudgetSec is the fixed time budget per experiment in seconds.
	TimeBudgetSec int `json:"time_budget_sec"`
	// BranchTag is the suffix for the experiment branch (autoresearch/<tag>).
	BranchTag string `json:"branch_tag"`
	// Model is the LLM model to use for hypothesis generation.
	Model string `json:"model,omitempty"`

	// --- Mutable state updated during the run ---

	// BestMetric is the best metric value achieved so far.
	BestMetric *float64 `json:"best_metric,omitempty"`
	// BestCommit is the git commit hash for the best metric.
	BestCommit string `json:"best_commit,omitempty"`
	// KeptCommit is the most recent commit that was kept.
	KeptCommit string `json:"kept_commit,omitempty"`
	// BaselineMetric is the metric value from the initial baseline run.
	BaselineMetric *float64 `json:"baseline_metric,omitempty"`
	// TotalIterations is the number of completed experiment iterations.
	TotalIterations int `json:"total_iterations"`
	// KeptIterations is the number of iterations where the result was kept.
	KeptIterations int `json:"kept_iterations"`
	// ConsecutiveFailures tracks failures in a row for stuck recovery.
	ConsecutiveFailures int `json:"consecutive_failures"`
}

// Validate checks that required fields are set.
func (c *Config) Validate() error {
	if len(c.TargetFiles) == 0 {
		return fmt.Errorf("target_files is required")
	}
	if c.MetricCmd == "" {
		return fmt.Errorf("metric_cmd is required")
	}
	if c.MetricName == "" {
		return fmt.Errorf("metric_name is required")
	}
	if c.MetricDirection != "minimize" && c.MetricDirection != "maximize" {
		return fmt.Errorf("metric_direction must be 'minimize' or 'maximize', got %q", c.MetricDirection)
	}
	if c.BranchTag == "" {
		return fmt.Errorf("branch_tag is required")
	}
	if c.TimeBudgetSec <= 0 {
		c.TimeBudgetSec = 300 // default 5 minutes
	}
	return nil
}

// IsBetter returns true if newVal is better than oldVal according to the
// configured metric direction.
func (c *Config) IsBetter(newVal, oldVal float64) bool {
	if c.MetricDirection == "minimize" {
		return newVal < oldVal
	}
	return newVal > oldVal
}

// configPath returns the path to config.json inside the workspace.
func configPath(workdir string) string {
	return filepath.Join(workdir, configDir, "config.json")
}

// LoadConfig reads the config from the workspace.
func LoadConfig(workdir string) (*Config, error) {
	data, err := os.ReadFile(configPath(workdir))
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// SaveConfig writes the config to the workspace.
func SaveConfig(workdir string, cfg *Config) error {
	dir := filepath.Join(workdir, configDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(configPath(workdir), data, 0o644)
}
