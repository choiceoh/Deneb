package wiki

import (
	"os"
	"testing"
)

func TestConfigFromEnv_Defaults(t *testing.T) {
	ResetConfigForTest()

	// Clear all wiki env vars.
	for _, key := range []string{
		"DENEB_WIKI_ENABLED", "DENEB_WIKI_DIR",
		"DENEB_WIKI_DIARY_DIR", "DENEB_WIKI_MAX_PAGE_BYTES",
		"DENEB_WIKI_TIER1_MIN_IMPORTANCE",
	} {
		os.Unsetenv(key)
	}

	cfg := ConfigFromEnv()
	if cfg.Enabled {
		t.Error("Enabled should be false by default")
	}
	if cfg.MaxPageBytes != 50*1024 {
		t.Errorf("MaxPageBytes = %d, want %d", cfg.MaxPageBytes, 50*1024)
	}
	if cfg.Tier1MinImportance != 0.85 {
		t.Errorf("Tier1MinImportance = %f, want 0.85", cfg.Tier1MinImportance)
	}
}

func TestConfigFromEnv_Overrides(t *testing.T) {
	ResetConfigForTest()

	os.Setenv("DENEB_WIKI_ENABLED", "true")
	os.Setenv("DENEB_WIKI_DIR", "/tmp/test-wiki")
	os.Setenv("DENEB_WIKI_MAX_PAGE_BYTES", "102400")
	os.Setenv("DENEB_WIKI_TIER1_MIN_IMPORTANCE", "0.90")
	defer func() {
		os.Unsetenv("DENEB_WIKI_ENABLED")
		os.Unsetenv("DENEB_WIKI_DIR")
		os.Unsetenv("DENEB_WIKI_MAX_PAGE_BYTES")
		os.Unsetenv("DENEB_WIKI_TIER1_MIN_IMPORTANCE")
	}()

	cfg := ConfigFromEnv()
	if !cfg.Enabled {
		t.Error("Enabled should be true")
	}
	if cfg.Dir != "/tmp/test-wiki" {
		t.Errorf("Dir = %q", cfg.Dir)
	}
	if cfg.MaxPageBytes != 102400 {
		t.Errorf("MaxPageBytes = %d", cfg.MaxPageBytes)
	}
	if cfg.Tier1MinImportance != 0.90 {
		t.Errorf("Tier1MinImportance = %f", cfg.Tier1MinImportance)
	}
}

func TestConfigFromEnv_Cached(t *testing.T) {
	ResetConfigForTest()

	os.Setenv("DENEB_WIKI_ENABLED", "true")
	cfg1 := ConfigFromEnv()

	os.Setenv("DENEB_WIKI_ENABLED", "false")
	cfg2 := ConfigFromEnv()

	os.Unsetenv("DENEB_WIKI_ENABLED")

	// Should be cached — both return the same value.
	if cfg1.Enabled != cfg2.Enabled {
		t.Error("ConfigFromEnv should be cached after first call")
	}
}
