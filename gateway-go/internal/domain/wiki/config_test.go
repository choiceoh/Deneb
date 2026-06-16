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

// With no explicit DENEB_WIKI_DIR, the wiki + diary dirs follow DENEB_STATE_DIR so
// a test/dev gateway with an isolated state dir keeps its wiki out of prod ~/.deneb.
func TestConfigFromEnv_DefaultsFollowStateDir(t *testing.T) {
	ResetConfigForTest()
	os.Setenv("DENEB_STATE_DIR", "/tmp/deneb-iso-test")
	os.Unsetenv("DENEB_WIKI_DIR")
	os.Unsetenv("DENEB_WIKI_DIARY_DIR")
	defer func() {
		os.Unsetenv("DENEB_STATE_DIR")
		ResetConfigForTest()
	}()

	cfg := ConfigFromEnv()
	if cfg.Dir != "/tmp/deneb-iso-test/wiki" {
		t.Errorf("Dir = %q, want /tmp/deneb-iso-test/wiki", cfg.Dir)
	}
	if cfg.DiaryDir != "/tmp/deneb-iso-test/memory/diary" {
		t.Errorf("DiaryDir = %q, want /tmp/deneb-iso-test/memory/diary", cfg.DiaryDir)
	}
}
