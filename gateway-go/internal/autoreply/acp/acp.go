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
// Status and EndedAt are derived from session.Manager (via ACPRegistry.sessions);
// they are populated on read by ACPRegistry.Get/List.
type ACPAgent struct {
	ID           string `json:"id"`
	ParentID     string `json:"parentId,omitempty"`
	Role         string `json:"role,omitempty"`
	Status       string `json:"status"` // derived from session: "idle", "running", "done", "failed", "killed"
	SessionKey   string `json:"sessionKey"`
	SpawnedAt    int64  `json:"spawnedAt"`
	EndedAt      int64  `json:"endedAt,omitempty"` // derived from session
	WorkspaceDir string `json:"workspaceDir,omitempty"`
	Depth        int    `json:"depth"`
}

// ACPRegistry tracks spawned sub-agents.
//
// When a session.Manager is attached, Status and EndedAt are derived from
// session state on every Get/List call, so the registry is always in sync
// without a separate lifecycle-sync goroutine.
//
// Snapshot cache: every mutating operation bumps ver. List() rebuilds the
// cached snapshot only when ver has advanced, so repeated List("") calls
// between mutations return the same immutable slice at zero copy cost.
// Callers MUST NOT modify the returned slice.
type ACPRegistry struct {
	mu       sync.RWMutex
	agents   map[string]*ACPAgent
	sessions *session.Manager // optional; when set, Status/EndedAt derived from session
	ver      uint64           // bumped on every mutation
	snapVer  uint64           // ver at which snapAll was built
	snapAll  []ACPAgent       // immutable snapshot; rebuilt lazily on List
}

// NewACPRegistry creates a new ACP agent registry.
func NewACPRegistry() *ACPRegistry {
	return &ACPRegistry{
		agents: make(map[string]*ACPAgent),
	}
}

// SetSessionManager attaches a session.Manager for deriving agent status.
// Must be called before any agents are registered.
func (r *ACPRegistry) SetSessionManager(mgr *session.Manager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions = mgr
}

// bumpVer increments the version counter. Must be called under write lock.
func (r *ACPRegistry) bumpVer() {
	r.ver++
}

// enrichFromSession populates Status and EndedAt from session.Manager if available.
// Caller need not hold a lock — r.sessions is set once via SetSessionManager
// before any agents are registered, so the field is effectively immutable after init.
func (r *ACPRegistry) enrichFromSession(a *ACPAgent) {
	if r.sessions == nil || a.SessionKey == "" {
		return
	}
	sess := r.sessions.Get(a.SessionKey)
	if sess == nil {
		return
	}
	if mapped := mapSessionStatusToACP(sess.Status); mapped != "" {
		a.Status = mapped
	}
	if sess.EndedAt != nil {
		a.EndedAt = *sess.EndedAt
	}
}

// Register adds a sub-agent to the registry.
func (r *ACPRegistry) Register(agent ACPAgent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[agent.ID] = &agent
	r.bumpVer()
}

// RegisterIfUnderLimit atomically checks the active child count for parentID
// and registers the agent only if the count is below limit. Returns false
// if the limit would be exceeded.
func (r *ACPRegistry) RegisterIfUnderLimit(agent ACPAgent, parentID string, limit int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Count active children under this parent while holding the lock.
	count := 0
	for _, a := range r.agents {
		if a.ParentID == parentID {
			st := a.Status
			if r.sessions != nil {
				if sess := r.sessions.Get(a.SessionKey); sess != nil {
					st = string(sess.Status)
				}
			}
			if st == "idle" || st == "running" {
				count++
			}
		}
	}
	if count >= limit {
		return false
	}
	r.agents[agent.ID] = &agent
	r.bumpVer()
	return true
}

// Get returns an agent by ID. Status and EndedAt are derived from
// session.Manager when available.
func (r *ACPRegistry) Get(id string) *ACPAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a := r.agents[id]
	if a == nil {
		return nil
	}
	cp := *a
	r.enrichFromSession(&cp)
	return &cp
}

// List returns all agents, optionally filtered by parent.
// Status and EndedAt are derived from session.Manager when available.
//
// When a session.Manager is attached, snapshots are always freshly enriched
// because session state may change between calls. Without a session.Manager,
// the original O(1) snapshot cache applies.
// Callers MUST NOT modify the returned slice.
func (r *ACPRegistry) List(parentID string) []ACPAgent {
	r.mu.RLock()
	hasSessionMgr := r.sessions != nil

	// Fast path (no session manager): serve from cached snapshot.
	if !hasSessionMgr && r.snapVer == r.ver && r.snapAll != nil {
		snap := r.snapAll
		r.mu.RUnlock()
		if parentID == "" {
			return snap
		}
		return filterByParent(snap, parentID)
	}
	r.mu.RUnlock()

	// Slow path: rebuild snapshot under write lock.
	r.mu.Lock()
	if r.snapVer != r.ver || r.snapAll == nil || hasSessionMgr {
		snap := make([]ACPAgent, 0, len(r.agents))
		for _, a := range r.agents {
			cp := *a
			r.enrichFromSession(&cp)
			snap = append(snap, cp)
		}
		r.snapAll = snap
		r.snapVer = r.ver
	}
	snap := r.snapAll
	r.mu.Unlock()

	if parentID == "" {
		return snap
	}
	return filterByParent(snap, parentID)
}

// filterByParent returns agents whose ParentID matches. Allocates a new slice.
func filterByParent(snap []ACPAgent, parentID string) []ACPAgent {
	var result []ACPAgent
	for i := range snap {
		if snap[i].ParentID == parentID {
			result = append(result, snap[i])
		}
	}
	return result
}

// ActiveCount returns the number of active (non-terminal) agents.
func (r *ACPRegistry) ActiveCount(parentID string) int {
	snap := r.List(parentID)
	count := 0
	for _, a := range snap {
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
	r.bumpVer()
	return true
}

// Remove deletes an agent from the registry.
func (r *ACPRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.agents, id)
	r.bumpVer()
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
			r.bumpVer()
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
//
// When a session.Manager is attached to the registry (via SetSessionManager),
// Status/EndedAt are derived on read, making this sync redundant. The function
// still works for backward compatibility when no session.Manager is set.
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
	OutputText string
	TokensUsed ACPTokenUsage
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
