// query_deps.go defines injectable dependencies for the chat query loop.
//
// Passing a QueryDeps struct into the query loop (instead of importing
// modules directly) enables:
//   - Tests to inject fakes without module-level spy boilerplate
//   - Clear documentation of what the query loop actually needs
//   - Future step() extraction into a pure reducer
//
// Scope is intentionally narrow — only dependencies that are commonly
// mocked in tests. Expand carefully.
//
// Inspired by Claude Code's query/deps.ts pattern.
package chat

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	compact "github.com/choiceoh/deneb/gateway-go/internal/chat/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// QueryDeps holds injectable dependencies for the chat query loop.
// Each field has a production implementation and can be overridden in tests.
type QueryDeps struct {
	// CompleteModel sends a chat request to the LLM and returns text.
	CompleteModel func(ctx context.Context, client *llm.Client, req llm.ChatRequest) (string, error)

	// StreamModel sends a streaming chat request and returns an event channel.
	StreamModel func(ctx context.Context, client *llm.Client, req llm.ChatRequest) (<-chan llm.StreamEvent, error)

	// Microcompact prunes old tool results to save tokens.
	Microcompact func(messages []llm.Message, now time.Time) ([]llm.Message, compact.MicrocompactResult)

	// EvaluateCompaction checks if full compaction is needed.
	EvaluateCompaction func(cfg aurora.SweepConfig, storedTokens, liveTokens, tokenBudget uint64) (bool, string, error)

	// GenerateUUID produces a unique identifier (for tool use IDs, etc.).
	GenerateUUID func() string
}

// ProductionQueryDeps returns the production implementations.
func ProductionQueryDeps() QueryDeps {
	return QueryDeps{
		CompleteModel:      productionComplete,
		StreamModel:        productionStream,
		Microcompact:       compact.MicrocompactMessages,
		EvaluateCompaction: aurora.EvaluateCompaction,
		GenerateUUID:       generateUUID,
	}
}

// productionComplete is the production non-streaming LLM call.
func productionComplete(ctx context.Context, client *llm.Client, req llm.ChatRequest) (string, error) {
	return client.Complete(ctx, req)
}

// productionStream is the production streaming LLM call.
func productionStream(ctx context.Context, client *llm.Client, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	return client.StreamChat(ctx, req)
}

// generateUUID produces a time-based unique ID.
// Uses a simple implementation suitable for single-user deployment.
func generateUUID() string {
	return time.Now().Format("20060102-150405.000")
}
