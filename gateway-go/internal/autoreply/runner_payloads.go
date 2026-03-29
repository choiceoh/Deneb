package autoreply

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// ContentBlockKind represents content block type values in agent messages.
type ContentBlockKind string

const (
	ContentBlockText       ContentBlockKind = "text"
	ContentBlockToolUse    ContentBlockKind = "tool_use"
	ContentBlockToolResult ContentBlockKind = "tool_result"
	ContentBlockThinking   ContentBlockKind = "thinking"
)

// ThinkingType represents the reasoning mode in LLM payloads.
type ThinkingType string

const (
	ThinkingTypeEnabled  ThinkingType = "enabled"
	ThinkingTypeDisabled ThinkingType = "disabled"
)

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
	Type         ThinkingType `json:"type"`                    // "enabled" or "disabled"
	BudgetTokens int          `json:"budget_tokens,omitempty"` // token budget for thinking
}

// AgentMessage is a single message in the agent conversation.
type AgentMessage struct {
	Role          string         `json:"role"`
	Content       string         `json:"content,omitempty"`
	ToolUseID     string         `json:"tool_use_id,omitempty"`    // for tool_result
	IsError       bool           `json:"is_error,omitempty"`       // for tool_result
	ContentBlocks []ContentBlock `json:"content_blocks,omitempty"` // for multi-block messages
}

// ContentBlock represents a block within a message (text, tool_use, tool_result, thinking).
type ContentBlock struct {
	Type     ContentBlockKind `json:"type"` // "text", "tool_use", "tool_result", "thinking"
	Text     string           `json:"text,omitempty"`
	ID       string           `json:"id,omitempty"`
	Name     string           `json:"name,omitempty"`
	Input    map[string]any   `json:"input,omitempty"`
	Content  string           `json:"content,omitempty"`
	IsError  bool             `json:"is_error,omitempty"`
	Thinking string           `json:"thinking,omitempty"`
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
		maxTokens = model.DefaultMaxTokens
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
	case types.ThinkOff, "":
		// No thinking config needed.
	case types.ThinkMinimal:
		payload.Thinking = &ThinkingConfig{Type: ThinkingTypeEnabled, BudgetTokens: 1024}
	case types.ThinkLow:
		payload.Thinking = &ThinkingConfig{Type: ThinkingTypeEnabled, BudgetTokens: 4096}
	case types.ThinkMedium:
		payload.Thinking = &ThinkingConfig{Type: ThinkingTypeEnabled, BudgetTokens: 10240}
	case types.ThinkHigh:
		payload.Thinking = &ThinkingConfig{Type: ThinkingTypeEnabled, BudgetTokens: 32768}
	case types.ThinkXHigh:
		payload.Thinking = &ThinkingConfig{Type: ThinkingTypeEnabled, BudgetTokens: 65536}
	case types.ThinkAdaptive:
		payload.Thinking = &ThinkingConfig{Type: ThinkingTypeEnabled, BudgetTokens: 16384}
	}

	return payload
}

func buildToolResultMessage(toolUseID, output string, isError bool) AgentMessage {
	return AgentMessage{
		Role:      "user",
		ToolUseID: toolUseID,
		Content:   output,
		IsError:   isError,
	}
}

func IsToolUseContent(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "{\"tool_use\"") || strings.HasPrefix(trimmed, "<tool_use>")
}

func FormatUsageSummary(usage session.TokenUsage) string {
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
