// agent.go — Thin adapter bridging chat/ to the unified agent executor.
//
// All core loop logic lives in internal/agent/executor.go.
// This file re-exports the shared types and wraps RunAgent so that
// the rest of the chat package can remain unchanged.
package chat

import (
	"context"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// AgentConfig configures the agent execution loop.
// Type alias — identical to agent.AgentConfig; no conversion needed.
type AgentConfig = agent.AgentConfig

// AgentResult is the outcome of an agent run.
type AgentResult = agent.AgentResult

// StreamHooks contains optional callbacks for agent streaming events.
type StreamHooks = agent.StreamHooks

// TurnCallback is called after each agent turn with accumulated token count.
type TurnCallback = agent.TurnCallback

// DefaultAgentConfig returns sensible defaults.
var DefaultAgentConfig = agent.DefaultAgentConfig

// RunAgent executes the agent tool-call loop: call LLM → detect tool_use →
// execute tool → feed result → repeat until the model stops or limits are hit.
//
// This is a thin wrapper around agent.RunAgent; *llm.Client satisfies the
// agent.LLMStreamer interface so it can be passed directly.
func RunAgent(
	ctx context.Context,
	cfg AgentConfig,
	messages []llm.Message,
	client *llm.Client,
	tools ToolExecutor,
	hooks StreamHooks,
	logger *slog.Logger,
	runLog *agentlog.RunLogger,
) (*AgentResult, error) {
	return agent.RunAgent(ctx, cfg, messages, client, tools, hooks, logger, runLog)
}
