// agent_runner.go — Core LLM agent execution engine.
// Mirrors src/auto-reply/reply/agent-runner.ts, agent-runner-execution.ts,
// agent-runner-memory.ts, agent-runner-payloads.ts, agent-runner-utils.ts,
// agent-runner-helpers.ts, agent-runner-reminder-guard.ts.
package autoreply

import (
	"context"
	"fmt"
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
}

// TokenUsage tracks token consumption for an agent turn.
type TokenUsage struct {
	InputTokens       int64 `json:"inputTokens,omitempty"`
	OutputTokens      int64 `json:"outputTokens,omitempty"`
	TotalTokens       int64 `json:"totalTokens,omitempty"`
	CacheReadTokens   int64 `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens  int64 `json:"cacheWriteTokens,omitempty"`
}

// AgentExecutor runs LLM agent turns with tool execution and streaming.
// This is the Go equivalent of the TS agent-runner.ts module.
type AgentExecutor interface {
	// RunTurn executes a single agent turn (LLM call + tool execution loop).
	RunTurn(ctx context.Context, cfg AgentTurnConfig) (*AgentTurnResult, error)
}

// ReminderGuard prevents infinite reminder loops during agent execution.
type ReminderGuard struct {
	mu       sync.Mutex
	count    int
	maxCount int
}

// NewReminderGuard creates a guard that limits reminder injections.
func NewReminderGuard(maxCount int) *ReminderGuard {
	if maxCount <= 0 {
		maxCount = 3
	}
	return &ReminderGuard{maxCount: maxCount}
}

// TryRemind returns true if a reminder is allowed, false if limit reached.
func (g *ReminderGuard) TryRemind() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.count >= g.maxCount {
		return false
	}
	g.count++
	return true
}

// Reset clears the reminder counter.
func (g *ReminderGuard) Reset() {
	g.mu.Lock()
	g.count = 0
	g.mu.Unlock()
}

// AgentRunnerPayload builds the LLM request payload from an agent turn config.
type AgentRunnerPayload struct {
	Model       string            `json:"model"`
	System      string            `json:"system,omitempty"`
	Messages    []AgentMessage    `json:"messages"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Tools       []AgentToolDef    `json:"tools,omitempty"`
	Stream      bool              `json:"stream,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
}

// AgentMessage is a single message in the agent conversation.
type AgentMessage struct {
	Role    string `json:"role"` // "user", "assistant", "system"
	Content string `json:"content"`
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
	if cfg.Message != "" {
		messages = append(messages, AgentMessage{Role: "user", Content: cfg.Message})
	}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}

	return AgentRunnerPayload{
		Model:     cfg.Model,
		System:    cfg.SystemPrompt,
		Messages:  messages,
		MaxTokens: maxTokens,
		Tools:     tools,
		Stream:    true,
	}
}

// AgentRunnerMemory manages conversation context for agent execution.
type AgentRunnerMemory struct {
	mu         sync.Mutex
	history    []AgentMessage
	maxTokens  int
	usedTokens int
}

// NewAgentRunnerMemory creates a memory manager with a token budget.
func NewAgentRunnerMemory(maxTokens int) *AgentRunnerMemory {
	if maxTokens <= 0 {
		maxTokens = DefaultContextTokens
	}
	return &AgentRunnerMemory{maxTokens: maxTokens}
}

// Append adds a message to the conversation history.
func (m *AgentRunnerMemory) Append(msg AgentMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.history = append(m.history, msg)
	m.usedTokens += EstimateTokens(msg.Content)
}

// History returns the current conversation history.
func (m *AgentRunnerMemory) History() []AgentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]AgentMessage, len(m.history))
	copy(result, m.history)
	return result
}

// Compact removes older messages to fit within the token budget.
func (m *AgentRunnerMemory) Compact() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.usedTokens <= m.maxTokens {
		return 0
	}

	removed := 0
	for m.usedTokens > m.maxTokens && len(m.history) > 2 {
		// Keep first (system) and last message, remove from the middle.
		oldest := m.history[1]
		m.history = append(m.history[:1], m.history[2:]...)
		m.usedTokens -= EstimateTokens(oldest.Content)
		removed++
	}
	return removed
}

// Clear removes all history.
func (m *AgentRunnerMemory) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.history = nil
	m.usedTokens = 0
}

// UsedTokens returns the estimated token count.
func (m *AgentRunnerMemory) UsedTokens() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usedTokens
}

// MemoryFlush writes conversation history to persistent storage.
type MemoryFlush struct {
	SessionKey string
	AgentID    string
	Messages   []AgentMessage
	Timestamp  int64
}

// BuildMemoryFlush creates a flush payload from current memory state.
func BuildMemoryFlush(memory *AgentRunnerMemory, sessionKey, agentID string) MemoryFlush {
	return MemoryFlush{
		SessionKey: sessionKey,
		AgentID:    agentID,
		Messages:   memory.History(),
		Timestamp:  time.Now().UnixMilli(),
	}
}

// AgentRunnerUtils provides utility functions for the agent runner.

// FormatUsageSummary builds a compact usage summary string.
func FormatUsageSummary(usage TokenUsage) string {
	if usage.TotalTokens == 0 {
		return ""
	}
	return fmt.Sprintf("%d tokens (in: %d, out: %d)", usage.TotalTokens, usage.InputTokens, usage.OutputTokens)
}

// FormatDuration formats a duration in milliseconds to a human-readable string.
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

// IsToolUseContent checks if text looks like a tool use response.
func IsToolUseContent(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "{\"tool_use\"") || strings.HasPrefix(trimmed, "<tool_use>")
}
