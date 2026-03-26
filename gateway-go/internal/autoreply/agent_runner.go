// agent_runner.go — Core LLM agent execution engine.
// Mirrors src/auto-reply/reply/agent-runner.ts (763 LOC),
// agent-runner-execution.ts (674 LOC), agent-runner-memory.ts (561 LOC),
// agent-runner-payloads.ts (251 LOC), agent-runner-utils.ts (303 LOC),
// agent-runner-helpers.ts (76 LOC), agent-runner-reminder-guard.ts (64 LOC).
package autoreply

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// AgentTurnConfig configures a single agent turn execution.
type AgentTurnConfig struct {
	SessionKey     string
	AgentID        string
	Model          string
	Provider       string
	SystemPrompt   string
	Message        string
	Attachments    []MediaAttachment
	ThinkLevel     ThinkLevel
	FastMode       bool
	VerboseLevel   VerboseLevel
	ReasoningLevel ReasoningLevel
	ElevatedLevel  ElevatedLevel
	MaxTokens      int
	ContextTokens  int
	TimeoutMs      int64
	SkillFilter    []string
	// Execution environment.
	ExecHost     string // "local", "sandbox"
	ExecSecurity string // "standard", "strict"
	ExecAsk      string // "always", "elevated-only", "never"
	// Model fallback.
	FallbackModels []string
	AuthProfile    string
}

// AgentTurnResult holds the outcome of an agent turn.
type AgentTurnResult struct {
	Payloads       []ReplyPayload
	ToolMeta       *ToolMeta
	OutputText     string
	Summary        string
	TokensUsed     TokenUsage
	ModelUsed      string
	ProviderUsed   string
	DurationMs     int64
	WasAborted     bool
	Error          error
	FallbackActive bool
	FallbackAttempts []FallbackAttempt
	CompactedAt    int64
	TurnCount      int
}

// TokenUsage tracks token consumption for an agent turn.
type TokenUsage struct {
	InputTokens      int64 `json:"inputTokens,omitempty"`
	OutputTokens     int64 `json:"outputTokens,omitempty"`
	TotalTokens      int64 `json:"totalTokens,omitempty"`
	CacheReadTokens  int64 `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64 `json:"cacheWriteTokens,omitempty"`
}

// AddUsage accumulates usage from another.
func (u *TokenUsage) AddUsage(other TokenUsage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.TotalTokens += other.TotalTokens
	u.CacheReadTokens += other.CacheReadTokens
	u.CacheWriteTokens += other.CacheWriteTokens
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
	Usage      TokenUsage
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
	Type       string     // "text", "tool_use_start", "tool_use_input", "tool_use_end", "thinking", "done"
	Text       string     // for "text" events
	ToolCall   *ToolCall  // for "tool_use_start"
	ToolInput  string     // for "tool_use_input" (JSON delta)
	Usage      *TokenUsage // for "done" events
	StopReason string     // for "done" events
}

// ToolExecutor runs tool calls and returns results.
type ToolExecutor interface {
	Execute(ctx context.Context, call ToolCall) (output string, isError bool, err error)
}

// --- Default Agent Runner Implementation ---

// DefaultAgentRunner implements AgentExecutor with the full LLM loop.
type DefaultAgentRunner struct {
	llm          LLMClient
	tools        ToolExecutor
	logger       *slog.Logger
	maxTurns     int
	maxToolCalls int
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
		ToolMeta:     NewToolMeta(),
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
	var allPayloads []ReplyPayload

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
			// Try fallback models if available.
			if len(cfg.FallbackModels) > 0 && turn == 0 {
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
					result.FallbackAttempts = append(result.FallbackAttempts, FallbackAttempt{
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
			if llmErr != nil {
				result.Error = llmErr
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
				allPayloads = append(allPayloads, ReplyPayload{Text: response.Content})
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

				result.ToolMeta.Record(ToolInvocation{
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
					allPayloads = append(allPayloads, ReplyPayload{
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

func (r *DefaultAgentRunner) executeTool(ctx context.Context, call ToolCall, cfg AgentTurnConfig) (string, bool, error) {
	if r.tools == nil {
		return "", true, fmt.Errorf("no tool executor configured")
	}

	// Check elevated permissions for bash/exec tools.
	if (call.Name == "bash" || call.Name == "execute" || call.Name == "computer") &&
		cfg.ElevatedLevel == ElevatedOff {
		return "Tool execution requires elevated permissions. Use /elevated on to enable.", true, nil
	}

	// Check approval requirement.
	if cfg.ExecAsk == "always" && (call.Name == "bash" || call.Name == "execute") {
		// In approval mode, we'd normally pause and wait for user approval.
		// For now, auto-approve in the Go gateway (matches DGX Spark single-user model).
	}

	return r.tools.Execute(ctx, call)
}

// --- Supporting types and functions ---

// ReminderGuard prevents infinite reminder loops during agent execution.
type ReminderGuard struct {
	mu       sync.Mutex
	count    int
	maxCount int
}

func NewReminderGuard(maxCount int) *ReminderGuard {
	if maxCount <= 0 {
		maxCount = 3
	}
	return &ReminderGuard{maxCount: maxCount}
}

func (g *ReminderGuard) TryRemind() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.count >= g.maxCount {
		return false
	}
	g.count++
	return true
}

func (g *ReminderGuard) Reset() {
	g.mu.Lock()
	g.count = 0
	g.mu.Unlock()
}

// AgentRunnerPayload builds the LLM request payload.
type AgentRunnerPayload struct {
	Model       string         `json:"model"`
	System      string         `json:"system,omitempty"`
	Messages    []AgentMessage `json:"messages"`
	MaxTokens   int            `json:"max_tokens,omitempty"`
	Tools       []AgentToolDef `json:"tools,omitempty"`
	Stream      bool           `json:"stream,omitempty"`
	Temperature float64        `json:"temperature,omitempty"`
	// Thinking configuration.
	Thinking *ThinkingConfig `json:"thinking,omitempty"`
}

// ThinkingConfig configures the thinking/reasoning mode for the LLM request.
type ThinkingConfig struct {
	Type         string `json:"type"`                    // "enabled" or "disabled"
	BudgetTokens int    `json:"budget_tokens,omitempty"` // token budget for thinking
}

// AgentMessage is a single message in the agent conversation.
type AgentMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolUseID  string          `json:"tool_use_id,omitempty"`  // for tool_result
	IsError    bool            `json:"is_error,omitempty"`     // for tool_result
	ContentBlocks []ContentBlock `json:"content_blocks,omitempty"` // for multi-block messages
}

// ContentBlock represents a block within a message (text, tool_use, tool_result, thinking).
type ContentBlock struct {
	Type      string         `json:"type"` // "text", "tool_use", "tool_result", "thinking"
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	Content   string         `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
	Thinking  string         `json:"thinking,omitempty"`
}

// AgentToolDef describes a tool available to the agent.
type AgentToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

// BuildAgentPayload constructs the LLM request from a turn config and history.
func BuildAgentPayload(cfg AgentTurnConfig, history []AgentMessage, tools []AgentToolDef) AgentRunnerPayload {
	messages := make([]AgentMessage, 0, len(history)+1)
	messages = append(messages, history...)

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}

	payload := AgentRunnerPayload{
		Model:     cfg.Model,
		System:    cfg.SystemPrompt,
		Messages:  messages,
		MaxTokens: maxTokens,
		Tools:     tools,
		Stream:    true,
	}

	// Configure thinking based on think level.
	switch cfg.ThinkLevel {
	case ThinkOff, "":
		// No thinking config needed.
	case ThinkMinimal:
		payload.Thinking = &ThinkingConfig{Type: "enabled", BudgetTokens: 1024}
	case ThinkLow:
		payload.Thinking = &ThinkingConfig{Type: "enabled", BudgetTokens: 4096}
	case ThinkMedium:
		payload.Thinking = &ThinkingConfig{Type: "enabled", BudgetTokens: 10240}
	case ThinkHigh:
		payload.Thinking = &ThinkingConfig{Type: "enabled", BudgetTokens: 32768}
	case ThinkXHigh:
		payload.Thinking = &ThinkingConfig{Type: "enabled", BudgetTokens: 65536}
	case ThinkAdaptive:
		payload.Thinking = &ThinkingConfig{Type: "enabled", BudgetTokens: 16384}
	}

	return payload
}

// AgentRunnerMemory manages conversation context for agent execution.
type AgentRunnerMemory struct {
	mu         sync.Mutex
	history    []AgentMessage
	maxTokens  int
	usedTokens int
	// compaction tracking
	compactionCount  int
	totalCompacted   int
}

func NewAgentRunnerMemory(maxTokens int) *AgentRunnerMemory {
	if maxTokens <= 0 {
		maxTokens = DefaultContextTokens
	}
	return &AgentRunnerMemory{maxTokens: maxTokens}
}

func (m *AgentRunnerMemory) Append(msg AgentMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.history = append(m.history, msg)
	m.usedTokens += EstimateTokens(msg.Content)
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
		tokens := EstimateTokens(oldest.Content)
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
		removedTokens += EstimateTokens(m.history[i].Content)
	}

	// Replace removed messages with summary.
	summaryMsg := AgentMessage{
		Role:    "system",
		Content: fmt.Sprintf("[Conversation summary: %s]", summary),
	}

	newHistory := make([]AgentMessage, 0, 1+1+keepTail)
	newHistory = append(newHistory, m.history[0])       // system prompt
	newHistory = append(newHistory, summaryMsg)          // summary
	newHistory = append(newHistory, m.history[len(m.history)-keepTail:]...) // recent

	m.history = newHistory
	m.usedTokens -= removedTokens
	m.usedTokens += EstimateTokens(summary)
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
	Usage      TokenUsage
}

func BuildMemoryFlush(memory *AgentRunnerMemory, sessionKey, agentID string, usage TokenUsage) MemoryFlush {
	return MemoryFlush{
		SessionKey: sessionKey,
		AgentID:    agentID,
		Messages:   memory.History(),
		Timestamp:  time.Now().UnixMilli(),
		Usage:      usage,
	}
}

// --- Utility functions ---

func FormatUsageSummary(usage TokenUsage) string {
	if usage.TotalTokens == 0 {
		return ""
	}
	return fmt.Sprintf("%d tokens (in: %d, out: %d)", usage.TotalTokens, usage.InputTokens, usage.OutputTokens)
}

func FormatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	secs := float64(ms) / 1000.0
	if secs < 60 {
		return fmt.Sprintf("%.1fs", secs)
	}
	mins := int(secs / 60)
	remainSecs := int(secs) % 60
	return fmt.Sprintf("%dm%ds", mins, remainSecs)
}

func IsToolUseContent(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "{\"tool_use\"") || strings.HasPrefix(trimmed, "<tool_use>")
}

func buildToolResultMessage(toolUseID, output string, isError bool) AgentMessage {
	return AgentMessage{
		Role:      "user",
		ToolUseID: toolUseID,
		Content:   output,
		IsError:   isError,
	}
}

func formatToolInput(input map[string]any) string {
	if input == nil {
		return ""
	}
	data, err := json.Marshal(input)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func truncateToolOutput(output string, maxLen int) string {
	if len(output) <= maxLen {
		return output
	}
	return output[:maxLen] + "…[truncated]"
}
