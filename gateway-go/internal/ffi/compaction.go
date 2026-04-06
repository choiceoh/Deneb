package ffi

import (
	"encoding/json"
	"math"
)

// CompactionEvaluate evaluates whether compaction is needed (pure Go).
func CompactionEvaluate(configJSON string, storedTokens, liveTokens, tokenBudget uint64) ([]byte, error) {
	var config struct {
		ContextThreshold float64 `json:"contextThreshold"`
	}
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return nil, err
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
