package tokenest

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// ── Configuration ───────────────────────────────────────────────────────

const (
	// emaAlpha is the EMA smoothing factor. 0.1 = smooth (stable, slow
	// adaptation); 0.3 = responsive (faster adaptation, noisier).
	emaAlpha = 0.1

	// minSamples is the minimum observations before the correction factor
	// is applied. Below this threshold, Count returns the raw heuristic.
	minSamples = 10

	// Factor is clamped to [minFactor, maxFactor] to prevent runaway
	// correction from outlier data (e.g., tool schema overhead spikes).
	minFactor = 0.3
	maxFactor = 3.0
)

// ── Calibrator ──────────────────────────────────────────────────────────

// Calibrator maintains per-family correction factors learned from actual
// API usage data. Thread-safe.
//
// The correction factor converges such that:
//
//	Count(text) * factor ≈ actual API input_tokens
//
// Math: if estimated = rawCount * factor, then recording (estimated, actual)
// computes targetFactor = factor * actual / estimated = actual / rawCount,
// which is the ideal correction regardless of the current factor value.
type Calibrator struct {
	mu      sync.RWMutex
	entries [4]calEntry // indexed by Family
}

type calEntry struct {
	Factor  float64 `json:"factor"`
	Samples int     `json:"samples"`
}

// globalCal is the package-level calibrator used by Count/CountBytes.
var globalCal = newCalibrator()

func newCalibrator() *Calibrator {
	c := &Calibrator{}
	for i := range c.entries {
		c.entries[i].Factor = 1.0
	}
	return c
}

// RecordFeedback records an (estimated, actual) token count observation
// for self-calibration. Call this after each LLM API response.
//
//   - family: the model's tokenizer family (from ForModel().Family())
//   - estimated: what Count/CountBytes returned for the input content
//   - actual: the real input_tokens from the API response usage field
//
// The calibrator uses EMA to converge the correction factor over time.
// After ~50-100 observations per family, estimates converge to within
// ~3% of actual token counts.
func RecordFeedback(family Family, estimated, actual int) {
	globalCal.record(family, estimated, actual)
}

// CorrectionFactor returns the current correction multiplier for a family.
// Returns 1.0 if insufficient samples have been collected.
func CorrectionFactor(family Family) float64 {
	return globalCal.factor(family)
}

// CalibrationStats returns the current calibration state for diagnostics.
func CalibrationStats() map[string]any {
	globalCal.mu.RLock()
	defer globalCal.mu.RUnlock()
	names := [4]string{"claude", "openai", "gemini", "default"}
	stats := make(map[string]any, 4)
	for i, e := range globalCal.entries {
		stats[names[i]] = map[string]any{
			"factor":  e.Factor,
			"samples": e.Samples,
			"active":  e.Samples >= minSamples,
		}
	}
	return stats
}

func (c *Calibrator) record(family Family, estimated, actual int) {
	if estimated <= 0 || actual <= 0 || family < 0 || int(family) >= len(c.entries) {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	e := &c.entries[family]

	// targetFactor = currentFactor * actual / estimated
	// This correctly converges to actual/rawCount regardless of
	// the current calibration state (see package doc).
	targetFactor := e.Factor * float64(actual) / float64(estimated)
	if targetFactor < minFactor {
		targetFactor = minFactor
	}
	if targetFactor > maxFactor {
		targetFactor = maxFactor
	}

	if e.Samples == 0 {
		e.Factor = targetFactor
	} else {
		e.Factor = e.Factor*(1-emaAlpha) + targetFactor*emaAlpha
	}
	e.Samples++
}

func (c *Calibrator) factor(family Family) float64 {
	if family < 0 || int(family) >= len(c.entries) {
		return 1.0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	e := &c.entries[family]
	if e.Samples < minSamples {
		return 1.0
	}
	return e.Factor
}

// ── Persistence ─────────────────────────────────────────────────────────

// calFile is the filename for persisted calibration data.
const calFile = "tokenest-cal.json"

// calPersist is the on-disk format.
type calPersist struct {
	Entries [4]calEntry `json:"entries"`
}

// LoadCalibration loads persisted calibration data from dataDir.
// dataDir is typically ~/.deneb. No-op if file doesn't exist.
func LoadCalibration(dataDir string) {
	path := filepath.Join(dataDir, calFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return // file doesn't exist or unreadable — use defaults
	}
	var p calPersist
	if err := json.Unmarshal(data, &p); err != nil {
		slog.Warn("tokenest: ignoring corrupt calibration file", "path", path, "err", err)
		return
	}
	globalCal.mu.Lock()
	defer globalCal.mu.Unlock()
	for i, e := range p.Entries {
		if e.Factor >= minFactor && e.Factor <= maxFactor && e.Samples > 0 {
			globalCal.entries[i] = e
		}
	}
	slog.Info("tokenest: loaded calibration",
		"path", path,
		"claude", globalCal.entries[FamilyClaude].Factor,
		"openai", globalCal.entries[FamilyOpenAI].Factor,
	)
}

// SaveCalibration persists calibration data to dataDir.
// Call this on graceful shutdown.
func SaveCalibration(dataDir string) error {
	globalCal.mu.RLock()
	p := calPersist{Entries: globalCal.entries}
	globalCal.mu.RUnlock()

	// Only save if we have meaningful data.
	hasData := false
	for _, e := range p.Entries {
		if e.Samples > 0 {
			hasData = true
			break
		}
	}
	if !hasData {
		return nil
	}

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dataDir, calFile)
	return os.WriteFile(path, data, 0o644)
}
