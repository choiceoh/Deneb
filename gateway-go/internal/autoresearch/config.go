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
	"regexp"
)

// configDir is the hidden directory inside the workspace that stores
// autoresearch state (config + results).
const configDir = ".autoresearch"

// ConstantDef describes a named constant the agent may tune in override mode.
// Instead of rewriting entire files, the agent proposes new values for these
// constants. The original source files are never permanently modified.
type ConstantDef struct {
	// Name is a human-readable identifier (e.g. "LEARNING_RATE").
	Name string `json:"name"`
	// File is the target file containing this constant (relative to workdir).
	File string `json:"file"`
	// Pattern is a regex with exactly one capture group for the constant's value.
	// Example: `lr\s*=\s*([\d.]+)` captures "0.001" from "lr = 0.001".
	// When empty, a pattern is auto-generated from Name + Type so that agents
	// don't need to write regex manually.
	Pattern string `json:"pattern,omitempty"`
	// Type is the value type: "float", "int", or "string".
	Type string `json:"type"`
	// Min is an optional lower bound (float/int types only).
	Min *float64 `json:"min,omitempty"`
	// Max is an optional upper bound (float/int types only).
	Max *float64 `json:"max,omitempty"`
}

// EffectivePattern returns the regex pattern for this constant. If Pattern is
// set, it is returned as-is. Otherwise a robust pattern is auto-generated from
// Name and Type, tolerating any leading whitespace (tabs, spaces) and flexible
// spacing around the `=` sign.
func (cd ConstantDef) EffectivePattern() string {
	if cd.Pattern != "" {
		return cd.Pattern
	}
	// Escape the name for regex safety (handles names with special chars).
	escaped := regexp.QuoteMeta(cd.Name)
	// Build a capture group appropriate for the value type.
	var capture string
	switch cd.Type {
	case "int":
		capture = `(-?\d+)`
	case "string":
		capture = `"([^"]*)"`
	default: // float
		capture = `(-?[\d.]+(?:e[+-]?\d+)?)`
	}
	// Allow any leading whitespace and flexible spacing around `=`.
	return `\b` + escaped + `\s*=\s*` + capture
}

// OverrideSet is the persisted set of best-found override values.
type OverrideSet struct {
	Values map[string]string `json:"values"`
}

// Params holds tunable experiment parameters. Zero values fall back to
// DefaultParams(). Users can override individual fields in config.json
// without touching Go source — the rest auto-fill with safe defaults.
type Params struct {
	// DefaultModel is the LLM model used when Config.Model is empty.
	DefaultModel string `json:"default_model,omitempty"`
	// MaxTokens is the max tokens for LLM hypothesis generation.
	MaxTokens int `json:"max_tokens,omitempty"`
	// GracePeriodSec is extra seconds beyond TimeBudgetSec before killing the process.
	GracePeriodSec int `json:"grace_period_sec,omitempty"`
	// RetryPauseSec is how long to wait after an iteration error before retrying.
	RetryPauseSec int `json:"retry_pause_sec,omitempty"`
	// StuckThresholdMild is consecutive failures before mild recovery prompt.
	StuckThresholdMild int `json:"stuck_threshold_mild,omitempty"`
	// StuckThresholdModerate is consecutive failures before moderate recovery prompt.
	StuckThresholdModerate int `json:"stuck_threshold_moderate,omitempty"`
	// StuckThresholdCritical is consecutive failures before critical recovery prompt.
	StuckThresholdCritical int `json:"stuck_threshold_critical,omitempty"`
	// PhaseEarlyEnd is the last iteration of the "early" phase (inclusive).
	PhaseEarlyEnd int `json:"phase_early_end,omitempty"`
	// PhaseExplorationEnd is the last iteration of the "exploration" phase (inclusive).
	PhaseExplorationEnd int `json:"phase_exploration_end,omitempty"`
	// PhaseExploitationEnd is the last iteration of the "exploitation" phase (inclusive).
	PhaseExploitationEnd int `json:"phase_exploitation_end,omitempty"`
	// RecentFailedWindow is how many recent iterations to scan for failed hypotheses.
	RecentFailedWindow int `json:"recent_failed_window,omitempty"`
	// TrendWindowSize is the number of recent iterations used for trend analysis.
	TrendWindowSize int `json:"trend_window_size,omitempty"`
	// PlateauThreshold is consecutive discards before flagging a plateau.
	PlateauThreshold int `json:"plateau_threshold,omitempty"`
	// DefaultTimeBudgetSec is the fallback time budget when Config.TimeBudgetSec is 0.
	DefaultTimeBudgetSec int `json:"default_time_budget_sec,omitempty"`
	// MaxIterations is the total number of iterations to run before auto-stopping.
	// When reached, the runner stops and sends a completion report with chart.
	// 0 means unlimited (manual stop only). Default: 30.
	MaxIterations int `json:"max_iterations,omitempty"`
}

// DefaultParams returns the canonical default values for all tunable parameters.
// These match the original hard-coded constants exactly.
func DefaultParams() Params {
	return Params{
		DefaultModel:           "claude-sonnet-4-20250514",
		MaxTokens:              8192,
		GracePeriodSec:         30,
		RetryPauseSec:          5,
		StuckThresholdMild:     3,
		StuckThresholdModerate: 5,
		StuckThresholdCritical: 8,
		PhaseEarlyEnd:          3,
		PhaseExplorationEnd:    15,
		PhaseExploitationEnd:   30,
		RecentFailedWindow:     5,
		TrendWindowSize:        10,
		PlateauThreshold:       5,
		DefaultTimeBudgetSec:   300,
		MaxIterations:          30,
	}
}

// applyDefaults fills zero-valued fields with DefaultParams values, then
// validates sanity (e.g. phase ordering). Invalid combos are silently
// replaced with defaults so a bad config.json never breaks the runner.
func (p *Params) applyDefaults() {
	d := DefaultParams()
	if p.DefaultModel == "" {
		p.DefaultModel = d.DefaultModel
	}
	if p.MaxTokens <= 0 {
		p.MaxTokens = d.MaxTokens
	}
	if p.GracePeriodSec <= 0 {
		p.GracePeriodSec = d.GracePeriodSec
	}
	if p.RetryPauseSec <= 0 {
		p.RetryPauseSec = d.RetryPauseSec
	}
	if p.StuckThresholdMild <= 0 {
		p.StuckThresholdMild = d.StuckThresholdMild
	}
	if p.StuckThresholdModerate <= 0 {
		p.StuckThresholdModerate = d.StuckThresholdModerate
	}
	if p.StuckThresholdCritical <= 0 {
		p.StuckThresholdCritical = d.StuckThresholdCritical
	}
	if p.PhaseEarlyEnd <= 0 {
		p.PhaseEarlyEnd = d.PhaseEarlyEnd
	}
	if p.PhaseExplorationEnd <= 0 {
		p.PhaseExplorationEnd = d.PhaseExplorationEnd
	}
	if p.PhaseExploitationEnd <= 0 {
		p.PhaseExploitationEnd = d.PhaseExploitationEnd
	}
	if p.RecentFailedWindow <= 0 {
		p.RecentFailedWindow = d.RecentFailedWindow
	}
	if p.TrendWindowSize <= 0 {
		p.TrendWindowSize = d.TrendWindowSize
	}
	if p.PlateauThreshold <= 0 {
		p.PlateauThreshold = d.PlateauThreshold
	}
	if p.DefaultTimeBudgetSec <= 0 {
		p.DefaultTimeBudgetSec = d.DefaultTimeBudgetSec
	}
	// MaxIterations: 0 means "not set" (JSON omitempty), default to 30.
	// Negative means unlimited (explicit opt-out).
	if p.MaxIterations == 0 {
		p.MaxIterations = d.MaxIterations
	}
	if p.MaxIterations < 0 {
		p.MaxIterations = 0
	}

	// Sanity: stuck thresholds must be in ascending order.
	if p.StuckThresholdMild >= p.StuckThresholdModerate ||
		p.StuckThresholdModerate >= p.StuckThresholdCritical {
		p.StuckThresholdMild = d.StuckThresholdMild
		p.StuckThresholdModerate = d.StuckThresholdModerate
		p.StuckThresholdCritical = d.StuckThresholdCritical
	}

	// Sanity: phase boundaries must be in ascending order.
	if p.PhaseEarlyEnd >= p.PhaseExplorationEnd ||
		p.PhaseExplorationEnd >= p.PhaseExploitationEnd {
		p.PhaseEarlyEnd = d.PhaseEarlyEnd
		p.PhaseExplorationEnd = d.PhaseExplorationEnd
		p.PhaseExploitationEnd = d.PhaseExploitationEnd
	}
}

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
	// MetricPattern is an optional regex to extract the metric from experiment
	// output. Must contain exactly one capture group for the numeric value.
	// Example: `val_bpb:\s*([\d.]+)` extracts 1.087 from "val_bpb: 1.087".
	// If empty, the runner uses the default heuristic (last number on last line).
	MetricPattern string `json:"metric_pattern,omitempty"`
	// CacheEnabled enables a persistent cache directory for experiment commands.
	// When true, the runner sets AUTORESEARCH_CACHE_DIR env var pointing to a
	// stable cache directory (.autoresearch/cache/) so that expensive operations
	// like LLM inference or embedding computation can cache their results across
	// iterations.
	CacheEnabled bool `json:"cache_enabled,omitempty"`
	// CacheDir overrides the default cache directory path. If empty and
	// CacheEnabled is true, defaults to .autoresearch/cache/ inside workdir.
	CacheDir string `json:"cache_dir,omitempty"`
	// OriginalBranch records the branch autoresearch was started from,
	// so we know where to return after the experiment completes.
	OriginalBranch string `json:"original_branch,omitempty"`

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

	// Params holds tunable experiment parameters (phase boundaries, thresholds, etc.).
	// Zero-valued fields automatically fall back to DefaultParams().
	Params Params `json:"params,omitempty"`

	// Constants lists named constants for override mode. When non-empty,
	// the agent proposes override values instead of rewriting entire files.
	Constants []ConstantDef `json:"constants,omitempty"`
}

// IsConstantsMode returns true when override mode is active.
func (c *Config) IsConstantsMode() bool {
	return len(c.Constants) > 0
}

// Validate checks that required fields are set and applies param defaults.
func (c *Config) Validate() error {
	// Apply param defaults first so TimeBudgetSec fallback uses the configured value.
	c.Params.applyDefaults()

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
		c.TimeBudgetSec = c.Params.DefaultTimeBudgetSec
	}

	// Validate constants definitions when override mode is active.
	if c.IsConstantsMode() {
		targetSet := make(map[string]bool, len(c.TargetFiles))
		for _, tf := range c.TargetFiles {
			targetSet[tf] = true
		}
		for i := range c.Constants {
			cd := &c.Constants[i]
			if cd.Name == "" {
				return fmt.Errorf("constants[%d]: name is required", i)
			}
			if cd.File == "" {
				return fmt.Errorf("constants[%d] (%s): file is required", i, cd.Name)
			}
			if !targetSet[cd.File] {
				return fmt.Errorf("constants[%d] (%s): file %q not in target_files", i, cd.Name, cd.File)
			}
			// Default type to "float" when empty — most constants are floats.
			if cd.Type == "" {
				cd.Type = "float"
			}
			switch cd.Type {
			case "float", "int", "string":
			default:
				return fmt.Errorf("constants[%d] (%s): type must be float, int, or string, got %q", i, cd.Name, cd.Type)
			}
			// Auto-fill Pattern from Name + Type when empty.
			if cd.Pattern == "" {
				cd.Pattern = cd.EffectivePattern()
			}
			if _, err := regexp.Compile(cd.Pattern); err != nil {
				return fmt.Errorf("constants[%d] (%s): invalid pattern: %w", i, cd.Name, err)
			}
		}
	}

	return nil
}

// ResolveCacheDir returns the cache directory path for this experiment.
// Returns empty string if caching is disabled.
func (c *Config) ResolveCacheDir(workdir string) string {
	if !c.CacheEnabled {
		return ""
	}
	if c.CacheDir != "" {
		if filepath.IsAbs(c.CacheDir) {
			return c.CacheDir
		}
		return filepath.Join(workdir, c.CacheDir)
	}
	return filepath.Join(workdir, configDir, "cache")
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
