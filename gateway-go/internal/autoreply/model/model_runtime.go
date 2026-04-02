package model

import "unicode/utf8"

// ModelRuntimeInfo holds capacity and token information for a model.
type ModelRuntimeInfo struct {
	Provider         string
	Model            string
	MaxTokens        int
	ContextTokens    int
	ReasoningCapable bool
}

// DefaultContextTokens is the default context token budget.
const DefaultContextTokens = 128000

// DefaultMaxTokens is the default max output tokens.
const DefaultMaxTokens = 8192

// ResolveContextTokens returns the effective context token limit.
func ResolveContextTokens(configured, modelDefault int) int {
	if configured > 0 {
		return configured
	}
	if modelDefault > 0 {
		return modelDefault
	}
	return DefaultContextTokens
}

// ResolveMaxTokens returns the effective max output token limit.
func ResolveMaxTokens(configured, modelDefault int) int {
	if configured > 0 {
		return configured
	}
	if modelDefault > 0 {
		return modelDefault
	}
	return DefaultMaxTokens
}

// EstimateTokens provides a rough token estimate for text.
// Uses Unicode rune count divided by 2, calibrated for Korean BPE (~2 runes/token).
// Matches the canonical estimator in chat/prompt.EstimateTokens.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	n := utf8.RuneCountInString(text) / 2
	if n < 1 {
		return 1
	}
	return n
}
