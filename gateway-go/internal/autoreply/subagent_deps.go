package autoreply

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/queue"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/shortid"
)

// SubagentInfraDeps provides the full dependency interface for subagent/ACP
// infrastructure. This connects to the actual ACPRegistry, session manager,
// and followup queue infrastructure.
type SubagentInfraDeps struct {
	ACPRegistry   *ACPRegistry
	ACPProjector  *ACPProjector
	FollowupQueue *queue.FollowupQueueRegistry
	SessionStore  func(key string) *types.SessionState
	SaveSession   func(session *types.SessionState) error
	AbortSession  func(sessionKey string) error
	Logger        *slog.Logger
}

// SpawnSubagentParams holds the parameters for spawning a sub-agent.
type SpawnSubagentParams struct {
	ParentSessionKey string
	ParentAgentID    string
	Role             string
	WorkspaceDir     string
	Model            string
	Provider         string
	ThinkLevel       types.ThinkLevel
	InitialMessage   string
	MaxDepth         int
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

	// Register in ACP registry.
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
	if err := d.ACPRegistry.Register(agent); err != nil {
		return SpawnSubagentResult{Error: fmt.Errorf("register agent: %w", err)}
	}

	// Create session state if SaveSession is available.
	if d.SaveSession != nil {
		session := &types.SessionState{
			SessionKey:      sessionKey,
			AgentID:         agentID,
			Channel:         "acp",
			Model:           params.Model,
			Provider:        params.Provider,
			ThinkLevel:      params.ThinkLevel,
			GroupActivation: types.ActivationAlways,
		}
		if err := d.SaveSession(session); err != nil {
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

	// Kill descendants first.
	children := d.ACPRegistry.List(agentID)
	for _, child := range children {
		_ = d.KillSubagent(child.ID)
	}

	// Kill the agent.
	d.ACPRegistry.Kill(agentID)

	// Abort its session if possible.
	if d.AbortSession != nil && agent.SessionKey != "" {
		_ = d.AbortSession(agent.SessionKey)
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

	// Abort any running session.
	if d.AbortSession != nil && agent.SessionKey != "" {
		_ = d.AbortSession(agent.SessionKey)
	}

	// Re-register as idle (update, not new — limit won't apply).
	agent.Status = "idle"
	agent.EndedAt = 0
	_ = d.ACPRegistry.Register(*agent)

	return nil
}

// EnqueueFollowup adds a followup message for a session.
func (d *SubagentInfraDeps) EnqueueFollowup(sessionKey, text string, run *types.FollowupRunContext) {
	if d.FollowupQueue == nil {
		return
	}
	d.FollowupQueue.EnqueueFollowupRun(
		sessionKey,
		types.FollowupRun{
			Prompt:     text,
			Run:        run,
			EnqueuedAt: time.Now().UnixMilli(),
		},
		types.FollowupQueueSettings{},
		types.DedupeNone,
		queue.NewRecentMessageIDCache(),
	)
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
