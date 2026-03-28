// acp.go — Agent Control Protocol (ACP) integration.
// Mirrors src/auto-reply/reply/acp-projector.ts (512 LOC),
// commands-acp/ (7 files), commands-subagents/ (11 files),
// dispatch-acp.ts (367 LOC), dispatch-acp-delivery.ts (189 LOC),
// acp-stream-settings.ts, acp-reset-target.ts.
package acp

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

// ACPAgent represents a spawned sub-agent.
type ACPAgent struct {
	ID           string `json:"id"`
	ParentID     string `json:"parentId,omitempty"`
	Role         string `json:"role,omitempty"`
	Status       string `json:"status"` // "idle", "running", "done", "failed", "killed"
	SessionKey   string `json:"sessionKey"`
	SpawnedAt    int64  `json:"spawnedAt"`
	EndedAt      int64  `json:"endedAt,omitempty"`
	WorkspaceDir string `json:"workspaceDir,omitempty"`
	Depth        int    `json:"depth"`
}

// ACPRegistry tracks spawned sub-agents.
type ACPRegistry struct {
	mu     sync.RWMutex
	agents map[string]*ACPAgent
}

// NewACPRegistry creates a new ACP agent registry.
func NewACPRegistry() *ACPRegistry {
	return &ACPRegistry{
		agents: make(map[string]*ACPAgent),
	}
}

// Register adds a sub-agent to the registry.
func (r *ACPRegistry) Register(agent ACPAgent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[agent.ID] = &agent
}

// Get returns an agent by ID.
func (r *ACPRegistry) Get(id string) *ACPAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a := r.agents[id]
	if a == nil {
		return nil
	}
	cp := *a
	return &cp
}

// List returns all agents, optionally filtered by parent.
func (r *ACPRegistry) List(parentID string) []ACPAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []ACPAgent
	for _, a := range r.agents {
		if parentID != "" && a.ParentID != parentID {
			continue
		}
		result = append(result, *a)
	}
	return result
}

// ActiveCount returns the number of active (non-terminal) agents.
func (r *ACPRegistry) ActiveCount(parentID string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, a := range r.agents {
		if parentID != "" && a.ParentID != parentID {
			continue
		}
		if a.Status == "idle" || a.Status == "running" {
			count++
		}
	}
	return count
}

// Kill marks an agent as killed.
func (r *ACPRegistry) Kill(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.agents[id]
	if !ok {
		return false
	}
	a.Status = "killed"
	a.EndedAt = time.Now().UnixMilli()
	return true
}

// Remove deletes an agent from the registry.
func (r *ACPRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.agents, id)
}

// UpdateStatusBySessionKey finds an agent by session key and updates its status.
// Returns true if an agent was found and updated.
func (r *ACPRegistry) UpdateStatusBySessionKey(sessionKey, status string, endedAt int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.agents {
		if a.SessionKey == sessionKey {
			a.Status = status
			if endedAt > 0 {
				a.EndedAt = endedAt
			}
			return true
		}
	}
	return false
}

// mapSessionStatusToACP converts a session RunStatus to an ACP agent status string.
func mapSessionStatusToACP(status session.RunStatus) string {
	switch status {
	case session.StatusRunning:
		return "running"
	case session.StatusDone:
		return "done"
	case session.StatusFailed:
		return "failed"
	case session.StatusKilled:
		return "killed"
	case session.StatusTimeout:
		return "failed"
	default:
		return ""
	}
}

// StartACPLifecycleSync subscribes to session lifecycle events and keeps
// the ACPRegistry agent statuses in sync. Returns an unsubscribe function.
func StartACPLifecycleSync(registry *ACPRegistry, eventBus *session.EventBus) func() {
	return eventBus.Subscribe(func(event session.Event) {
		if event.Kind != session.EventStatusChanged {
			return
		}
		acpStatus := mapSessionStatusToACP(event.NewStatus)
		if acpStatus == "" {
			return
		}
		var endedAt int64
		if event.NewStatus != session.StatusRunning {
			endedAt = time.Now().UnixMilli()
		}
		registry.UpdateStatusBySessionKey(event.Key, acpStatus, endedAt)
	})
}

// ACPTurnResult is a minimal result type used by ACPProjector to render
// sub-agent output. The autoreply root package maps AgentTurnResult onto this.
type ACPTurnResult struct {
	OutputText  string
	TokensUsed  ACPTokenUsage
}

// ACPTokenUsage tracks token consumption summary for ACP display purposes.
type ACPTokenUsage struct {
	TotalTokens int64
}

// ACPProjector projects ACP sub-agent results into the parent chat.
type ACPProjector struct {
	registry *ACPRegistry
}

// NewACPProjector creates a new ACP projector.
func NewACPProjector(registry *ACPRegistry) *ACPProjector {
	return &ACPProjector{registry: registry}
}

// ProjectResult formats a sub-agent result for display in the parent chat.
func (p *ACPProjector) ProjectResult(agentID string, result *ACPTurnResult) string {
	agent := p.registry.Get(agentID)
	if agent == nil {
		return result.OutputText
	}

	var parts []string
	role := agent.Role
	if role == "" {
		role = agentID
	}
	parts = append(parts, fmt.Sprintf("**[%s]**", role))

	if result.OutputText != "" {
		parts = append(parts, result.OutputText)
	}

	if result.TokensUsed.TotalTokens > 0 {
		parts = append(parts, fmt.Sprintf("_%s_", formatACPTokenSummary(result.TokensUsed)))
	}

	return strings.Join(parts, "\n")
}

// formatACPTokenSummary returns a brief token usage summary string.
func formatACPTokenSummary(usage ACPTokenUsage) string {
	return fmt.Sprintf("%d tokens", usage.TotalTokens)
}

// --- Subagent command helpers ---

// SubagentListEntry is a display-friendly subagent summary.
type SubagentListEntry struct {
	ID     string `json:"id"`
	Role   string `json:"role"`
	Status string `json:"status"`
	Depth  int    `json:"depth"`
}

// FormatSubagentList formats a list of subagents for display.
func FormatSubagentList(agents []ACPAgent) string {
	if len(agents) == 0 {
		return "No active subagents."
	}
	var lines []string
	for _, a := range agents {
		status := a.Status
		if status == "running" {
			status = "🟢 running"
		} else if status == "idle" {
			status = "🟡 idle"
		} else if status == "done" {
			status = "✅ done"
		} else if status == "failed" {
			status = "❌ failed"
		} else if status == "killed" {
			status = "💀 killed"
		}
		role := a.Role
		if role == "" {
			role = a.ID
		}
		lines = append(lines, fmt.Sprintf("• **%s** — %s", role, status))
	}
	return strings.Join(lines, "\n")
}
