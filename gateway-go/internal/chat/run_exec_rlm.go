// run_exec_rlm.go provides helper functions for RLM REPL integration
// in the agent execution pipeline.
package chat

import (
	"context"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/rlm/repl"
	"github.com/choiceoh/deneb/gateway-go/internal/unified"
)

// unifiedToLLMMessages converts unified MessageRecords to llm.Messages
// for inclusion in the LLM prompt (fresh tail).
func unifiedToLLMMessages(records []unified.MessageRecord) []llm.Message {
	msgs := make([]llm.Message, 0, len(records))
	for _, r := range records {
		msgs = append(msgs, llm.NewTextMessage(r.Role, r.Content))
	}
	return msgs
}

// unifiedToREPLEntries converts unified MessageRecords to REPL MessageEntries
// for the Starlark context variable.
func unifiedToREPLEntries(records []unified.MessageRecord) []repl.MessageEntry {
	entries := make([]repl.MessageEntry, len(records))
	for i, r := range records {
		entries[i] = repl.MessageEntry{
			Seq:       int(r.Seq),
			Role:      r.Role,
			Content:   r.Content,
			CreatedAt: r.CreatedAt,
		}
	}
	return entries
}

// buildREPLLLMQuery creates the llm_query callback for the REPL environment.
// It uses agent.RunAgent with zero tools for a simple single-turn LLM call.
func buildREPLLLMQuery(deps runDeps, logger *slog.Logger) repl.LLMQueryFunc {
	return func(ctx context.Context, prompt string) (string, error) {
		if deps.llmClient == nil {
			return "", nil
		}
		cfg := agent.AgentConfig{
			MaxTurns:  1,
			Timeout:   30 * time.Second,
			MaxTokens: 500,
		}
		msgs := []llm.Message{llm.NewTextMessage("user", prompt)}
		result, err := agent.RunAgent(ctx, cfg, msgs, deps.llmClient, nil, agent.StreamHooks{}, logger, nil)
		if err != nil {
			logger.Warn("REPL llm_query failed", "error", err)
			return "", err
		}
		return result.AllText, nil
	}
}
