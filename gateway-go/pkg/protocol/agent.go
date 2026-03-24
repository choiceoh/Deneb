// Package protocol — agent execution wire types.
//
// These types mirror the Protobuf definitions in proto/agent.proto
// and the TypeScript types in src/protocol/generated/agent.ts.
package protocol

// AgentStatus tracks the lifecycle of an agent execution.
// Mirrors proto/agent.proto AgentStatus enum.
type AgentStatus string

const (
	AgentStatusUnspecified AgentStatus = ""
	AgentStatusSpawning   AgentStatus = "spawning"
	AgentStatusRunning    AgentStatus = "running"
	AgentStatusCompleted  AgentStatus = "completed"
	AgentStatusFailed     AgentStatus = "failed"
	AgentStatusKilled     AgentStatus = "killed"
)

// AgentSpawnRequest initiates a new agent execution.
// Mirrors proto/agent.proto AgentSpawnRequest.
type AgentSpawnRequest struct {
	SessionKey       string  `json:"sessionKey"`
	Model            *string `json:"model,omitempty"`
	Provider         *string `json:"provider,omitempty"`
	Prompt           *string `json:"prompt,omitempty"`
	ThinkingLevel    *string `json:"thinkingLevel,omitempty"`
	ParentSessionKey *string `json:"parentSessionKey,omitempty"`
}

// AgentStatusUpdate reports a change in agent execution state.
// Mirrors proto/agent.proto AgentStatusUpdate.
type AgentStatusUpdate struct {
	SessionKey  string      `json:"sessionKey"`
	Status      AgentStatus `json:"status"`
	TimestampMs *int64      `json:"timestampMs,omitempty"`
	Reason      *string     `json:"reason,omitempty"`
}

// AgentExecutionResult is the final result of an agent execution.
// Mirrors proto/agent.proto AgentExecutionResult.
type AgentExecutionResult struct {
	SessionKey       string      `json:"sessionKey"`
	FinalStatus      AgentStatus `json:"finalStatus"`
	Output           *string     `json:"output,omitempty"`
	RuntimeMs        *int64      `json:"runtimeMs,omitempty"`
	InputTokens      *uint64     `json:"inputTokens,omitempty"`
	OutputTokens     *uint64     `json:"outputTokens,omitempty"`
	EstimatedCostUsd *float64    `json:"estimatedCostUsd,omitempty"`
}

// IsTerminal returns true if the agent status represents a final state.
func (s AgentStatus) IsTerminal() bool {
	switch s {
	case AgentStatusCompleted, AgentStatusFailed, AgentStatusKilled:
		return true
	default:
		return false
	}
}
