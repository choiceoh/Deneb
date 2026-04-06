// Package rlm implements Recursive Language Model context externalization.
// Instead of Go pre-assembling context (Vega/Aurora) into the prompt,
// RLM provides tools that let the LLM fetch data on-demand.
package rlm

import (
	"os"
	"strconv"
	"sync"
)

// Config holds RLM configuration.
// RLM is always active — there is no Enabled flag.
type Config struct {
	// MaxSubSpawns is the maximum number of prompts in a single spawn_batch call.
	MaxSubSpawns int
	// SubMaxTokens is the default max_tokens for sub-LLM calls.
	// 0 means no limit (use LLM client default).
	SubMaxTokens int
	// SubMaxToolCalls is the maximum tool calls a sub-LLM can make per run.
	SubMaxToolCalls int
	// TotalTokenBudget is the per-request token budget across main + all sub-LLM calls.
	// 0 means unlimited (default). Only set when you need a hard cap.
	TotalTokenBudget int

	// FreshTailCount is the number of recent messages included in the prompt.
	// Older messages are accessible via the REPL's context variable.
	FreshTailCount int
	// RecursiveDepthLimit caps how deep rlm_query() recursion can go.
	RecursiveDepthLimit int
	// REPLTimeoutSec is the per-execution timeout for Starlark code.
	REPLTimeoutSec int

	// ── Independent iteration loop (inspired by alexzhang13/rlm) ──

	// MaxIterations is the max LLM→code→execute cycles before forced fallback.
	MaxIterations int
	// CompactionThresholdPct is the fraction of ModelContextLimit at which
	// older iterations are summarized to reclaim context space (default: 0.85).
	CompactionThresholdPct float64
	// ModelContextLimit is the estimated context window size in tokens
	// for the model being used (default: 200000 for 200K+ context models).
	ModelContextLimit int
	// MaxConsecutiveErrors is the consecutive REPL errors before termination.
	MaxConsecutiveErrors int
	// FallbackEnabled generates a best-effort answer when iterations exhaust.
	FallbackEnabled bool
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
		cachedConfig = Config{
			MaxSubSpawns:        envInt("DENEB_RLM_MAX_SUB_SPAWNS", 10),
			SubMaxTokens:        envInt("DENEB_RLM_SUB_MAX_TOKENS", 0),
			SubMaxToolCalls:     envInt("DENEB_RLM_SUB_MAX_TOOL_CALLS", 5),
			TotalTokenBudget:    envInt("DENEB_RLM_TOTAL_TOKEN_BUDGET", 0),
			FreshTailCount:      envInt("DENEB_RLM_FRESH_TAIL", 48),
			RecursiveDepthLimit: envInt("DENEB_RLM_MAX_DEPTH", 3),
			REPLTimeoutSec:      envInt("DENEB_RLM_REPL_TIMEOUT", 30),

			MaxIterations:          envInt("DENEB_RLM_MAX_ITERATIONS", 30),
			CompactionThresholdPct: envFloat("DENEB_RLM_COMPACTION_PCT", 0.85),
			ModelContextLimit:      envInt("DENEB_RLM_MODEL_CONTEXT", 200000),
			MaxConsecutiveErrors:   envInt("DENEB_RLM_MAX_ERRORS", 5),
			FallbackEnabled:        envBool("DENEB_RLM_FALLBACK", true),
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

func envFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 || f > 1 {
		return def
	}
	return f
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
	if err != nil || n < 0 {
		return def
	}
	return n
}
