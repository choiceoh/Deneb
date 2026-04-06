package rlm

import (
	"testing"
)

func TestConfigFromEnv_Defaults(t *testing.T) {
	ResetConfigForTest()

	cfg := ConfigFromEnv()
	if cfg.TotalTokenBudget != 50000 {
		t.Errorf("expected TotalTokenBudget=50000, got %d", cfg.TotalTokenBudget)
	}
	if cfg.MaxIterations != 25 {
		t.Errorf("expected MaxIterations=25, got %d", cfg.MaxIterations)
	}
	if cfg.FreshTailCount != 48 {
		t.Errorf("expected FreshTailCount=48, got %d", cfg.FreshTailCount)
	}
	if !cfg.FallbackEnabled {
		t.Error("expected FallbackEnabled=true by default")
	}
}

func TestConfigFromEnv_CustomValues(t *testing.T) {
	ResetConfigForTest()
	t.Setenv("DENEB_RLM_TOTAL_TOKEN_BUDGET", "100000")
	t.Setenv("DENEB_RLM_MAX_ITERATIONS", "50")
	t.Setenv("DENEB_RLM_COMPACTION_THRESHOLD", "60000")

	cfg := ConfigFromEnv()
	if cfg.TotalTokenBudget != 100000 {
		t.Errorf("expected TotalTokenBudget=100000, got %d", cfg.TotalTokenBudget)
	}
	if cfg.MaxIterations != 50 {
		t.Errorf("expected MaxIterations=50, got %d", cfg.MaxIterations)
	}
	if cfg.CompactionThreshold != 60000 {
		t.Errorf("expected CompactionThreshold=60000, got %d", cfg.CompactionThreshold)
	}
}

func TestConfigFromEnv_InvalidValues(t *testing.T) {
	ResetConfigForTest()
	t.Setenv("DENEB_RLM_TOTAL_TOKEN_BUDGET", "notanint")

	cfg := ConfigFromEnv()
	if cfg.TotalTokenBudget != 50000 {
		t.Errorf("expected default TotalTokenBudget=50000 for invalid int, got %d", cfg.TotalTokenBudget)
	}
}

func TestConfigFromEnv_NegativeValues(t *testing.T) {
	ResetConfigForTest()
	t.Setenv("DENEB_RLM_TOTAL_TOKEN_BUDGET", "-1000")
	t.Setenv("DENEB_RLM_MAX_SUB_SPAWNS", "-5")

	cfg := ConfigFromEnv()
	if cfg.TotalTokenBudget != 50000 {
		t.Errorf("expected default TotalTokenBudget=50000 for negative, got %d", cfg.TotalTokenBudget)
	}
	if cfg.MaxSubSpawns != 10 {
		t.Errorf("expected default MaxSubSpawns=10 for negative, got %d", cfg.MaxSubSpawns)
	}
}
