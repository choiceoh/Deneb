package acp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// SubagentInfraDeps provides the full dependency interface for subagent/ACP
// infrastructure. This connects to the actual ACPRegistry, session manager,
// and followup queue infrastructure.
type SubagentInfraDeps struct {
	ACPRegistry  *ACPRegistry
	ACPProjector *ACPProjector
	Sessions     *session.Manager // session lifecycle for subagent sessions
	SessionStore func(key string) *types.SessionState
	SaveSession  func(session *types.SessionState) error
	AbortSession func(sessionKey string) error
	Logger       *slog.Logger
}

// SpawnSubagentParams holds the parameters for spawning a sub-agent.
type SpawnSubagentParams struct {
	ParentSessionKey string
	ParentAgentID    string
	Role             string
	WorkspaceDir     string
	Model            string
	Provider         string
	InitialMessage   string
	MaxDepth         int
	ToolPreset       string // tool preset restricting available tools (researcher, implementer, verifier)
}

// SpawnSubagentResult holds the result of spawning a sub-agent.
type SpawnSubagentResult struct {
	AgentID    string
	SessionKey string
	Error      error
}

// SpawnSubagent creates a new sub-agent, registers it in the ACP registry,
// and initializes its session state.
func (d *SubagentInfraDeps) SpawnSubagent(ctx context.Context, params SpawnSubagentParams) SpawnSubagentResult {
	if d.ACPRegistry == nil {
		return SpawnSubagentResult{Error: fmt.Errorf("ACP registry not available")}
	}

	// Determine depth.
	parentAgent := d.ACPRegistry.Get(params.ParentAgentID)
	depth := 0
	if parentAgent != nil {
		depth = parentAgent.Depth + 1
	}

	// Enforce max depth.
	maxDepth := params.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 5
	}
	if depth >= maxDepth {
		return SpawnSubagentResult{
			Error: fmt.Errorf("max subagent depth (%d) reached", maxDepth),
		}
	}

	// Generate agent ID and session key.
	agentID := shortid.New("sub_" + sanitizeAgentID(params.Role))
	sessionKey := fmt.Sprintf("acp:%s:%s", params.ParentSessionKey, agentID)

	// Register in ACP registry with atomic breadth limit check.
	const maxChildrenPerParent = 10
	agent := ACPAgent{
		ID:           agentID,
		ParentID:     params.ParentAgentID,
		Role:         params.Role,
		Status:       "idle",
		SessionKey:   sessionKey,
		SpawnedAt:    time.Now().UnixMilli(),
		WorkspaceDir: params.WorkspaceDir,
		Depth:        depth,
	}
	if params.ParentAgentID != "" {
		if !d.ACPRegistry.RegisterIfUnderLimit(agent, params.ParentAgentID, maxChildrenPerParent) {
			return SpawnSubagentResult{
				Error: fmt.Errorf("max concurrent children (%d) reached for parent %s", maxChildrenPerParent, params.ParentAgentID),
			}
		}
	} else {
		d.ACPRegistry.Register(agent)
	}

	// Create KindSubagent session in session.Manager for lifecycle tracking and GC.
	// Set a 30-minute absolute timeout and 10-minute idle timeout for stall detection.
	if d.Sessions != nil {
		sess := d.Sessions.Create(sessionKey, session.KindSubagent)
		if sess != nil {
			timeoutAt := time.Now().Add(30 * time.Minute).UnixMilli()
			sess.TimeoutAt = &timeoutAt
			sess.IdleTimeoutMs = 10 * 60 * 1000 // 10 minutes idle → stall
			now := time.Now().UnixMilli()
			sess.LastActivityAt = &now
			if err := d.Sessions.Set(sess); err != nil {
				d.logger().Warn("failed to persist subagent session timeout",
					"agentId", agentID, "sessionKey", sessionKey, "error", err)
			}
		}
	}

	// Create session state if SaveSession is available.
	if d.SaveSession != nil {
		sess := &types.SessionState{
			SessionOrigin: types.SessionOrigin{
				SessionKey: sessionKey,
				Channel:    "acp",
			},
			AgentID:    agentID,
			Model:      params.Model,
			Provider:   params.Provider,
			ToolPreset: params.ToolPreset,
		}
		if err := d.SaveSession(sess); err != nil {
			d.logger().Warn("failed to save subagent session",
				"agentId", agentID,
				"error", err,
			)
		}
	}

	return SpawnSubagentResult{
		AgentID:    agentID,
		SessionKey: sessionKey,
	}
}

// KillSubagent kills a sub-agent and its descendants.
func (d *SubagentInfraDeps) KillSubagent(agentID string) error {
	if d.ACPRegistry == nil {
		return fmt.Errorf("ACP registry not available")
	}

	agent := d.ACPRegistry.Get(agentID)
	if agent == nil {
		return fmt.Errorf("agent %q not found", agentID)
	}

	// Kill descendants first (best-effort: log failures but continue cleanup).
	children := d.ACPRegistry.List(agentID)
	for _, child := range children {
		if err := d.KillSubagent(child.ID); err != nil {
			d.logger().Warn("failed to kill child subagent",
				"parentId", agentID, "childId", child.ID, "error", err)
		}
	}

	// Kill the agent.
	d.ACPRegistry.Kill(agentID)

	// Abort its session if possible.
	if d.AbortSession != nil && agent.SessionKey != "" {
		if err := d.AbortSession(agent.SessionKey); err != nil {
			d.logger().Warn("failed to abort subagent session",
				"agentId", agentID, "sessionKey", agent.SessionKey, "error", err)
		}
	}

	return nil
}

// ListSubagents returns a display-formatted list of sub-agents.
func (d *SubagentInfraDeps) ListSubagents(parentID string) string {
	if d.ACPRegistry == nil {
		return "No subagent system available."
	}
	agents := d.ACPRegistry.List(parentID)
	return FormatSubagentList(agents)
}

// ActiveSubagentCount returns the number of active sub-agents.
func (d *SubagentInfraDeps) ActiveSubagentCount(parentID string) int {
	if d.ACPRegistry == nil {
		return 0
	}
	return d.ACPRegistry.ActiveCount(parentID)
}

// ResetSubagent performs an ACP reset-in-place for a bound conversation.
func (d *SubagentInfraDeps) ResetSubagent(agentID, reason string) error {
	if d.ACPRegistry == nil {
		return fmt.Errorf("ACP registry not available")
	}

	agent := d.ACPRegistry.Get(agentID)
	if agent == nil {
		return fmt.Errorf("agent %q not found", agentID)
	}

	// Cannot reset a running agent — kill it first.
	if agent.Status == "running" {
		return fmt.Errorf("cannot reset running agent %q; kill it first", agentID)
	}

	d.logger().Info("resetting subagent", "agentId", agentID, "previousStatus", agent.Status, "reason", reason)

	// Abort any running session.
	if d.AbortSession != nil && agent.SessionKey != "" {
		if err := d.AbortSession(agent.SessionKey); err != nil {
			d.logger().Warn("failed to abort session during reset",
				"agentId", agentID, "sessionKey", agent.SessionKey, "error", err)
		}
	}

	// Re-register as idle with fresh timestamp.
	agent.Status = "idle"
	agent.EndedAt = 0
	agent.SpawnedAt = time.Now().UnixMilli()
	d.ACPRegistry.Register(*agent)

	return nil
}

func (d *SubagentInfraDeps) logger() *slog.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return slog.Default()
}

func sanitizeAgentID(role string) string {
	r := strings.ToLower(strings.TrimSpace(role))
	if r == "" {
		return "agent"
	}
	// Replace non-alphanumeric with underscores.
	var b strings.Builder
	for _, c := range r {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		} else {
			b.WriteRune('_')
		}
	}
	result := b.String()
	if len(result) > 20 {
		result = result[:20]
	}
	return result
}
