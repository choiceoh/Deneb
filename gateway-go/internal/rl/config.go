// Package rl provides the RL self-learning pipeline for Deneb.
//
// Architecture: Go gateway orchestrates external Python processes (sglang +
// Tinker-Atropos) rather than reimplementing training. The Go side handles
// lifecycle management, session trajectory collection, and LoRA adapter loading.
//
// Process topology on DGX Spark:
//
//	Go Gateway (orchestrator)
//	  ├── sglang server (inference + logprobs for rollouts)
//	  ├── Tinker trainer (IS loss + LoRA + Adam)
//	  └── Atropos server (trajectory API + environment scoring)
package rl

import (
	"os"
	"path/filepath"
)

// Config configures the RL training pipeline.
type Config struct {
	// Enable toggles the entire RL pipeline. Default: false.
	Enable bool `json:"enable" yaml:"enable"`

	// BaseModelPath is the HuggingFace model path for sglang.
	BaseModelPath string `json:"baseModelPath" yaml:"baseModelPath"`

	// AdapterDir is where trained LoRA adapters are saved.
	AdapterDir string `json:"adapterDir" yaml:"adapterDir"`

	// VenvDir is the Python virtualenv containing sglang/Tinker/Atropos.
	VenvDir string `json:"venvDir" yaml:"venvDir"`

	// SGLang configuration.
	SGLang SGLangConfig `json:"sglang" yaml:"sglang"`

	// Tinker trainer configuration.
	Tinker TinkerConfig `json:"tinker" yaml:"tinker"`

	// Atropos environment configuration.
	Atropos AtroposConfig `json:"atropos" yaml:"atropos"`

	// Collection controls which sessions are collected for training.
	Collection CollectionConfig `json:"collection" yaml:"collection"`
}

// SGLangConfig configures the sglang inference server.
type SGLangConfig struct {
	Port       int     `json:"port" yaml:"port"`             // default: 30000
	GPUMemFrac float64 `json:"gpuMemFrac" yaml:"gpuMemFrac"` // default: 0.85
	TPSize     int     `json:"tpSize" yaml:"tpSize"`         // tensor parallel, default: 1
}

// TinkerConfig configures the Tinker LoRA trainer.
type TinkerConfig struct {
	LoraRank     int     `json:"loraRank" yaml:"loraRank"`         // default: 32
	LearningRate float64 `json:"learningRate" yaml:"learningRate"` // default: 3e-5
	BatchSize    int     `json:"batchSize" yaml:"batchSize"`       // default: 4
	GroupSize    int     `json:"groupSize" yaml:"groupSize"`       // default: 16
}

// AtroposConfig configures the Atropos trajectory API.
type AtroposConfig struct {
	Port int `json:"port" yaml:"port"` // default: 30001
}

// CollectionConfig controls session trajectory collection.
type CollectionConfig struct {
	// MinTurns is the minimum agent turns to collect a session. Default: 3.
	MinTurns int `json:"minTurns" yaml:"minTurns"`
	// MinToolCalls is the minimum tool calls. Default: 2.
	MinToolCalls int `json:"minToolCalls" yaml:"minToolCalls"`
}

// DefaultConfig returns sensible defaults for DGX Spark deployment.
func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	adapterDir := ""
	venvDir := ""
	if home != "" {
		adapterDir = filepath.Join(home, ".deneb", "rl", "adapters")
		venvDir = filepath.Join(home, ".deneb", "rl", "venv")
	}
	return Config{
		Enable:     false,
		AdapterDir: adapterDir,
		VenvDir:    venvDir,
		SGLang: SGLangConfig{
			Port:       30000,
			GPUMemFrac: 0.85,
			TPSize:     1,
		},
		Tinker: TinkerConfig{
			LoraRank:     32,
			LearningRate: 3e-5,
			BatchSize:    4,
			GroupSize:    16,
		},
		Atropos: AtroposConfig{
			Port: 30001,
		},
		Collection: CollectionConfig{
			MinTurns:     3,
			MinToolCalls: 2,
		},
	}
}
