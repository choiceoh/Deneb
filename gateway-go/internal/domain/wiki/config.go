// Package wiki implements a Karpathy-style LLM Wiki knowledge base.
// Instead of storing facts in SQL with vector/FTS retrieval,
// knowledge is pre-compiled into structured markdown pages that
// LLMs can read directly via tools.
package wiki

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// Config holds wiki feature configuration.
type Config struct {
	// Enabled activates the wiki knowledge base system.
	Enabled bool
	// Dir is the wiki root directory (default: ~/.deneb/wiki).
	Dir string
	// DiaryDir is the diary directory for raw daily logs (default: ~/.deneb/memory/diary).
	DiaryDir string
	// MaxPageBytes is the maximum page size before the dreaming cycle splits a
	// page into H2 sub-pages (store_split.go). Default 32 KB.
	//
	// Lowered 50->32 KB on the Infini Memory finding (arXiv:2606.10677, Table 3):
	// retrieval over topic documents peaks near ~5000 tokens and the curve is
	// ASYMMETRIC — too-large hurts more (-5.3 pts at 1.4x optimal) than too-small
	// (-2.0 pts at 0.2x). Deneb's old 50 KB cap (~7-12K tokens for mixed
	// Korean+Latin markdown) sat on the costly too-large side; 32 KB (~5.5-7K
	// tokens) moves toward the measured optimum without over-fragmenting curated
	// project pages.
	//
	// Falsifiable prediction: this does not regress overall wiki recall and
	// improves retrieval precision on the largest (most-split) pages, because a
	// focused sub-page is a larger fraction of the matched terms than the same
	// fact buried in a multi-topic page (BM25 length normalization). Verify with
	// the split-sensitivity sweep (search_split_sweep_test.go, the Deneb-stack
	// version of the paper's Table 3) + DGX recall-metric; if recall regresses,
	// revert via DENEB_WIKI_MAX_PAGE_BYTES (Deneb's stack/corpus optimum differs,
	// or splitting fragmented a coherent page).
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
		// Derive defaults from the resolved state dir (honors DENEB_STATE_DIR) so a
		// test/dev gateway with an isolated state dir keeps its wiki + diary out of
		// prod ~/.deneb. Prod sets DENEB_STATE_DIR=~/.deneb, so the path is identical
		// there. DENEB_WIKI_DIR / DENEB_WIKI_DIARY_DIR still override explicitly.
		stateDir := config.ResolveStateDir()
		defaultDir := filepath.Join(stateDir, "wiki")
		defaultDiary := filepath.Join(stateDir, "memory", "diary")

		cachedConfig = Config{
			Enabled:            envBool("DENEB_WIKI_ENABLED", true),
			Dir:                envStr("DENEB_WIKI_DIR", defaultDir),
			DiaryDir:           envStr("DENEB_WIKI_DIARY_DIR", defaultDiary),
			MaxPageBytes:       envInt("DENEB_WIKI_MAX_PAGE_BYTES", 32*1024),
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
