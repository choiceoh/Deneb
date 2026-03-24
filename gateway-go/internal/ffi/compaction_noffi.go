//go:build no_ffi || !cgo

package ffi

import (
	"encoding/json"
	"errors"
	"math"
)

// CompactionEvaluate is a pure-Go fallback for compaction threshold evaluation.
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

// CompactionSweepNew is not available without FFI.
func CompactionSweepNew(_ string, _, _ uint64, _, _ bool, _ int64) (uint32, error) {
	return 0, errors.New("ffi: compaction sweep not available without native FFI")
}

// CompactionSweepStart is not available without FFI.
func CompactionSweepStart(_ uint32) (json.RawMessage, error) {
	return nil, errors.New("ffi: compaction sweep not available without native FFI")
}

// CompactionSweepStep is not available without FFI.
func CompactionSweepStep(_ uint32, _ []byte) (json.RawMessage, error) {
	return nil, errors.New("ffi: compaction sweep not available without native FFI")
}

// CompactionSweepDrop is a no-op without FFI.
func CompactionSweepDrop(_ uint32) {}
