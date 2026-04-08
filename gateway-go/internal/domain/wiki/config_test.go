package wiki

import (
	"os"
	"testing"
)


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

