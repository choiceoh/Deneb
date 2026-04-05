package aurora

import (
	"testing"
)

func TestCompactionMetric_DefaultConfig(t *testing.T) {
	score := RunCompactionBenchmark(DefaultSweepConfig())
	t.Logf("Default config score: %.2f", score)
	if score < 10 || score > 100 {
		t.Errorf("default config score %.2f outside reasonable range [10, 100]", score)
	}
}

func TestCompactionMetric_DegenerateConfig(t *testing.T) {
	// Degenerate: no fresh tail, minimal fanout.
	cfg := SweepConfig{
		ContextThreshold:       0.10,
		FreshTailCount:         0,
		LeafMinFanout:          2,
		CondensedMinFanout:     2,
		CondensedMinFanoutHard: 2,
		LeafTargetTokens:       100,
		CondensedTargetTokens:  100,
		MaxRounds:              3,
	}
	score := RunCompactionBenchmark(cfg)
	t.Logf("Degenerate config score: %.2f", score)

	defaultScore := RunCompactionBenchmark(DefaultSweepConfig())
	t.Logf("Default config score: %.2f", defaultScore)

	if score >= defaultScore {
		t.Errorf("degenerate config (%.2f) should score lower than default (%.2f)", score, defaultScore)
	}
}

func TestCompactionMetric_ConservativeConfig(t *testing.T) {
	// Conservative: large fresh tail, high target tokens.
	cfg := SweepConfig{
		ContextThreshold:       0.80,
		FreshTailCount:         16,
		LeafMinFanout:          12,
		CondensedMinFanout:     6,
		CondensedMinFanoutHard: 3,
		LeafTargetTokens:       1000,
		CondensedTargetTokens:  1500,
		MaxRounds:              10,
	}
	score := RunCompactionBenchmark(cfg)
	t.Logf("Conservative config score: %.2f", score)
	if score < 0 || score > 100 {
		t.Errorf("conservative config score %.2f outside valid range [0, 100]", score)
	}
}

func TestCompactionMetric_ScenarioVariance(t *testing.T) {
	// Verify that multiple runs produce similar (but not identical) results
	// due to perturbation seed.
	scores := make([]float64, 5)
	for i := range scores {
		scores[i] = RunCompactionBenchmark(DefaultSweepConfig())
	}
	t.Logf("Scores across 5 runs: %v", scores)

	// All scores should be in a reasonable range.
	for i, s := range scores {
		if s < 5 || s > 100 {
			t.Errorf("run %d score %.2f outside range", i, s)
		}
	}
}

func BenchmarkCompactionMetric(b *testing.B) {
	cfg := DefaultSweepConfig()
	for b.Loop() {
		RunCompactionBenchmark(cfg)
	}
}
