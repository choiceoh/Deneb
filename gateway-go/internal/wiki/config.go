// Package wiki implements a Karpathy-style LLM Wiki knowledge base.
// Instead of storing facts in SQL with vector/FTS retrieval,
// knowledge is pre-compiled into structured markdown pages that
// LLMs can read directly via tools.
package wiki

import (
	"os"
	"strconv"
	"sync"
)

// Config holds wiki feature configuration.
type Config struct {
	// Enabled activates the wiki knowledge base system.
	Enabled bool
	// Dir is the wiki root directory (default: ~/.deneb/wiki).
	Dir string
	// DiaryDir is the diary directory for raw daily logs (default: ~/.deneb/memory/diary).
	DiaryDir string
	// MaxPageBytes is the maximum page size before the dreaming cycle
	// triggers a split (default: 50 KB).
	MaxPageBytes int
	// Tier1MinImportance is the minimum importance for Tier-1 auto-injection
	// into the system prompt (default: 0.85).
	Tier1MinImportance float64
}

var (
	configOnce   sync.Once
	cachedConfig Config
)

// ConfigFromEnv reads wiki configuration from environment variables.
// The result is cached after the first call.
func ConfigFromEnv() Config {
	configOnce.Do(func() {
		home, _ := os.UserHomeDir()
		defaultDir := home + "/.deneb/wiki"
		defaultDiary := home + "/.deneb/memory/diary"

		cachedConfig = Config{
			Enabled:            envBool("DENEB_WIKI_ENABLED", false),
			Dir:                envStr("DENEB_WIKI_DIR", defaultDir),
			DiaryDir:           envStr("DENEB_WIKI_DIARY_DIR", defaultDiary),
			MaxPageBytes:       envInt("DENEB_WIKI_MAX_PAGE_BYTES", 50*1024),
			Tier1MinImportance: envFloat("DENEB_WIKI_TIER1_MIN_IMPORTANCE", 0.85),
		}
	})
	return cachedConfig
}

// ResetConfigForTest resets the cached config so tests can set new env vars.
func ResetConfigForTest() {
	configOnce = sync.Once{}
	cachedConfig = Config{}
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envStr(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}
