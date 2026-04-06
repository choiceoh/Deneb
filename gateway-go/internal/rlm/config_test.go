package rlm

import (
	"testing"
)

func TestConfigFromEnv_Defaults(t *testing.T) {
	ResetConfigForTest()
	t.Setenv("DENEB_RLM_ENABLED", "")

	cfg := ConfigFromEnv()
	if cfg.Enabled {
		t.Error("expected Enabled=false by default")
	}
	if cfg.SkipKnowledge {
		t.Error("expected SkipKnowledge=false when disabled")
	}
	if cfg.SubLLMEnabled {
		t.Error("expected SubLLMEnabled=false by default")
	}
	if cfg.TotalTokenBudget != 50000 {
		t.Errorf("expected TotalTokenBudget=50000, got %d", cfg.TotalTokenBudget)
	}
}

func TestConfigFromEnv_Enabled(t *testing.T) {
	ResetConfigForTest()
	t.Setenv("DENEB_RLM_ENABLED", "true")

	cfg := ConfigFromEnv()
	if !cfg.Enabled {
		t.Error("expected Enabled=true")
	}
	if !cfg.SkipKnowledge {
		t.Error("expected SkipKnowledge=true when enabled (default)")
	}
}

func TestConfigFromEnv_SubLLM(t *testing.T) {
	ResetConfigForTest()
	t.Setenv("DENEB_RLM_ENABLED", "true")
	t.Setenv("DENEB_RLM_SUB_LLM_ENABLED", "true")
	t.Setenv("DENEB_RLM_TOTAL_TOKEN_BUDGET", "100000")

	cfg := ConfigFromEnv()
	if !cfg.SubLLMEnabled {
		t.Error("expected SubLLMEnabled=true")
	}
	if cfg.TotalTokenBudget != 100000 {
		t.Errorf("expected TotalTokenBudget=100000, got %d", cfg.TotalTokenBudget)
	}
}

func TestConfigFromEnv_SkipKnowledgeOverride(t *testing.T) {
	ResetConfigForTest()
	t.Setenv("DENEB_RLM_ENABLED", "true")
	t.Setenv("DENEB_RLM_SKIP_KNOWLEDGE", "false")

	cfg := ConfigFromEnv()
	if cfg.SkipKnowledge {
		t.Error("expected SkipKnowledge=false with explicit override")
	}
}

func TestConfigFromEnv_InvalidValues(t *testing.T) {
	ResetConfigForTest()
	t.Setenv("DENEB_RLM_ENABLED", "notabool")
	t.Setenv("DENEB_RLM_TOTAL_TOKEN_BUDGET", "notanint")

	cfg := ConfigFromEnv()
	if cfg.Enabled {
		t.Error("expected Enabled=false for invalid bool")
	}
	if cfg.TotalTokenBudget != 50000 {
		t.Errorf("expected default TotalTokenBudget=50000 for invalid int, got %d", cfg.TotalTokenBudget)
	}
}

func TestConfigFromEnv_NegativeValues(t *testing.T) {
	ResetConfigForTest()
	t.Setenv("DENEB_RLM_ENABLED", "true")
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
