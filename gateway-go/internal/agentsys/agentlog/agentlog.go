// Package agentlog provides detailed JSONL logging for AI agent runs.
//
// Each agent run records structured events (start, prep, turn, tool, end, error)
// to a per-session JSONL file under ~/.deneb/agent-logs/{sessionKey}.jsonl.
// The AI agent can query its own past run logs via the agent_logs tool
// to diagnose issues and understand prior execution context.
package agentlog

import "encoding/json"

// LogEntry is a single line in the agent log JSONL file.
type LogEntry struct {
	Ts      int64           `json:"ts"` //nolint:staticcheck // ST1003 — JSON field name
	Type    string          `json:"type"`
	RunID   string          `json:"runId"`
	Session string          `json:"session"`
	Data    json.RawMessage `json:"data"`
}

// Log entry types.
const (
	TypeRunStart = "run.start"
	TypeRunPrep  = "run.prep"
	TypeTurnLLM  = "turn.llm"
	TypeTurnTool = "turn.tool"
	TypeRunEnd   = "run.end"
	TypeRunError = "run.error"
)

// RunStartData records agent run initialization.
type RunStartData struct {
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Message  string `json:"message"` // user input (truncated to maxMessageLen)
	Channel  string `json:"channel,omitempty"`
}

// RunPrepData records context assembly metrics.
type RunPrepData struct {
	SystemPromptChars int   `json:"systemPromptChars"`
	ContextMessages   int   `json:"contextMessages"`
	PrepMs            int64 `json:"prepMs"`
}

// TurnLLMData records a single LLM turn result.
type TurnLLMData struct {
	Turn         int    `json:"turn"`
	InputTokens  int    `json:"inputTokens"`
	OutputTokens int    `json:"outputTokens"`
	StopReason   string `json:"stopReason,omitempty"`
	TextLen      int    `json:"textLen"`
	ToolCalls    int    `json:"toolCalls"`
}

// TurnToolData records a single tool execution within a turn.
type TurnToolData struct {
	Turn       int    `json:"turn"`
	Name       string `json:"name"`
	DurationMs int64  `json:"durationMs"`
	OutputLen  int    `json:"outputLen"`
	IsError    bool   `json:"isError,omitempty"`
	Error      string `json:"error,omitempty"`
}

// RunEndData records agent run completion.
type RunEndData struct {
	StopReason   string `json:"stopReason"`
	Turns        int    `json:"turns"`
	InputTokens  int    `json:"inputTokens"`
	OutputTokens int    `json:"outputTokens"`
	TotalMs      int64  `json:"totalMs"`
	TextLen      int    `json:"textLen"`
}

// RunErrorData records agent run failure.
type RunErrorData struct {
	Error   string `json:"error"`
	Aborted bool   `json:"aborted,omitempty"`
}
