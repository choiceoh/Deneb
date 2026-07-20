// executor_turn.go — Per-turn context and LLM request preparation.
package agent

import (
	"context"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// turnRequestPreparer owns the mutable request-level state that spans turns.
// Keeping it separate from RunAgent makes the ordering of context decoration,
// deferred inputs, request-only message hooks, and thinking selection explicit.
type turnRequestPreparer struct {
	cfg              *AgentConfig
	thinkingOverride *llm.ThinkingConfig
}

type preparedTurnRequest struct {
	ctx      context.Context
	request  llm.ChatRequest
	thinking *llm.ThinkingConfig
}

func newTurnRequestPreparer(cfg *AgentConfig) *turnRequestPreparer {
	return &turnRequestPreparer{cfg: cfg}
}

// prepare builds one request in the same order the turn observes its inputs:
// context first, then late system/tool additions, request-only message changes,
// and finally the thinking policy used by the ChatRequest.
func (p *turnRequestPreparer) prepare(
	ctx context.Context,
	turn int,
	messages []llm.Message,
	toolActivities []ToolActivity,
) preparedTurnRequest {
	ctx = p.initializeContext(ctx)
	p.refreshDeferredInputs(turn)
	apiMessages := p.messagesForRequest(messages)
	thinking := p.selectThinking(turn, toolActivities)

	return preparedTurnRequest{
		ctx:      ctx,
		request:  p.buildRequest(apiMessages, thinking),
		thinking: thinking,
	}
}

func (p *turnRequestPreparer) initializeContext(ctx context.Context) context.Context {
	if p.cfg.OnTurnInit == nil {
		return ctx
	}
	if decorated := p.cfg.OnTurnInit(ctx); decorated != nil {
		return decorated
	}
	return ctx
}

// refreshDeferredInputs applies mid-run tool activations to the request's
// tools array. This is the ONLY mid-run mutation of the (system, tools)
// prefix left by design: a model will not call a tool absent from the tools
// array (live-probed on kimi 2026-07-20 — with the schema in-band only, it
// re-calls fetch_tools instead), so the one prompt-cache cold start per
// activation turn is the price of the tool being callable at all. System
// stays untouched mid-run; late notifications ride DeferredTurnNotices.
func (p *turnRequestPreparer) refreshDeferredInputs(turn int) {
	if turn <= 0 {
		return
	}
	if p.cfg.DynamicToolsProvider != nil {
		if extra := p.cfg.DynamicToolsProvider(); len(extra) > 0 {
			p.cfg.Tools = appendUniqueTools(p.cfg.Tools, extra)
		}
	}
}

// messagesForRequest returns the per-call view produced by BeforeAPICall. It
// never replaces RunAgent's message history, which keeps subsequent prompt
// cache prefixes stable. A nil hook result falls back to the original view.
func (p *turnRequestPreparer) messagesForRequest(messages []llm.Message) []llm.Message {
	if p.cfg.BeforeAPICall == nil {
		return messages
	}
	if apiMessages := p.cfg.BeforeAPICall(messages); apiMessages != nil {
		return apiMessages
	}
	return messages
}

func (p *turnRequestPreparer) overrideThinkingOnce(thinking *llm.ThinkingConfig) {
	p.thinkingOverride = thinking
}

func (p *turnRequestPreparer) selectThinking(turn int, toolActivities []ToolActivity) *llm.ThinkingConfig {
	if p.thinkingOverride != nil {
		thinking := p.thinkingOverride
		p.thinkingOverride = nil
		return thinking
	}
	if p.cfg.ThinkingModulator != nil {
		if thinking := p.cfg.ThinkingModulator(turn, toolActivities); thinking != nil {
			return thinking
		}
	}
	return p.cfg.Thinking
}

func (p *turnRequestPreparer) buildRequest(messages []llm.Message, thinking *llm.ThinkingConfig) llm.ChatRequest {
	return llm.ChatRequest{
		Model:            p.cfg.Model,
		Messages:         messages,
		System:           llm.FlexibleFromRaw(p.cfg.System),
		MaxTokens:        p.cfg.MaxTokens,
		Tools:            p.cfg.Tools,
		Stream:           true,
		Thinking:         thinking,
		Temperature:      p.cfg.Temperature,
		TopP:             p.cfg.TopP,
		TopK:             p.cfg.TopK,
		FrequencyPenalty: p.cfg.FrequencyPenalty,
		PresencePenalty:  p.cfg.PresencePenalty,
		Seed:             p.cfg.Seed,
		StopSequences:    p.cfg.StopSequences,
		ResponseFormat:   p.cfg.ResponseFormat,
		ToolChoice:       flexibleToolChoice(p.cfg.ToolChoice),
	}
}

// appendBudgetWarning adds the warning after the turn's tool results and other
// nudges have been collected, immediately before their user message is built.
func (p *turnRequestPreparer) appendBudgetWarning(turn int, blocks []llm.ContentBlock) []llm.ContentBlock {
	if p.cfg.SpawnDetected == nil || !p.cfg.SpawnDetected() {
		return blocks
	}
	warning := buildTurnBudgetWarning(turn, p.cfg.MaxTurns)
	if warning == "" {
		return blocks
	}
	return append(blocks, llm.ContentBlock{Type: "text", Text: warning})
}

// buildTurnBudgetWarning returns a warning message when the agent is
// approaching the turn limit while sub-agents are running. Used to tell the
// agent to wrap up and yield to the notification system. Returns "" when no
// warning is needed.
func buildTurnBudgetWarning(currentTurn, maxTurns int) string {
	remaining := maxTurns - currentTurn - 1 // -1 because turn is 0-based and we just finished it
	if remaining <= 0 {
		return ""
	}
	// Warning at 80% of budget (5 turns remaining out of 25).
	threshold := maxTurns / 5
	if threshold < 3 {
		threshold = 3
	}
	if remaining > threshold {
		return ""
	}
	return fmt.Sprintf("[System: 턴 예산 정보 — 남은 턴 %d/%d. 서브에이전트가 작업 중입니다. 추가 작업이 없으면 턴을 종료하세요.]",
		remaining, maxTurns)
}

// appendUniqueTools appends extra tools to base, skipping any whose name
// already exists in base. Used for dynamic tool injection (deferred tools).
func appendUniqueTools(base, extra []llm.Tool) []llm.Tool {
	existing := make(map[string]struct{}, len(base))
	for _, tool := range base {
		existing[tool.Name] = struct{}{}
	}
	for _, tool := range extra {
		if _, ok := existing[tool.Name]; !ok {
			base = append(base, tool)
			existing[tool.Name] = struct{}{}
		}
	}
	return base
}

func flexibleToolChoice(v any) llm.FlexibleJSON {
	if v == nil {
		return llm.FlexibleJSON{}
	}
	return llm.FlexibleFromValue(v)
}
