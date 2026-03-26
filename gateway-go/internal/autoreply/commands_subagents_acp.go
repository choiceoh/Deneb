// commands_subagents_acp.go — Wires SubagentCommandDeps to the ACPRegistry.
// Bridges the abstract SubagentCommandDeps interface to the concrete ACP
// sub-agent registry so that /subagents commands work against real state.
package autoreply

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// NewSubagentCommandDepsFromACP creates a SubagentCommandDeps backed by
// the given ACPRegistry. This is the production wiring point.
func NewSubagentCommandDepsFromACP(registry *ACPRegistry) *SubagentCommandDeps {
	return &SubagentCommandDeps{
		ListRuns: func(controllerKey string) []SubagentRunRecord {
			return acpAgentsToRunRecords(registry.List(""), controllerKey)
		},
		Kill: &SubagentKillDeps{
			KillRun: func(runID string) (bool, error) {
				return registry.Kill(runID), nil
			},
			KillAll: func(controllerKey string, runs []SubagentRunRecord) (int, error) {
				killed := 0
				for _, run := range runs {
					if run.EndedAt > 0 {
						continue
					}
					if registry.Kill(run.RunID) {
						killed++
					}
				}
				return killed, nil
			},
		},
		Send:    nil, // Send/steer requires agent turn execution — not yet wired.
		Spawn:   nil, // Spawn requires ACP spawn protocol — not yet wired.
		Focus:   nil, // Focus requires session binding service — not yet wired.
		Unfocus: nil, // Unfocus requires session binding service — not yet wired.
		Agents:  nil, // Agents requires session binding service — not yet wired.
		Log: &SubagentLogDeps{
			GetHistory: func(sessionKey string, limit int) ([]ChatLogMessage, error) {
				// Stub: actual transcript loading needs SessionManager.
				return nil, fmt.Errorf("transcript loading not yet available in Go gateway")
			},
		},
	}
}

// acpAgentsToRunRecords converts ACPAgent entries to SubagentRunRecord.
// Filters to agents parented by controllerKey (or all if controllerKey is empty).
func acpAgentsToRunRecords(agents []ACPAgent, controllerKey string) []SubagentRunRecord {
	var records []SubagentRunRecord
	for _, a := range agents {
		// Filter by controller: match on ParentID or SessionKey relationships.
		if controllerKey != "" && a.ParentID != controllerKey {
			continue
		}
		records = append(records, acpAgentToRunRecord(a))
	}
	// Sort: active first, then by spawn time descending.
	sort.Slice(records, func(i, j int) bool {
		iActive := records[i].EndedAt == 0
		jActive := records[j].EndedAt == 0
		if iActive != jActive {
			return iActive
		}
		return records[i].CreatedAt > records[j].CreatedAt
	})
	return records
}

// acpAgentToRunRecord converts a single ACPAgent to SubagentRunRecord.
func acpAgentToRunRecord(a ACPAgent) SubagentRunRecord {
	status := ""
	switch a.Status {
	case "done":
		status = "done"
	case "failed":
		status = "error"
	case "killed":
		status = "killed"
	case "running", "idle":
		status = ""
	}

	return SubagentRunRecord{
		RunID:           a.ID,
		ChildSessionKey: a.SessionKey,
		ControllerKey:   a.ParentID,
		RequesterKey:    a.ParentID,
		Task:            a.Role, // ACP uses Role as the task descriptor.
		Label:           resolveACPLabel(a),
		SpawnDepth:      a.Depth,
		CreatedAt:       a.SpawnedAt,
		StartedAt:       a.SpawnedAt,
		EndedAt:         a.EndedAt,
		OutcomeStatus:   status,
		Cleanup:         "keep",
		WorkspaceDir:    a.WorkspaceDir,
	}
}

// resolveACPLabel derives a display label from the ACP agent.
func resolveACPLabel(a ACPAgent) string {
	if a.Role != "" {
		return a.Role
	}
	if len(a.ID) >= 8 {
		return a.ID[:8]
	}
	return a.ID
}

// ACPSubagentCommandHandler is a convenience wrapper that dispatches
// subagent commands using an ACPRegistry-backed deps.
type ACPSubagentCommandHandler struct {
	registry *ACPRegistry
	deps     *SubagentCommandDeps
}

// NewACPSubagentCommandHandler creates a handler wired to the given registry.
func NewACPSubagentCommandHandler(registry *ACPRegistry) *ACPSubagentCommandHandler {
	deps := NewSubagentCommandDepsFromACP(registry)
	return &ACPSubagentCommandHandler{
		registry: registry,
		deps:     deps,
	}
}

// Handle dispatches a subagent command. Returns nil if not a subagent command.
func (h *ACPSubagentCommandHandler) Handle(
	normalized string,
	sessionKey string,
	channel string,
	accountID string,
	threadID string,
	senderID string,
	isGroup bool,
	isAuthorized bool,
) *SubagentCommandResult {
	return HandleSubagentsCommand(
		normalized, sessionKey, channel, accountID, threadID,
		senderID, isGroup, isAuthorized, h.deps,
	)
}

// RegisterACPSubagentRPC registers the subagent command handler on a
// CommandRouter, adding the /subagents family as recognized text commands.
func RegisterACPSubagentRPC(router *CommandRouter, registry *ACPRegistry) {
	handler := NewACPSubagentCommandHandler(registry)

	// Register all subagent command prefixes as text aliases.
	prefixes := []string{
		"/subagents", "/kill", "/steer", "/tell",
		"/focus", "/unfocus", "/agents",
	}
	for _, prefix := range prefixes {
		localPrefix := prefix
		router.RegisterTextHandler(localPrefix, func(ctx CommandContext) (*CommandResult, error) {
			result := handler.Handle(
				strings.TrimSpace(localPrefix+" "+ctx.Args.Raw),
				ctx.SessionKey,
				ctx.Channel,
				ctx.AccountID,
				"", // threadID from ctx.Msg
				ctx.Msg.SenderID,
				ctx.IsGroup,
				true, // authorized (already passed command gating)
			)
			if result == nil {
				return nil, nil
			}
			return &CommandResult{
				Reply:     result.Reply,
				SkipAgent: result.ShouldStop,
			}, nil
		})
	}
}

// RegisterTextHandler adds a text command handler to the router.
func (r *CommandRouter) RegisterTextHandler(prefix string, handler func(CommandContext) (*CommandResult, error)) {
	if r.handlers == nil {
		r.handlers = make(map[string]CommandHandler)
	}
	canonical := strings.TrimPrefix(prefix, "/")
	r.handlers[canonical] = func(ctx CommandContext) (*CommandResult, error) {
		return handler(ctx)
	}
}

// --- Helpers for ACP status summary ---

// FormatACPSubagentSummary returns a brief status line for the ACP registry.
func FormatACPSubagentSummary(registry *ACPRegistry) string {
	agents := registry.List("")
	if len(agents) == 0 {
		return ""
	}
	running := 0
	done := 0
	failed := 0
	for _, a := range agents {
		switch a.Status {
		case "running", "idle":
			running++
		case "done":
			done++
		case "failed", "killed":
			failed++
		}
	}
	parts := make([]string, 0, 3)
	if running > 0 {
		parts = append(parts, fmt.Sprintf("%d active", running))
	}
	if done > 0 {
		parts = append(parts, fmt.Sprintf("%d done", done))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	return fmt.Sprintf("subagents: %s", strings.Join(parts, ", "))
}

// PruneStaleACPAgents removes ACP agents that ended more than maxAgeMs ago.
func PruneStaleACPAgents(registry *ACPRegistry, maxAgeMs int64) int {
	now := time.Now().UnixMilli()
	agents := registry.List("")
	pruned := 0
	for _, a := range agents {
		if a.EndedAt > 0 && (now-a.EndedAt) > maxAgeMs {
			registry.Remove(a.ID)
			pruned++
		}
	}
	return pruned
}
