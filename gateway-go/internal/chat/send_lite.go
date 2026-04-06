// send_lite.go — Lightweight agent execution for system background tasks.
//
// SendLite bypasses the full chat pipeline (knowledge prefetch, context files,
// Aurora assembly, session history, skills, shadow context) and runs a minimal
// agent loop with only the specified tools. This dramatically reduces token
// consumption for simple system tasks like boot checks and diary extraction.
//
// Token comparison (typical boot task):
//   - SendSync: ~105K input tokens (193 msgs, 20 tools, 32K sysprompt, 15K knowledge)
//   - SendLite: ~2K input tokens (1 msg, 2 tools, minimal sysprompt)
package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// LiteOptions configures a lightweight agent run.
type LiteOptions struct {
	// MaxTurns limits agent loop iterations. Default: 5.
	MaxTurns int
	// Timeout for the entire run. Default: 2 minutes.
	Timeout time.Duration
	// MaxTokens per LLM output. Default: 2048.
	MaxTokens int
	// Model overrides the default model. Empty = use handler default.
	Model string
}

// liteDefaults fills in zero-value fields with sensible defaults.
func (o *LiteOptions) liteDefaults() {
	if o.MaxTurns <= 0 {
		o.MaxTurns = 5
	}
	if o.Timeout <= 0 {
		o.Timeout = 2 * time.Minute
	}
	if o.MaxTokens <= 0 {
		o.MaxTokens = 2048
	}
}

// SendLite runs a lightweight agent turn with no history, no knowledge prefetch,
// no context files, and only the specified tools. Designed for system background
// tasks (boot, diary, cron) that need minimal LLM interaction.
//
// Unlike SendSync which goes through the full executeAgentRun pipeline, SendLite
// calls agent.RunAgent directly with a stripped-down config:
//   - No session creation or transcript persistence
//   - No knowledge prefetch (Vega, memory search, user model)
//   - No context files (CLAUDE.md, SOUL.md, etc.)
//   - No Aurora context assembly or compaction
//   - No skills prompt, shadow context, or proactive hints
//   - No nudge budget or autonomous continuation
//   - Only the explicitly listed tools are available
func (h *Handler) SendLite(ctx context.Context, systemPrompt, userMessage string, toolNames []string, opts *LiteOptions) (*SyncResult, error) {
	if opts == nil {
		opts = &LiteOptions{}
	}
	opts.liteDefaults()

	// Resolve model.
	model := opts.Model
	if model == "" {
		model = h.DefaultModel()
	}
	if model == "" && h.registry != nil {
		model = h.registry.FullModelID("main")
	}
	if model == "" {
		return nil, fmt.Errorf("send_lite: no model configured")
	}

	// Resolve provider and model name.
	providerID, modelName := parseModelID(model)
	model = modelName

	// Resolve LLM client.
	deps := h.buildRunDeps()
	client := resolveClient(deps, providerID, h.logger)
	if client == nil {
		return nil, fmt.Errorf("send_lite: no LLM client available (provider=%q, model=%q)", providerID, model)
	}

	// Build filtered tool list: only include explicitly requested tools.
	var tools []llm.Tool
	var toolExecutor agent.ToolExecutor
	if len(toolNames) > 0 && deps.tools != nil {
		allowed := make(map[string]bool, len(toolNames))
		for _, name := range toolNames {
			allowed[name] = true
		}
		tools = deps.tools.FilteredLLMTools(allowed)
		toolExecutor = deps.tools
	}

	// Build messages: just the user prompt.
	messages := []llm.Message{llm.NewTextMessage("user", userMessage)}

	// Build system prompt.
	var system []byte
	if systemPrompt != "" {
		system = llm.SystemString(systemPrompt)
	}

	// Minimal agent config: no nudge, no continuation, no deferred tools.
	cfg := agent.AgentConfig{
		MaxTurns:  opts.MaxTurns,
		Timeout:   opts.Timeout,
		Model:     model,
		System:    system,
		Tools:     tools,
		MaxTokens: opts.MaxTokens,
	}

	logger := h.logger.With("mode", "lite")
	logger.Info("send_lite: starting",
		"model", model,
		"provider", providerID,
		"tools", len(tools),
		"maxTurns", opts.MaxTurns)

	result, err := agent.RunAgent(ctx, cfg, messages, client, toolExecutor, agent.StreamHooks{}, logger, nil)
	if err != nil {
		return nil, fmt.Errorf("send_lite: agent run failed: %w", err)
	}

	logger.Info("send_lite: complete",
		"turns", result.Turns,
		"inputTokens", result.Usage.InputTokens,
		"outputTokens", result.Usage.OutputTokens,
		"stopReason", result.StopReason)

	return &SyncResult{
		Text:         result.Text,
		Model:        model,
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
		StopReason:   result.StopReason,
	}, nil
}
