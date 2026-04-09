// agent_types.go — Shared types for the agent execution interface.
//
// AgentExecutor is consumed by ReplyFromConfig (get_reply.go)
// and implemented by chatSendExecutor (server/inbound_deps.go).
package autoreply

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
)

// AgentExecutor runs LLM agent turns with tool execution and streaming.
type AgentExecutor interface {
	RunTurn(ctx context.Context, cfg AgentTurnConfig) (*AgentTurnResult, error)
}

// AgentTurnConfig configures a single agent turn execution.
type AgentTurnConfig struct {
	SessionKey     string
	AgentID        string
	Model          string
	Provider       string
	Message        string
	ThinkLevel     types.ThinkLevel
	FastMode       bool
	VerboseLevel   types.VerboseLevel
	ReasoningLevel types.ReasoningLevel
	ElevatedLevel  types.ElevatedLevel
	MaxTokens      int
	ContextTokens  int
	AuthProfile    string
	DeepWork       bool
}

// AgentTurnResult holds the outcome of an agent turn.
// The actual reply delivery happens asynchronously via chatSendExecutor →
// chat.Handler.Send(). Payloads here are only used for command replies.
type AgentTurnResult struct {
	Payloads []types.ReplyPayload
}
