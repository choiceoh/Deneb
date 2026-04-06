// Package rlm implements Recursive Language Model context externalization.
// Instead of Go pre-assembling context (Vega/Aurora) into the prompt,
// RLM provides tools that let the LLM fetch data on-demand.
package rlm

import (
	"os"
	"strconv"
	"sync"
)

// Config holds RLM feature configuration.
type Config struct {
	// Enabled activates RLM tools (projects_*, memory_recall).
	Enabled bool
	// SkipKnowledge disables the knowledge prefetch phase when RLM is active.
	// Defaults to true when Enabled is true.
	SkipKnowledge bool
	// SubLLMEnabled activates Phase 2 sub-LLM spawning tools (llm_spawn, llm_spawn_batch).
	SubLLMEnabled bool
	// MaxSubSpawns is the maximum number of prompts in a single spawn_batch call.
	MaxSubSpawns int
	// SubMaxTokens is the default max_tokens for sub-LLM calls.
	SubMaxTokens int
	// SubMaxToolCalls is the maximum tool calls a sub-LLM can make per run.
	SubMaxToolCalls int
	// TotalTokenBudget is the per-request token budget across main + all sub-LLM calls.
	TotalTokenBudget int
}

var (
	configOnce   sync.Once
	cachedConfig Config
)

// ConfigFromEnv reads RLM configuration from environment variables.
// The result is cached after the first call so that tool registration
// (startup) and per-request prompt injection always see the same config.
func ConfigFromEnv() Config {
	configOnce.Do(func() {
		enabled := envBool("DENEB_RLM_ENABLED", false)
		cachedConfig = Config{
			Enabled:          enabled,
			SkipKnowledge:    enabled && envBool("DENEB_RLM_SKIP_KNOWLEDGE", true),
			SubLLMEnabled:    enabled && envBool("DENEB_RLM_SUB_LLM_ENABLED", false),
			MaxSubSpawns:     envInt("DENEB_RLM_MAX_SUB_SPAWNS", 10),
			SubMaxTokens:     envInt("DENEB_RLM_SUB_MAX_TOKENS", 500),
			SubMaxToolCalls:  envInt("DENEB_RLM_SUB_MAX_TOOL_CALLS", 5),
			TotalTokenBudget: envInt("DENEB_RLM_TOTAL_TOKEN_BUDGET", 50000),
		}
	})
	return cachedConfig
}

// ResetConfigForTest resets the cached config so tests can set new env vars.
// Must not be called in production code.
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
