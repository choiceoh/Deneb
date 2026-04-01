// query_config.go captures immutable configuration snapshotted once at query
// entry. Separating these from per-iteration state makes the loop easier to
// reason about: a pure reducer can take (state, event, config) where config
// is plain, frozen data.
//
// Inspired by Claude Code's query/config.ts pattern.
package chat

import "time"

// QueryConfig holds immutable values snapshotted once per query() invocation.
// It must NOT contain mutable references or per-turn state.
type QueryConfig struct {
	// SessionKey identifies the conversation.
	SessionKey string

	// Model is the resolved model ID for this query.
	Model string

	// ProviderID is the resolved provider (e.g., "anthropic", "google").
	ProviderID string

	// TokenBudget is the context window token budget.
	TokenBudget uint64

	// MaxTurns is the maximum agent loop iterations.
	MaxTurns int

	// MaxOutputTokens is the max tokens per LLM response.
	MaxOutputTokens int

	// AgentTimeout is the total time allowed for the agent run.
	AgentTimeout time.Duration

	// MaxCompactionRetries limits compaction retry attempts per query.
	MaxCompactionRetries int

	// WorkspaceDir is the resolved workspace directory.
	WorkspaceDir string

	// SnapshotTime is when this config was created.
	SnapshotTime time.Time
}

// DefaultQueryConfig returns a QueryConfig with sensible defaults.
// Callers should override fields from RunParams before use.
func DefaultQueryConfig() QueryConfig {
	return QueryConfig{
		TokenBudget:          defaultTokenBudget,
		MaxTurns:             defaultMaxTurns,
		MaxOutputTokens:      defaultMaxTokens,
		AgentTimeout:         defaultAgentTimeout,
		MaxCompactionRetries: maxCompactionRetries,
		SnapshotTime:         time.Now(),
	}
}

// BuildQueryConfig creates a QueryConfig from RunParams and resolved values.
func BuildQueryConfig(params RunParams, model, providerID, workspaceDir string) QueryConfig {
	cfg := DefaultQueryConfig()
	cfg.SessionKey = params.SessionKey
	cfg.Model = model
	cfg.ProviderID = providerID
	cfg.WorkspaceDir = workspaceDir
	if params.MaxTokens != nil && *params.MaxTokens > 0 {
		cfg.MaxOutputTokens = *params.MaxTokens
	}
	return cfg
}
