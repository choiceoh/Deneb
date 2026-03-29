// runner_loop.go — Core LLM agent execution loop and memory management.
package autoreply

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/media"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
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

// LLMClient abstracts the LLM HTTP API for the agent runner.
type LLMClient interface {
	// Chat sends a chat completion request and returns the response.
	Chat(ctx context.Context, req AgentRunnerPayload) (*LLMResponse, error)
	// ChatStream sends a streaming chat request and returns an event iterator.
	ChatStream(ctx context.Context, req AgentRunnerPayload) (LLMStreamIterator, error)
}

// LLMResponse holds a non-streaming LLM response.
type LLMResponse struct {
	Content    string
	ToolCalls  []ToolCall
	Usage      session.TokenUsage
	StopReason string // "end_turn", "tool_use", "max_tokens"
}

// ToolCall represents a tool invocation from the LLM.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// LLMStreamIterator yields streaming events.
type LLMStreamIterator interface {
	// Next returns the next event, or nil when done.
	Next() (*LLMStreamEvent, error)
	// Close releases resources.
	Close()
}

// LLMStreamEvent represents a single streaming event.
type LLMStreamEvent struct {
	Type       string              // "text", "tool_use_start", "tool_use_input", "tool_use_end", "thinking", "done"
	Text       string              // for "text" events
	ToolCall   *ToolCall           // for "tool_use_start"
	ToolInput  string              // for "tool_use_input" (JSON delta)
	Usage      *session.TokenUsage // for "done" events
	StopReason string              // for "done" events
}

// ToolExecutor runs tool calls and returns results.
type ToolExecutor interface {
	Execute(ctx context.Context, call ToolCall) (output string, isError bool, err error)
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

// DefaultAgentRunner implements AgentExecutor with the full LLM loop.
type DefaultAgentRunner struct {
	llm          LLMClient
	tools        ToolExecutor
	logger       *slog.Logger
	maxTurns     int
	maxToolCalls int
	// Error recovery state.
	onSessionReset func(sessionKey, reason string) // callback for session reset
}

// AgentRunnerConfig configures the default agent runner.
type AgentRunnerConfig struct {
	LLM          LLMClient
	Tools        ToolExecutor
	Logger       *slog.Logger
	MaxTurns     int // max LLM round-trips (default 25)
	MaxToolCalls int // max total tool calls per turn (default 100)
}

// NewDefaultAgentRunner creates a new agent runner.
func NewDefaultAgentRunner(cfg AgentRunnerConfig) *DefaultAgentRunner {
	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 25
	}
	maxToolCalls := cfg.MaxToolCalls
	if maxToolCalls <= 0 {
		maxToolCalls = 100
	}
	return &DefaultAgentRunner{
		llm:          cfg.LLM,
		tools:        cfg.Tools,
		logger:       cfg.Logger,
		maxTurns:     maxTurns,
		maxToolCalls: maxToolCalls,
	}
}

// RunTurn executes the full agent loop: LLM call → tool execution → repeat.
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

	// Build initial memory with system prompt and user message.
	memory := NewAgentRunnerMemory(cfg.ContextTokens)
	if cfg.SystemPrompt != "" {
		memory.Append(AgentMessage{Role: "system", Content: cfg.SystemPrompt})
	}
	if cfg.Message != "" {
		memory.Append(AgentMessage{Role: "user", Content: cfg.Message})
	}

	// Build tools list (from ToolExecutor — could be extended to support
	// tool filtering via cfg.SkillFilter).
	var tools []AgentToolDef
	// Tools are discovered from the ToolExecutor at call time.

	reminderGuard := NewReminderGuard(3)
	totalToolCalls := 0
	var allPayloads []types.ReplyPayload

	// Agent execution loop: send to LLM, process tool calls, repeat.
	for turn := 0; turn < r.maxTurns; turn++ {
		result.TurnCount = turn + 1

		// Check context cancellation.
		if runCtx.Err() != nil {
			result.WasAborted = true
			break
		}

		// Check if memory needs compaction.
		if memory.UsedTokens() > memory.maxTokens*8/10 {
			removed := memory.Compact()
			if removed > 0 {
				result.CompactedAt = time.Now().UnixMilli()
				r.logger.Debug("memory compacted", "removed", removed, "session", cfg.SessionKey)
			}
		}

		// Build LLM request.
		payload := BuildAgentPayload(cfg, memory.History(), tools)

		// Call LLM (streaming).
		var response *LLMResponse
		var llmErr error

		if r.llm == nil {
			llmErr = fmt.Errorf("no LLM client configured")
		} else {
			response, llmErr = r.llm.Chat(runCtx, payload)
		}

		if llmErr != nil {
			errMsg := llmErr.Error()

			// 1. Transient HTTP retry (502/503/521/429 → wait 2.5s, retry once).
			if IsTransientHTTPError(errMsg) && turn == 0 {
				r.logger.Warn("transient HTTP error, retrying",
					"error", errMsg, "session", cfg.SessionKey)
				select {
				case <-runCtx.Done():
					result.WasAborted = true
					break
				case <-time.After(TransientRetryDelayMs * time.Millisecond):
				}
				response, llmErr = r.llm.Chat(runCtx, payload)
			}

			// 2. Context overflow → auto-recovery.
			if llmErr != nil && IsContextOverflowError(llmErr.Error()) {
				if r.onSessionReset != nil {
					r.onSessionReset(cfg.SessionKey, "context_overflow")
				}
				result.Error = nil
				allPayloads = append(allPayloads, types.ReplyPayload{
					Text:    ContextOverflowMessage,
					IsError: true,
				})
				break
			}

			// 3. Billing error → specific message.
			if llmErr != nil && IsBillingError(llmErr.Error()) {
				result.Error = nil
				allPayloads = append(allPayloads, types.ReplyPayload{
					Text:    BillingErrorMessage,
					IsError: true,
				})
				break
			}

			// 4. Role ordering → session reset.
			if llmErr != nil && IsRoleOrderingError(llmErr.Error()) {
				if r.onSessionReset != nil {
					r.onSessionReset(cfg.SessionKey, "role_ordering")
				}
				result.Error = nil
				allPayloads = append(allPayloads, types.ReplyPayload{
					Text:    RoleOrderingMessage,
					IsError: true,
				})
				break
			}

			// 5. Try fallback models if available.
			if llmErr != nil && len(cfg.FallbackModels) > 0 && turn == 0 {
				for i, fallback := range cfg.FallbackModels {
					r.logger.Info("trying fallback model",
						"model", fallback, "attempt", i+1, "session", cfg.SessionKey)
					parts := splitProviderModel(fallback)
					if parts[0] != "" {
						cfg.Provider = parts[0]
					}
					cfg.Model = parts[1]
					payload.Model = cfg.Model
					result.FallbackActive = true
					result.FallbackAttempts = append(result.FallbackAttempts, model.FallbackAttempt{
						Provider: cfg.Provider,
						Model:    cfg.Model,
						Error:    llmErr.Error(),
					})

					response, llmErr = r.llm.Chat(runCtx, payload)
					if llmErr == nil {
						result.ModelUsed = cfg.Model
						result.ProviderUsed = cfg.Provider
						break
					}
				}
			}

			// 6. Final error — no recovery possible.
			if llmErr != nil {
				errText := llmErr.Error()
				if IsTransientHTTPError(errText) {
					errText = "⚠️ Provider temporarily unavailable. Please try again."
				}
				result.Error = llmErr
				allPayloads = append(allPayloads, types.ReplyPayload{
					Text:    fmt.Sprintf("⚠️ Agent failed: %s", strings.TrimRight(errText, ".")),
					IsError: true,
				})
				break
			}
		}

		// Accumulate usage.
		result.TokensUsed.AddUsage(response.Usage)

		// Process response content.
		if response.Content != "" {
			memory.Append(AgentMessage{Role: "assistant", Content: response.Content})
			result.OutputText += response.Content
		}

		// No tool calls → we're done.
		if len(response.ToolCalls) == 0 || response.StopReason == "end_turn" {
			if response.Content != "" {
				allPayloads = append(allPayloads, types.ReplyPayload{Text: response.Content})
			}
			break
		}

		// Process tool calls.
		if response.StopReason == "tool_use" && len(response.ToolCalls) > 0 {
			for _, call := range response.ToolCalls {
				totalToolCalls++
				if totalToolCalls > r.maxToolCalls {
					r.logger.Warn("max tool calls exceeded",
						"limit", r.maxToolCalls, "session", cfg.SessionKey)
					// Inject a reminder to the agent.
					if reminderGuard.TryRemind() {
						memory.Append(AgentMessage{
							Role:    "user",
							Content: "[System: Tool call limit reached. Please provide your final response.]",
						})
					}
					break
				}

				// Execute the tool.
				toolStart := time.Now()
				output, isError, toolErr := r.executeTool(runCtx, call, cfg)
				toolDuration := time.Since(toolStart).Milliseconds()

				result.ToolMeta.Record(media.ToolInvocation{
					Name:     call.Name,
					ID:       call.ID,
					Input:    formatToolInput(call.Input),
					Output:   truncateToolOutput(output, 2000),
					IsError:  isError || toolErr != nil,
					Duration: toolDuration,
				})

				if toolErr != nil {
					output = fmt.Sprintf("Error: %s", toolErr.Error())
					isError = true
				}

				// Add tool result to conversation.
				toolResult := buildToolResultMessage(call.ID, output, isError)
				memory.Append(toolResult)

				// Emit tool result payload for block streaming.
				if output != "" {
					allPayloads = append(allPayloads, types.ReplyPayload{
						Text:    output,
						IsError: isError,
					})
				}
			}
		}
	}

	result.Payloads = allPayloads
	result.DurationMs = time.Since(startedAt).Milliseconds()
	if result.OutputText != "" {
		result.Summary = truncateToolOutput(result.OutputText, 2000)
	}

	return result, nil
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
	// Keep system prompt (first) and at least 2 recent messages.
	minKeep := 3
	for m.usedTokens > m.maxTokens && len(m.history) > minKeep {
		// Remove the second message (oldest non-system).
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

	// Count messages to remove (keep system + last 2).
	keepTail := 2
	removeCount := len(m.history) - 1 - keepTail
	if removeCount <= 0 {
		return 0
	}

	// Calculate tokens being removed.
	removedTokens := 0
	for i := 1; i <= removeCount; i++ {
		removedTokens += model.EstimateTokens(m.history[i].Content)
	}

	// Replace removed messages with summary.
	summaryMsg := AgentMessage{
		Role:    "system",
		Content: fmt.Sprintf("[Conversation summary: %s]", summary),
	}

	newHistory := make([]AgentMessage, 0, 1+1+keepTail)
	newHistory = append(newHistory, m.history[0])                           // system prompt
	newHistory = append(newHistory, summaryMsg)                             // summary
	newHistory = append(newHistory, m.history[len(m.history)-keepTail:]...) // recent

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
