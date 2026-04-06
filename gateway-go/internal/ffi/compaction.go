package ffi

import (
	"encoding/json"
	"errors"
	"math"
)

// CompactionEvaluate evaluates whether compaction is needed (pure Go).
func CompactionEvaluate(configJSON string, storedTokens, liveTokens, tokenBudget uint64) ([]byte, error) {
	var config struct {
		ContextThreshold float64 `json:"contextThreshold"`
	}
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return nil, errors.New("ffi: invalid config JSON")
	}
	if config.ContextThreshold <= 0 {
		config.ContextThreshold = 0.75
	}

	currentTokens := storedTokens
	if liveTokens > currentTokens {
		currentTokens = liveTokens
	}
	threshold := uint64(math.Floor(config.ContextThreshold * float64(tokenBudget)))

	type decision struct {
		ShouldCompact bool   `json:"shouldCompact"`
		Reason        string `json:"reason"`
		CurrentTokens uint64 `json:"currentTokens"`
		Threshold     uint64 `json:"threshold"`
	}

	d := decision{
		CurrentTokens: currentTokens,
		Threshold:     threshold,
	}
	if currentTokens > threshold {
		d.ShouldCompact = true
		d.Reason = "threshold"
	} else {
		d.Reason = "none"
	}

	return json.Marshal(d)
}

// CompactionSweepNew is not available (Rust FFI removed).
func CompactionSweepNew(_ string, _, _ uint64, _, _ bool, _ int64) (uint32, error) {
	return 0, errors.New("ffi: compaction sweep not available (Rust FFI removed)")
}

// CompactionSweepStart is not available (Rust FFI removed).
func CompactionSweepStart(_ uint32) (json.RawMessage, error) {
	return nil, errors.New("ffi: compaction sweep not available (Rust FFI removed)")
}

// CompactionSweepStep is not available (Rust FFI removed).
func CompactionSweepStep(_ uint32, _ []byte) (json.RawMessage, error) {
	return nil, errors.New("ffi: compaction sweep not available (Rust FFI removed)")
}

// CompactionSweepDrop is a no-op (Rust FFI removed).
func CompactionSweepDrop(_ uint32) {}
