// Package hindsight integrates the self-hosted Hindsight memory bank
// (https://github.com/vectorize-io/hindsight) as a cross-session memory
// provider for Deneb. Hindsight runs as a local Docker service on the DGX
// Spark and exposes a plain HTTP API on port 8888; Deneb talks to it directly.
//
// The integration is dormant unless DENEB_HINDSIGHT_URL is set: with no URL
// the client constructor returns nil and every call site degrades to a no-op,
// so the chat pipeline behaves exactly as before.
package hindsight

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	defaultBankID          = "deneb"
	defaultBudget          = "low"
	defaultRecallMaxTokens = 1024
)

// Config holds Hindsight memory provider configuration.
type Config struct {
	// BaseURL is the Hindsight API root, e.g. "http://127.0.0.1:8888".
	// Empty disables the entire integration.
	BaseURL string
	// BankID is the memory bank ("brain") Deneb reads from and writes to.
	// Defaults to "deneb" so Deneb's memories stay isolated from other
	// agents sharing the same Hindsight instance (e.g. the "hermes" bank).
	BankID string
	// APIKey is an optional bearer token. Self-hosted instances usually
	// need none; Hindsight Cloud uses an "hsk-" prefixed key.
	APIKey string
	// Retain enables the write path (storing completed turns). When false
	// Deneb only reads from the bank.
	Retain bool
	// Budget is the recall thoroughness level: "low", "mid", or "high".
	Budget string
	// RecallMaxTokens caps the token size of a recall response.
	RecallMaxTokens int
}

// Enabled reports whether the integration is configured.
func (c Config) Enabled() bool { return strings.TrimSpace(c.BaseURL) != "" }

var (
	configOnce   sync.Once
	cachedConfig Config
)

// ConfigFromEnv reads Hindsight configuration from environment variables.
// The result is cached after the first call.
func ConfigFromEnv() Config {
	configOnce.Do(func() {
		cachedConfig = Config{
			BaseURL:         strings.TrimRight(strings.TrimSpace(os.Getenv("DENEB_HINDSIGHT_URL")), "/"),
			BankID:          envStr("DENEB_HINDSIGHT_BANK_ID", defaultBankID),
			APIKey:          strings.TrimSpace(os.Getenv("DENEB_HINDSIGHT_API_KEY")),
			Retain:          envBool("DENEB_HINDSIGHT_RETAIN", true),
			Budget:          normalizeBudget(os.Getenv("DENEB_HINDSIGHT_BUDGET")),
			RecallMaxTokens: envInt("DENEB_HINDSIGHT_RECALL_MAX_TOKENS", defaultRecallMaxTokens),
		}
	})
	return cachedConfig
}

// ResetConfigForTest resets the cached config so tests can set new env vars.
func ResetConfigForTest() {
	configOnce = sync.Once{}
	cachedConfig = Config{}
}

// normalizeBudget lowercases the budget and falls back to the default for
// any value Hindsight does not recognize.
func normalizeBudget(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "low", "mid", "high":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return defaultBudget
	}
}

func envStr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
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
