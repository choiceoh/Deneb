package hindsight

import "testing"

func TestConfigFromEnvDisabledWithoutURL(t *testing.T) {
	ResetConfigForTest()
	if ConfigFromEnv().Enabled() {
		t.Fatal("expected disabled when DENEB_HINDSIGHT_URL is unset")
	}
}

func TestConfigFromEnvDefaults(t *testing.T) {
	ResetConfigForTest()
	t.Setenv("DENEB_HINDSIGHT_URL", "http://127.0.0.1:8888/")
	cfg := ConfigFromEnv()
	if !cfg.Enabled() {
		t.Fatal("expected enabled with URL set")
	}
	if cfg.BaseURL != "http://127.0.0.1:8888" {
		t.Fatalf("trailing slash not trimmed: %q", cfg.BaseURL)
	}
	if cfg.BankID != defaultBankID {
		t.Fatalf("bank default: got %q want %q", cfg.BankID, defaultBankID)
	}
	if cfg.Budget != defaultBudget {
		t.Fatalf("budget default: got %q want %q", cfg.Budget, defaultBudget)
	}
	if !cfg.Retain {
		t.Fatal("retain should default to true")
	}
	if cfg.RecallMaxTokens != defaultRecallMaxTokens {
		t.Fatalf("recall max tokens default: %d", cfg.RecallMaxTokens)
	}
}

func TestConfigFromEnvOverrides(t *testing.T) {
	ResetConfigForTest()
	t.Setenv("DENEB_HINDSIGHT_URL", "http://spark:8888")
	t.Setenv("DENEB_HINDSIGHT_BANK_ID", "deneb-test")
	t.Setenv("DENEB_HINDSIGHT_BUDGET", "HIGH")
	t.Setenv("DENEB_HINDSIGHT_RETAIN", "false")
	t.Setenv("DENEB_HINDSIGHT_RECALL_MAX_TOKENS", "256")
	cfg := ConfigFromEnv()
	if cfg.BankID != "deneb-test" {
		t.Fatalf("bank override: %q", cfg.BankID)
	}
	if cfg.Budget != "high" {
		t.Fatalf("budget should be normalized to lowercase: %q", cfg.Budget)
	}
	if cfg.Retain {
		t.Fatal("retain override to false ignored")
	}
	if cfg.RecallMaxTokens != 256 {
		t.Fatalf("recall max tokens override: %d", cfg.RecallMaxTokens)
	}
}

func TestNormalizeBudgetFallback(t *testing.T) {
	if got := normalizeBudget("nonsense"); got != defaultBudget {
		t.Fatalf("invalid budget should fall back to default: %q", got)
	}
	if got := normalizeBudget("  Mid "); got != "mid" {
		t.Fatalf("budget should trim and lowercase: %q", got)
	}
}
