// Package rl provides a task-specific RL training pipeline for Deneb.
//
// Architecture: Go gateway orchestrates external Python processes (sglang +
// Tinker-Atropos) rather than implementing training. The Go side handles
// process lifecycle, trajectory collection from the local AI hub, and
// LoRA adapter hot-reload on the serving sglang instance.
//
// Unlike generic RL, this targets the specific narrow tasks the local AI
// already performs (fact extraction, compaction, verification, etc.) with
// measurable per-task reward functions.
//
// Process topology on DGX Spark:
//
//	Go Gateway (orchestrator)
//	  ├── sglang server (inference + logprobs for rollouts)
//	  ├── Tinker trainer (IS loss + LoRA + Adam)
//	  └── Atropos server (multi-environment trajectory scoring)
package rl

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Config configures the RL training pipeline.
type Config struct {
	// Enabled toggles the entire RL pipeline. Default: false.
	Enabled bool `json:"enabled"`

	// BaseModelPath is the HuggingFace model path for the training sglang.
	BaseModelPath string `json:"baseModelPath"`

	// AdapterDir is where Tinker saves trained LoRA adapter checkpoints.
	AdapterDir string `json:"adapterDir"`

	// TrajectoryDir is the JSONL trajectory export directory.
	TrajectoryDir string `json:"trajectoryDir"`

	// VenvDir is the Python virtualenv containing sglang/Tinker/Atropos.
	VenvDir string `json:"venvDir"`

	// MaxTrajectories is the in-memory ring buffer capacity. Default: 10000.
	MaxTrajectories int `json:"maxTrajectories"`

	// WatchdogInterval is how often to check process health + scan for
	// new adapters. Default: 30 seconds.
	WatchdogInterval time.Duration `json:"watchdogInterval"`

	// SGLang configures the training sglang inference server.
	SGLang SGLangConfig `json:"sglang"`

	// Tinker configures the LoRA trainer.
	Tinker TinkerConfig `json:"tinker"`

	// Atropos configures the multi-environment trajectory scorer.
	Atropos AtroposConfig `json:"atropos"`

	// Environments lists which local AI task types to collect trajectories for.
	Environments []EnvConfig `json:"environments"`

	// Collection controls session-level trajectory collection (fallback path).
	Collection CollectionConfig `json:"collection"`
}

// SGLangConfig configures the training sglang server (separate from serving sglang).
type SGLangConfig struct {
	Port       int     `json:"port"`       // default: 30100
	GPUMemFrac float64 `json:"gpuMemFrac"` // default: 0.4
	TPSize     int     `json:"tpSize"`     // tensor parallel, default: 1
}

// TinkerConfig configures the Tinker LoRA trainer.
type TinkerConfig struct {
	LoraRank     int     `json:"loraRank"`     // default: 32
	LearningRate float64 `json:"learningRate"` // default: 3e-5
	BatchSize    int     `json:"batchSize"`    // default: 4
	GroupSize    int     `json:"groupSize"`    // default: 16
}

// AtroposConfig configures the Atropos multi-environment server.
type AtroposConfig struct {
	Port int `json:"port"` // default: 30101
}

// EnvConfig configures a single task-type environment for trajectory collection.
type EnvConfig struct {
	// TaskType matches the CallerTag from local AI hub requests.
	TaskType string `json:"taskType"`
	// Weight controls sampling weight for training. Default: 1.0.
	Weight float64 `json:"weight"`
	// Enabled controls whether to collect trajectories for this task.
	Enabled bool `json:"enabled"`
}

// CollectionConfig controls session-level trajectory collection (SessionHook).
// This is the fallback path — the hub observer is the primary collection method.
type CollectionConfig struct {
	MinTurns     int `json:"minTurns"`
	MinToolCalls int `json:"minToolCalls"`
}

// DefaultConfig returns sensible defaults for DGX Spark deployment.
func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	rlDir := ""
	if home != "" {
		rlDir = filepath.Join(home, ".deneb", "rl")
	}
	return Config{
		Enabled:          false,
		AdapterDir:       filepath.Join(rlDir, "adapters"),
		TrajectoryDir:    filepath.Join(rlDir, "trajectories"),
		VenvDir:          filepath.Join(rlDir, "venv"),
		MaxTrajectories:  10000,
		WatchdogInterval: 30 * time.Second,
		SGLang: SGLangConfig{
			Port:       30100,
			GPUMemFrac: 0.4,
			TPSize:     1,
		},
		Tinker: TinkerConfig{
			LoraRank:     32,
			LearningRate: 3e-5,
			BatchSize:    4,
			GroupSize:    16,
		},
		Atropos: AtroposConfig{
			Port: 30101,
		},
		Environments: []EnvConfig{
			{TaskType: "memory_json", Weight: 1.0, Enabled: true},
			{TaskType: "aurora_compaction", Weight: 1.0, Enabled: true},
			{TaskType: "session_memory", Weight: 0.5, Enabled: true},
		},
		Collection: CollectionConfig{
			MinTurns:     3,
			MinToolCalls: 2,
		},
	}
}

// ConfigFromEnv builds a Config from environment variables.
// DENEB_RL_ENABLED=true enables the pipeline.
// DENEB_RL_MODEL overrides BaseModelPath.
func ConfigFromEnv() Config {
	cfg := DefaultConfig()
	if v := os.Getenv("DENEB_RL_ENABLED"); v == "true" || v == "1" {
		cfg.Enabled = true
	}
	if v := os.Getenv("DENEB_RL_MODEL"); v != "" {
		cfg.BaseModelPath = v
	}
	if v := os.Getenv("DENEB_RL_ADAPTER_DIR"); v != "" {
		cfg.AdapterDir = v
	}
	if v := os.Getenv("DENEB_RL_VENV_DIR"); v != "" {
		cfg.VenvDir = v
	}
	if v := os.Getenv("DENEB_RL_SGLANG_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.SGLang.Port = p
		}
	}
	return cfg
}
