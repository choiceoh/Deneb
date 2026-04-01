// runner_loop.go — Core LLM agent execution loop.
//
// DefaultAgentRunner wraps the unified agent.RunAgent executor with
// autoreply-specific concerns: error recovery, model fallback, per-run memory
// management, and ToolMeta tracking.  The core LLM loop logic lives in
// internal/agent/.
//
// Sub-modules:
//   - runner_memory.go   — AgentRunnerMemory: conversation history + compact/flush
//   - runner_thinking.go — buildThinkingConfig, thinkingStreamer, agentMessagesToLLM
//   - runner_recovery.go — runAgentWithRecovery: 6-step error recovery chain
package autoreply

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/media"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// AgentTurnConfig configures a single agent turn execution.
type AgentTurnConfig struct {
	SessionKey     string
	AgentID        string
	Model          string
	Provider       string
	SystemPrompt   string
	Message        string
	Attachments    []media.MediaAttachment
	ThinkLevel     types.ThinkLevel
	FastMode       bool
	VerboseLevel   types.VerboseLevel
	ReasoningLevel types.ReasoningLevel
	ElevatedLevel  types.ElevatedLevel
	MaxTokens      int
	ContextTokens  int
	TimeoutMs      int64
	SkillFilter    []string
	// Execution environment.
	ExecHost     ExecHost     // "local", "sandbox"
	ExecSecurity ExecSecurity // "standard", "strict"
	ExecAsk      ExecAsk      // "always", "elevated-only", "never"
	// Model fallback.
	FallbackModels []string
	AuthProfile    string
}

// AgentTurnResult holds the outcome of an agent turn.
type AgentTurnResult struct {
	Payloads         []types.ReplyPayload
	ToolMeta         *media.ToolMeta
	OutputText       string
	OutputBlocks     []StreamBlock // structured text/code blocks from output coalescing
	Summary          string
	TokensUsed       session.TokenUsage
	ModelUsed        string
	ProviderUsed     string
	DurationMs       int64
	WasAborted       bool
	Error            error
	FallbackActive   bool
	FallbackAttempts []model.FallbackAttempt
	CompactedAt      int64
	TurnCount        int
}

// AgentExecutor runs LLM agent turns with tool execution and streaming.
type AgentExecutor interface {
	RunTurn(ctx context.Context, cfg AgentTurnConfig) (*AgentTurnResult, error)
}

// ToolExecutor runs tool calls and returns results.
// Note: this interface uses a three-return signature (output, isError, error)
// to distinguish tool-reported errors (isError=true) from executor failures.
type ToolExecutor interface {
	Execute(ctx context.Context, call ToolCall) (output string, isError bool, err error)
}

// ToolCall represents a tool invocation from the LLM.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ExecHost represents where tools execute.
type ExecHost string

const (
	ExecHostLocal   ExecHost = "local"
	ExecHostSandbox ExecHost = "sandbox"
)

// ExecSecurity represents tool execution security level.
type ExecSecurity string

const (
	ExecSecurityStandard ExecSecurity = "standard"
	ExecSecurityStrict   ExecSecurity = "strict"
)

// ExecAsk represents approval behavior for tool execution.
type ExecAsk string

const (
	ExecAskAlways       ExecAsk = "always"
	ExecAskElevatedOnly ExecAsk = "elevated-only"
	ExecAskNever        ExecAsk = "never"
)

// DefaultAgentRunner implements AgentExecutor using the unified agent.RunAgent loop.
type DefaultAgentRunner struct {
	llm      agent.LLMStreamer
	tools    ToolExecutor
	logger   *slog.Logger
	maxTurns int
	// Error recovery callback: called on context overflow or role ordering errors.
	onSessionReset func(sessionKey, reason string)
}

// AgentRunnerConfig configures the default agent runner.
type AgentRunnerConfig struct {
	LLM      agent.LLMStreamer
	Tools    ToolExecutor
	Logger   *slog.Logger
	MaxTurns int // max LLM round-trips (default 25)
}

// NewDefaultAgentRunner creates a new agent runner.
func NewDefaultAgentRunner(cfg AgentRunnerConfig) *DefaultAgentRunner {
	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 25
	}
	return &DefaultAgentRunner{
		llm:      cfg.LLM,
		tools:    cfg.Tools,
		logger:   cfg.Logger,
		maxTurns: maxTurns,
	}
}

// RunTurn executes the full agent loop via agent.RunAgent, wrapping it with
// error recovery, model fallback, memory management, and ToolMeta tracking.
func (r *DefaultAgentRunner) RunTurn(ctx context.Context, cfg AgentTurnConfig) (*AgentTurnResult, error) {
	startedAt := time.Now()
	result := &AgentTurnResult{
		ModelUsed:    cfg.Model,
		ProviderUsed: cfg.Provider,
		ToolMeta:     media.NewToolMeta(),
	}

	// Apply timeout.
	timeoutMs := cfg.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 5 * 60 * 1000 // 5 minutes default
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	// Check for immediate cancellation (covers TestDefaultAgentRunner_Timeout).
	if runCtx.Err() != nil {
		result.WasAborted = true
		result.DurationMs = time.Since(startedAt).Milliseconds()
		return result, nil
	}

	// Build initial message history.
	memory := NewAgentRunnerMemory(cfg.ContextTokens)
	if cfg.SystemPrompt != "" {
		memory.Append(AgentMessage{Role: "system", Content: cfg.SystemPrompt})
	}
	if cfg.Message != "" {
		memory.Append(AgentMessage{Role: "user", Content: cfg.Message})
	}

	// Pre-compaction memory flush: persist conversation history before trimming.
	if ShouldRunMemoryFlush(ShouldRunMemoryFlushParams{
		TotalTokens:         memory.UsedTokens(),
		ContextWindowTokens: memory.maxTokens,
	}) {
		flush := BuildMemoryFlush(memory, cfg.SessionKey, cfg.AgentID, session.TokenUsage{})
		r.logger.Info("pre-compaction memory flush",
			"sessionKey", flush.SessionKey,
			"messages", len(flush.Messages),
			"ts", flush.Timestamp)
	}

	// Auto-compact if we're already over budget before the run starts.
	if memory.UsedTokens() > memory.maxTokens*8/10 {
		if removed := memory.Compact(); removed > 0 {
			result.CompactedAt = time.Now().UnixMilli()
			// Add a compaction hint to the conversation so the model
			// knows context was trimmed.
			hint := BuildPostCompactionHint(PostCompactionContext{
				CompactedAt:  result.CompactedAt,
				TotalRemoved: removed,
			})
			if hint != "" {
				memory.Append(AgentMessage{Role: "system", Content: hint})
			}
		}
	}

	// Build the agent.ToolExecutor adapter so the unified executor can call tools.
	var toolExec agent.ToolExecutor
	if r.tools != nil {
		toolExec = &autoreplyToolAdapter{runner: r, cfg: cfg, meta: result.ToolMeta}
	}

	// Build thinking config from ThinkLevel (see runner_thinking.go).
	thinking := buildThinkingConfig(cfg.ThinkLevel)

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = model.DefaultMaxTokens
	}

	agentCfg := agent.AgentConfig{
		MaxTurns:  r.maxTurns,
		Timeout:   time.Duration(timeoutMs) * time.Millisecond,
		Model:     cfg.Model,
		System:    llm.SystemString(cfg.SystemPrompt),
		MaxTokens: maxTokens,
	}

	// Wrap the LLM streamer to inject ThinkingConfig if needed.
	var client agent.LLMStreamer = r.llm
	if thinking != nil {
		client = &thinkingStreamer{inner: r.llm, thinking: thinking}
	}

	// Convert AgentRunnerMemory history to []llm.Message for the unified executor.
	messages := agentMessagesToLLM(memory.History())

	logger := r.logger
	if logger == nil {
		logger = slog.Default()
	}

	// Execute via unified agent loop with full error recovery (see runner_recovery.go).
	agentResult, done := r.runAgentWithRecovery(runCtx, agentCfg, messages, client, toolExec, &cfg, result, startedAt, logger)
	if done {
		return result, nil
	}

	// Check for context-cancelled abort.
	if agentResult != nil && (agentResult.StopReason == "aborted" || agentResult.StopReason == "timeout") {
		result.WasAborted = true
	}

	if agentResult != nil {
		result.TurnCount = agentResult.Turns
		result.OutputText = agentResult.Text
		result.TokensUsed = session.TokenUsage{
			InputTokens:  int64(agentResult.Usage.InputTokens),
			OutputTokens: int64(agentResult.Usage.OutputTokens),
			TotalTokens:  int64(agentResult.Usage.InputTokens + agentResult.Usage.OutputTokens),
		}
		if agentResult.Text != "" {
			// Sanitize untrusted content in the output.
			sanitized := SanitizeUntrustedContent(agentResult.Text, DefaultUntrustedContentPolicy())

			// Detect and strip streaming directives from output text.
			if sd := DetectStreamingDirective(sanitized); sd != nil {
				// Streaming directive detected in output — log but don't
				// act on it (directives in output are informational only).
				_ = sd
			}

			// Coalesce output into structured blocks (text vs code) for
			// downstream formatters (e.g., Telegram code block extraction).
			coalescer := NewBlockCoalescer()
			coalescer.Feed(sanitized)
			blocks := coalescer.Flush()
			if len(blocks) > 0 {
				result.OutputBlocks = blocks
			}

			result.Payloads = append(result.Payloads, types.ReplyPayload{Text: sanitized})
			result.Summary = truncateToolOutput(sanitized, 2000)
		}
	}

	result.DurationMs = time.Since(startedAt).Milliseconds()
	return result, nil
}

// autoreplyToolAdapter adapts autoreply.ToolExecutor to agent.ToolExecutor.
// It handles elevated-permission checks, records invocations in ToolMeta,
// and converts the three-return signature to the two-return agent interface.
type autoreplyToolAdapter struct {
	runner *DefaultAgentRunner
	cfg    AgentTurnConfig
	meta   *media.ToolMeta
}

func (a *autoreplyToolAdapter) Execute(ctx context.Context, name string, input json.RawMessage) (string, error) {
	var inputMap map[string]any
	_ = json.Unmarshal(input, &inputMap)

	call := ToolCall{Name: name, Input: inputMap}

	start := time.Now()
	output, isError, toolErr := a.runner.executeTool(ctx, call, a.cfg)
	durationMs := time.Since(start).Milliseconds()

	if toolErr != nil {
		output = fmt.Sprintf("Error: %s", toolErr.Error())
		isError = true
	}

	if a.meta != nil {
		a.meta.Record(media.ToolInvocation{
			Name:     name,
			ID:       call.ID,
			Input:    formatToolInput(inputMap),
			Output:   truncateToolOutput(output, 2000),
			IsError:  isError,
			Duration: durationMs,
		})
	}

	if isError {
		return "", fmt.Errorf("%s", output)
	}
	return output, nil
}
