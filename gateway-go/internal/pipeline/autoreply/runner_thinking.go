// runner_thinking.go — ThinkingConfig mapping and LLM message conversion.
//
// buildThinkingConfig maps a ThinkLevel to the corresponding llm.ThinkingConfig
// token budget, or returns nil for ThinkOff/unset (disables extended thinking).
// thinkingStreamer injects ThinkingConfig into every outgoing LLM request.
// agentMessagesToLLM converts AgentRunnerMemory history to []llm.Message.
package autoreply

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
)

// buildThinkingConfig maps a ThinkLevel to the corresponding llm.ThinkingConfig.
// Returns nil for ThinkOff or unset, which disables extended thinking.
// Token budgets are defined in types.ThinkLevel.BudgetTokens() (single source of truth).
func buildThinkingConfig(level types.ThinkLevel) *llm.ThinkingConfig {
	budget := level.BudgetTokens()
	if budget <= 0 {
		return nil
	}
	return &llm.ThinkingConfig{Type: "enabled", BudgetTokens: budget}
}

// Compile-time interface compliance.
var _ agent.LLMStreamer = (*thinkingStreamer)(nil)

// thinkingStreamer wraps an LLMStreamer to inject a ThinkingConfig into every request.
type thinkingStreamer struct {
	inner    agent.LLMStreamer
	thinking *llm.ThinkingConfig
}

func (t *thinkingStreamer) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	req.Thinking = t.thinking
	return t.inner.StreamChat(ctx, req)
}

func (t *thinkingStreamer) Complete(ctx context.Context, req llm.ChatRequest) (string, error) {
	req.Thinking = t.thinking
	return t.inner.Complete(ctx, req)
}

// agentMessagesToLLM converts AgentRunnerMemory history to llm.Message slice.
// System messages are skipped (they belong in AgentConfig.System, not messages).
func agentMessagesToLLM(history []AgentMessage) []llm.Message {
	out := make([]llm.Message, 0, len(history))
	for _, m := range history {
		if m.Role == "system" {
			continue // system goes in AgentConfig.System
		}
		switch {
		case m.ToolUseID != "":
			block := llm.ContentBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolUseID,
				Content:   m.Content,
				IsError:   m.IsError,
			}
			out = append(out, llm.NewBlockMessage("user", []llm.ContentBlock{block}))
		case len(m.ContentBlocks) > 0:
			blocks := make([]llm.ContentBlock, 0, len(m.ContentBlocks))
			for _, cb := range m.ContentBlocks {
				blocks = append(blocks, llm.ContentBlock{
					Type:  string(cb.Type),
					Text:  cb.Text,
					ID:    cb.ID,
					Name:  cb.Name,
					Input: marshalInput(cb.Input),
				})
			}
			out = append(out, llm.NewBlockMessage(m.Role, blocks))
		default:
			out = append(out, llm.NewTextMessage(m.Role, m.Content))
		}
	}
	return out
}

func marshalInput(input map[string]any) json.RawMessage {
	if input == nil {
		return nil
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return []byte("{}")
	}
	return raw
}
