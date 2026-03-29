// runner_loop.go — Core LLM agent execution loop and memory management.
//
// DefaultAgentRunner wraps the unified agent.RunAgent executor with
// autoreply-specific concerns: error recovery (transient HTTP, context
// overflow, billing), model fallback, per-run memory management, and
// ToolMeta tracking.  The core LLM loop logic lives in internal/agent/.
package autoreply

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/media"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/pipeline"
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
	apiType  string // "anthropic" or "openai" (default)
	tools    ToolExecutor
	logger   *slog.Logger
	maxTurns int
	// Error recovery callback: called on context overflow or role ordering errors.
	onSessionReset func(sessionKey, reason string)
}

// AgentRunnerConfig configures the default agent runner.
type AgentRunnerConfig struct {
	LLM      agent.LLMStreamer
	APIType  string // "anthropic" or "openai" (default)
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
		apiType:  cfg.APIType,
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

	// Auto-compact if we're already over budget before the run starts.
	if memory.UsedTokens() > memory.maxTokens*8/10 {
		if removed := memory.Compact(); removed > 0 {
			result.CompactedAt = time.Now().UnixMilli()
		}
	}

	// Build the agent.ToolExecutor adapter so the unified executor can call tools.
	var toolExec agent.ToolExecutor
	if r.tools != nil {
		toolExec = &autoreplyToolAdapter{runner: r, cfg: cfg, meta: result.ToolMeta}
	}

	// Build thinking config from ThinkLevel.
	var thinking *llm.ThinkingConfig
	switch cfg.ThinkLevel {
	case types.ThinkMinimal:
		thinking = &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 1024}
	case types.ThinkLow:
		thinking = &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 4096}
	case types.ThinkMedium:
		thinking = &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 10240}
	case types.ThinkHigh:
		thinking = &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 32768}
	case types.ThinkXHigh:
		thinking = &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 65536}
	case types.ThinkAdaptive:
		thinking = &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 16384}
	}

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
		APIType:   r.apiType,
	}

	// Wrap the LLM streamer to inject ThinkingConfig if needed.
	var client agent.LLMStreamer = r.llm
	if thinking != nil {
		client = &thinkingStreamer{inner: r.llm, thinking: thinking}
	}

	// Convert AgentRunnerMemory history to []llm.Message for the unified executor.
	messages := agentMessagesToLLM(memory.History())

	// Execute via unified agent loop, with error recovery around it.
	logger := r.logger
	if logger == nil {
		logger = slog.Default()
	}

	var agentResult *agent.AgentResult
	var runErr error

	agentResult, runErr = agent.RunAgent(runCtx, agentCfg, messages, client, toolExec, agent.StreamHooks{}, logger, nil)

	if runErr != nil {
		errMsg := runErr.Error()

		// 1. Transient HTTP retry (502/503/521/429 → wait 2.5s, retry once).
		if IsTransientHTTPError(errMsg) {
			logger.Warn("transient HTTP error, retrying", "error", errMsg, "session", cfg.SessionKey)
			select {
			case <-runCtx.Done():
				result.WasAborted = true
				result.DurationMs = time.Since(startedAt).Milliseconds()
				return result, nil
			case <-time.After(TransientRetryDelayMs * time.Millisecond):
			}
			agentResult, runErr = agent.RunAgent(runCtx, agentCfg, messages, client, toolExec, agent.StreamHooks{}, logger, nil)
		}

		// 2. Context overflow → auto-recovery.
		if runErr != nil && IsContextOverflowError(runErr.Error()) {
			if r.onSessionReset != nil {
				r.onSessionReset(cfg.SessionKey, "context_overflow")
			}
			result.Payloads = append(result.Payloads, types.ReplyPayload{Text: ContextOverflowMessage, IsError: true})
			result.DurationMs = time.Since(startedAt).Milliseconds()
			return result, nil
		}

		// 3. Billing error.
		if runErr != nil && IsBillingError(runErr.Error()) {
			result.Payloads = append(result.Payloads, types.ReplyPayload{Text: BillingErrorMessage, IsError: true})
			result.DurationMs = time.Since(startedAt).Milliseconds()
			return result, nil
		}

		// 4. Role ordering → session reset.
		if runErr != nil && IsRoleOrderingError(runErr.Error()) {
			if r.onSessionReset != nil {
				r.onSessionReset(cfg.SessionKey, "role_ordering")
			}
			result.Payloads = append(result.Payloads, types.ReplyPayload{Text: RoleOrderingMessage, IsError: true})
			result.DurationMs = time.Since(startedAt).Milliseconds()
			return result, nil
		}

		// 5. Try fallback models if available.
		if runErr != nil && len(cfg.FallbackModels) > 0 {
			for i, fallback := range cfg.FallbackModels {
				logger.Info("trying fallback model", "model", fallback, "attempt", i+1, "session", cfg.SessionKey)
				parts := pipeline.SplitProviderModel(fallback)
				if parts[0] != "" {
					cfg.Provider = parts[0]
				}
				cfg.Model = parts[1]
				agentCfg.Model = cfg.Model
				result.FallbackActive = true
				result.FallbackAttempts = append(result.FallbackAttempts, model.FallbackAttempt{
					Provider: cfg.Provider,
					Model:    cfg.Model,
					Error:    runErr.Error(),
				})
				agentResult, runErr = agent.RunAgent(runCtx, agentCfg, messages, client, toolExec, agent.StreamHooks{}, logger, nil)
				if runErr == nil {
					result.ModelUsed = cfg.Model
					result.ProviderUsed = cfg.Provider
					break
				}
			}
		}

		// 6. Final error — no recovery possible.
		if runErr != nil {
			errText := runErr.Error()
			// Replace raw HTTP error strings with specific Korean messages.
			if specific := ClassifyErrorMessage(errText); specific != "" {
				errText = specific
			}
			result.Error = runErr
			result.Payloads = append(result.Payloads, types.ReplyPayload{
				Text:    fmt.Sprintf("⚠️ Agent failed: %s", strings.TrimRight(errText, ".")),
				IsError: true,
			})
			result.DurationMs = time.Since(startedAt).Milliseconds()
			return result, nil
		}
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
			result.Payloads = append(result.Payloads, types.ReplyPayload{Text: agentResult.Text})
			result.Summary = truncateToolOutput(agentResult.Text, 2000)
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

// thinkingStreamer wraps an LLMStreamer to inject a ThinkingConfig into every request.
type thinkingStreamer struct {
	inner    agent.LLMStreamer
	thinking *llm.ThinkingConfig
}

func (t *thinkingStreamer) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	req.Thinking = t.thinking
	return t.inner.StreamChat(ctx, req)
}

func (t *thinkingStreamer) StreamChatOpenAI(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	req.Thinking = t.thinking
	return t.inner.StreamChatOpenAI(ctx, req)
}

// agentMessagesToLLM converts AgentRunnerMemory history to llm.Message slice.
// System messages are skipped (they belong in AgentConfig.System, not messages).
func agentMessagesToLLM(history []AgentMessage) []llm.Message {
	out := make([]llm.Message, 0, len(history))
	for _, m := range history {
		if m.Role == "system" {
			continue // system goes in AgentConfig.System
		}
		if m.ToolUseID != "" {
			block := llm.ContentBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolUseID,
				Content:   m.Content,
				IsError:   m.IsError,
			}
			out = append(out, llm.NewBlockMessage("user", []llm.ContentBlock{block}))
		} else if len(m.ContentBlocks) > 0 {
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
		} else {
			out = append(out, llm.NewTextMessage(m.Role, m.Content))
		}
	}
	return out
}

func marshalInput(input map[string]any) json.RawMessage {
	if input == nil {
		return nil
	}
	raw, _ := json.Marshal(input)
	return raw
}

// AgentRunnerMemory manages conversation context for agent execution.
type AgentRunnerMemory struct {
	mu         sync.Mutex
	history    []AgentMessage
	maxTokens  int
	usedTokens int
	// compaction tracking
	compactionCount int
	totalCompacted  int
}

func NewAgentRunnerMemory(maxTokens int) *AgentRunnerMemory {
	if maxTokens <= 0 {
		maxTokens = model.DefaultContextTokens
	}
	return &AgentRunnerMemory{maxTokens: maxTokens}
}

func (m *AgentRunnerMemory) Append(msg AgentMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.history = append(m.history, msg)
	m.usedTokens += model.EstimateTokens(msg.Content)
}

func (m *AgentRunnerMemory) History() []AgentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]AgentMessage, len(m.history))
	copy(result, m.history)
	return result
}

// Compact removes older messages to fit within the token budget.
// Preserves the system message (index 0) and the most recent messages.
// Returns the number of messages removed.
func (m *AgentRunnerMemory) Compact() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.usedTokens <= m.maxTokens || len(m.history) <= 2 {
		return 0
	}

	removed := 0
	minKeep := 3
	for m.usedTokens > m.maxTokens && len(m.history) > minKeep {
		oldest := m.history[1]
		m.history = append(m.history[:1], m.history[2:]...)
		tokens := model.EstimateTokens(oldest.Content)
		m.usedTokens -= tokens
		removed++
	}

	m.compactionCount++
	m.totalCompacted += removed
	return removed
}

// CompactWithSummary replaces removed messages with a summary.
func (m *AgentRunnerMemory) CompactWithSummary(summary string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.usedTokens <= m.maxTokens || len(m.history) <= 3 {
		return 0
	}

	keepTail := 2
	removeCount := len(m.history) - 1 - keepTail
	if removeCount <= 0 {
		return 0
	}

	removedTokens := 0
	for i := 1; i <= removeCount; i++ {
		removedTokens += model.EstimateTokens(m.history[i].Content)
	}

	summaryMsg := AgentMessage{
		Role:    "system",
		Content: fmt.Sprintf("[Conversation summary: %s]", summary),
	}

	newHistory := make([]AgentMessage, 0, 1+1+keepTail)
	newHistory = append(newHistory, m.history[0])
	newHistory = append(newHistory, summaryMsg)
	newHistory = append(newHistory, m.history[len(m.history)-keepTail:]...)

	m.history = newHistory
	m.usedTokens -= removedTokens
	m.usedTokens += model.EstimateTokens(summary)
	m.compactionCount++
	m.totalCompacted += removeCount
	return removeCount
}

func (m *AgentRunnerMemory) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.history = nil
	m.usedTokens = 0
}

func (m *AgentRunnerMemory) UsedTokens() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usedTokens
}

func (m *AgentRunnerMemory) MessageCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.history)
}

func (m *AgentRunnerMemory) CompactionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.compactionCount
}

// MemoryFlush writes conversation history to persistent storage.
type MemoryFlush struct {
	SessionKey string
	AgentID    string
	Messages   []AgentMessage
	Timestamp  int64
	Usage      session.TokenUsage
}

func BuildMemoryFlush(memory *AgentRunnerMemory, sessionKey, agentID string, usage session.TokenUsage) MemoryFlush {
	return MemoryFlush{
		SessionKey: sessionKey,
		AgentID:    agentID,
		Messages:   memory.History(),
		Timestamp:  time.Now().UnixMilli(),
		Usage:      usage,
	}
}
