package model

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
