package tokenest

import (
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// saveGlobalCal saves the global calibrator entries and returns a restore func.
// Avoids copying the sync.RWMutex (vet error).
func saveGlobalCal() func() {
	saved := globalCal.entries
	return func() {
		globalCal.mu.Lock()
		globalCal.entries = saved
		globalCal.mu.Unlock()
	}
}

// resetGlobalCal resets the global calibrator to fresh state.
func resetGlobalCal() {
	globalCal.mu.Lock()
	for i := range globalCal.entries {
		globalCal.entries[i] = calEntry{Factor: 1.0}
	}
	globalCal.mu.Unlock()
}

func TestCalibrator_BasicConvergence(t *testing.T) {
	c := newCalibrator()

	// Simulate: raw estimate is 100, but actual is 130 (30% underestimate).
	// In production, estimated = raw * factor, so we simulate that.
	rawBase := 100
	actual := 130
	for i := 0; i < 200; i++ {
		factor := c.entries[FamilyClaude].Factor
		estimated := int(float64(rawBase) * factor)
		if estimated < 1 {
			estimated = 1
		}
		c.record(FamilyClaude, estimated, actual)
	}

	factor := c.factor(FamilyClaude)
	if math.Abs(factor-1.3) > 0.05 {
		t.Errorf("factor = %.3f, want ~1.3", factor)
	}
	t.Logf("converged factor: %.4f after 200 samples", factor)
}

func TestCalibrator_OverestimateCorrection(t *testing.T) {
	c := newCalibrator()

	// Simulate: raw estimate 200, actual 150 (overestimate by 33%).
	rawBase := 200
	actual := 150
	for i := 0; i < 200; i++ {
		factor := c.entries[FamilyOpenAI].Factor
		estimated := int(float64(rawBase) * factor)
		if estimated < 1 {
			estimated = 1
		}
		c.record(FamilyOpenAI, estimated, actual)
	}

	factor := c.factor(FamilyOpenAI)
	if math.Abs(factor-0.75) > 0.05 {
		t.Errorf("factor = %.3f, want ~0.75", factor)
	}
}

func TestCalibrator_MinSamples(t *testing.T) {
	c := newCalibrator()

	// Below minSamples, factor should be 1.0.
	for i := 0; i < minSamples-1; i++ {
		c.record(FamilyClaude, 100, 200)
	}
	if got := c.factor(FamilyClaude); got != 1.0 {
		t.Errorf("factor before minSamples = %.3f, want 1.0", got)
	}

	// One more sample tips it over.
	c.record(FamilyClaude, 100, 200)
	if got := c.factor(FamilyClaude); got == 1.0 {
		t.Error("factor should differ from 1.0 after reaching minSamples")
	}
}

func TestCalibrator_Clamping(t *testing.T) {
	c := newCalibrator()

	// Extreme outlier: actual 10x estimated.
	for i := 0; i < 50; i++ {
		c.record(FamilyGemini, 10, 1000)
	}
	factor := c.factor(FamilyGemini)
	if factor > maxFactor {
		t.Errorf("factor = %.3f, should be clamped to max %.1f", factor, maxFactor)
	}

	// Opposite: actual 0.01x estimated.
	c2 := newCalibrator()
	for i := 0; i < 50; i++ {
		c2.record(FamilyDefault, 1000, 1)
	}
	factor2 := c2.factor(FamilyDefault)
	if factor2 < minFactor {
		t.Errorf("factor = %.3f, should be clamped to min %.1f", factor2, minFactor)
	}
}

func TestCalibrator_FamilyIsolation(t *testing.T) {
	c := newCalibrator()

	// Only train Claude.
	for i := 0; i < 20; i++ {
		c.record(FamilyClaude, 100, 150)
	}

	// OpenAI should be unaffected.
	if got := c.factor(FamilyOpenAI); got != 1.0 {
		t.Errorf("OpenAI factor = %.3f, want 1.0 (untrained)", got)
	}
	if got := c.factor(FamilyClaude); got == 1.0 {
		t.Error("Claude factor should differ from 1.0 (trained)")
	}
}

func TestCalibrator_ConcurrentSafety(t *testing.T) {
	c := newCalibrator()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.record(FamilyClaude, 100, 120)
			_ = c.factor(FamilyClaude)
		}()
	}
	wg.Wait()

	// Should not panic or deadlock. Factor should be reasonable.
	factor := c.factor(FamilyClaude)
	if factor < 0.5 || factor > 3.0 {
		t.Errorf("concurrent factor = %.3f, out of expected range", factor)
	}
}

func TestCalibrator_InvalidInputs(t *testing.T) {
	c := newCalibrator()

	// Zero/negative inputs should be ignored.
	c.record(FamilyClaude, 0, 100)
	c.record(FamilyClaude, 100, 0)
	c.record(FamilyClaude, -1, 100)
	c.record(Family(-1), 100, 100) // invalid family

	if c.entries[FamilyClaude].Samples != 0 {
		t.Error("invalid inputs should not record samples")
	}
}

func TestCount_WithCalibration(t *testing.T) {
	defer saveGlobalCal()()
	resetGlobalCal()

	text := "서울에서 맛있는 김치를 먹었습니다"
	est := ForFamily(FamilyClaude)

	rawResult := est.Count(text)

	// Train: raw underestimates by 50%.
	for i := 0; i < 20; i++ {
		globalCal.record(FamilyClaude, 100, 150)
	}

	calibratedResult := est.Count(text)

	t.Logf("raw=%d, calibrated=%d", rawResult, calibratedResult)
	if calibratedResult <= rawResult {
		t.Errorf("calibrated (%d) should be > raw (%d) when factor > 1", calibratedResult, rawResult)
	}
}

func TestPersistence_SaveLoad(t *testing.T) {
	dir := t.TempDir()

	defer saveGlobalCal()()

	// Train some data (simulate production pattern: estimated = raw * factor).
	resetGlobalCal()
	for i := 0; i < 100; i++ {
		factor := globalCal.entries[FamilyClaude].Factor
		estimated := int(100 * factor)
		if estimated < 1 {
			estimated = 1
		}
		globalCal.record(FamilyClaude, estimated, 140)
	}
	claudeFactor := globalCal.factor(FamilyClaude)

	// Save.
	if err := SaveCalibration(dir); err != nil {
		t.Fatalf("SaveCalibration: %v", err)
	}

	// Verify file exists.
	path := filepath.Join(dir, calFile)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("calibration file not found: %v", err)
	}

	// Reset and load.
	resetGlobalCal()
	if got := globalCal.factor(FamilyClaude); got != 1.0 {
		t.Fatalf("after reset, factor should be 1.0, got %.3f", got)
	}

	LoadCalibration(dir)
	loaded := globalCal.factor(FamilyClaude)

	if math.Abs(loaded-claudeFactor) > 0.01 {
		t.Errorf("loaded factor %.4f != saved %.4f", loaded, claudeFactor)
	}
	t.Logf("saved=%.4f, loaded=%.4f", claudeFactor, loaded)
}



func TestRecordFeedback_Global(t *testing.T) {
	defer saveGlobalCal()()
	resetGlobalCal()

	// RecordFeedback should update the global calibrator.
	for i := 0; i < 20; i++ {
		factor := globalCal.entries[FamilyClaude].Factor
		estimated := int(100 * factor)
		if estimated < 1 {
			estimated = 1
		}
		RecordFeedback(FamilyClaude, estimated, 120)
	}
	factor := CorrectionFactor(FamilyClaude)
	if factor == 1.0 {
		t.Error("CorrectionFactor should differ from 1.0 after training")
	}
	t.Logf("global factor: %.4f", factor)
}

func TestCalibrationStats(t *testing.T) {
	defer saveGlobalCal()()
	resetGlobalCal()
	for i := 0; i < 20; i++ {
		factor := globalCal.entries[FamilyClaude].Factor
		estimated := int(100 * factor)
		if estimated < 1 {
			estimated = 1
		}
		RecordFeedback(FamilyClaude, estimated, 120)
	}

	stats := CalibrationStats()
	claude := stats["claude"].(map[string]any)
	if claude["samples"].(int) != 20 {
		t.Errorf("samples = %v, want 20", claude["samples"])
	}
	if !claude["active"].(bool) {
		t.Error("claude calibration should be active")
	}
	openai := stats["openai"].(map[string]any)
	if openai["active"].(bool) {
		t.Error("openai calibration should not be active (no samples)")
	}
}

// TestCalibrator_MathCorrectness verifies that the calibration math
// converges correctly even when the factor changes between recordings.
func TestCalibrator_MathCorrectness(t *testing.T) {
	c := newCalibrator()

	// The "true" ratio is 1.4 (raw estimate * 1.4 = actual).
	// Simulate varying estimates (as factor evolves):
	rawBase := 100
	trueActual := 140

	for i := 0; i < 200; i++ {
		factor := c.factor(FamilyClaude)
		if factor == 1.0 && c.entries[FamilyClaude].Samples >= minSamples {
			// Apply factor only after minSamples.
			factor = c.entries[FamilyClaude].Factor
		}
		estimated := int(float64(rawBase) * factor)
		if estimated < 1 {
			estimated = 1
		}
		c.record(FamilyClaude, estimated, trueActual)
	}

	factor := c.factor(FamilyClaude)
	// Should converge to ~1.4.
	if math.Abs(factor-1.4) > 0.05 {
		t.Errorf("factor = %.4f, want ~1.4", factor)
	}
	t.Logf("math convergence test: factor=%.4f (target=1.4)", factor)
}
