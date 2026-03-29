// runner_memory.go — Conversation context management for agent execution.
//
// AgentRunnerMemory tracks the message history and token budget for a single
// agent run. Compact/CompactWithSummary trim older messages when the history
// grows beyond the token budget. MemoryFlush and BuildMemoryFlush support
// persisting the conversation to storage before compaction.
package autoreply

import (
	"fmt"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/session"
)

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
