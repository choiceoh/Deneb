// run_exec_rlm.go provides helper functions for RLM REPL integration
// in the agent execution pipeline, including the independent loop mode bridge.
package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/rlm"
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

// executeRLMLoop bridges the chat pipeline with the independent RLM iteration loop.
// It converts pipeline-level deps into an rlm.LoopConfig and runs rlm.RunLoop,
// then wraps the result as an *agent.AgentResult for the existing pipeline to consume.
func executeRLMLoop(
	ctx context.Context,
	rlmCfg rlm.Config,
	replEnv *repl.Env,
	client agent.LLMStreamer,
	model string,
	system json.RawMessage,
	userPrompt string,
	hooks agent.StreamHooks,
	budget *rlm.TokenBudget,
	logger *slog.Logger,
) (*agent.AgentResult, error) {
	loopCfg := rlm.LoopConfig{
		Client:           client,
		Model:            model,
		System:           system,
		MaxTokens:        8192,
		MaxIter:          rlmCfg.MaxIterations,
		CompactThreshold: rlmCfg.CompactionThreshold,
		MaxConsecErrors:  rlmCfg.MaxConsecutiveErrors,
		FallbackEnabled:  rlmCfg.FallbackEnabled,
		REPLEnv:          replEnv,
		Budget:           budget,
		Logger:           logger,
		OnTextDelta:      hooks.OnTextDelta,
	}

	logger.Info("RLM loop mode starting",
		"max_iter", loopCfg.MaxIter,
		"compact_threshold", loopCfg.CompactThreshold,
		"max_errors", loopCfg.MaxConsecErrors)

	loopResult, err := rlm.RunLoop(ctx, loopCfg, userPrompt)
	if err != nil {
		return nil, err
	}

	logger.Info("RLM loop completed",
		"iterations", loopResult.Iterations,
		"stop_reason", loopResult.StopReason,
		"compactions", loopResult.CompactionCount,
		"errors", loopResult.ErrorCount,
		"fallback", loopResult.FallbackUsed,
		"answer_len", len(loopResult.FinalAnswer))

	return &agent.AgentResult{
		Text:       loopResult.FinalAnswer,
		AllText:    loopResult.FinalAnswer,
		StopReason: loopResult.StopReason,
		Usage: llm.TokenUsage{
			InputTokens:  loopResult.TotalTokensIn,
			OutputTokens: loopResult.TotalTokensOut,
		},
		Turns: loopResult.Iterations,
	}, nil
}
