// commands_subagents_acp.go — Wires SubagentCommandDeps to the ACPRegistry.
// Bridges the abstract SubagentCommandDeps interface to the concrete ACP
// sub-agent registry so that /subagents commands work against real state.
package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/acp"
	"github.com/choiceoh/deneb/gateway-go/internal/shortid"
)

// ACPCommandDepsConfig holds optional infra dependencies for wiring ACP
// subagent commands beyond the basic registry operations.
type ACPCommandDepsConfig struct {
	// Infra provides session management and spawn capabilities.
	Infra *acp.SubagentInfraDeps
	// SessionSendFn sends a message to a session, triggering an agent run.
	SessionSendFn func(sessionKey, message string) error
	// TranscriptLoader loads transcript history for a session.
	TranscriptLoader func(sessionKey string, limit int) ([]ChatLogMessage, error)
	// SessionBindings provides focus/unfocus/agents capabilities.
	SessionBindings *acp.SessionBindingService
}

// NewSubagentCommandDepsFromACP creates a SubagentCommandDeps backed by
// the given ACPRegistry. Optional config provides full infrastructure wiring.
func NewSubagentCommandDepsFromACP(registry *acp.ACPRegistry, cfg ...ACPCommandDepsConfig) *SubagentCommandDeps {
	var config ACPCommandDepsConfig
	if len(cfg) > 0 {
		config = cfg[0]
	}

	deps := &SubagentCommandDeps{
		ListRuns: func(controllerKey string) []SubagentRunRecord {
			return acpAgentsToRunRecords(registry.List(""), controllerKey)
		},
		Kill: &SubagentKillDeps{
			KillRun: func(runID string) (bool, error) {
				if config.Infra != nil {
					return true, config.Infra.KillSubagent(runID)
				}
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
	}

	// Wire Send/Steer if SessionSendFn is available.
	if config.SessionSendFn != nil {
		deps.Send = &SubagentSendDeps{
			SendMessage: func(sessionKey, message string) (*SubagentSendResult, error) {
				if err := config.SessionSendFn(sessionKey, message); err != nil {
					return &SubagentSendResult{Status: "error", Error: err.Error()}, nil
				}
				return &SubagentSendResult{Status: "ok", RunID: shortid.New("send")}, nil
			},
			SteerRun: func(runID, message string) (*SubagentSteerResult, error) {
				// Find the agent by run ID and send to its session.
				agent := registry.Get(runID)
				if agent == nil {
					return &SubagentSteerResult{Status: "error", Error: "agent not found"}, nil
				}
				if err := config.SessionSendFn(agent.SessionKey, message); err != nil {
					return &SubagentSteerResult{Status: "error", Error: err.Error()}, nil
				}
				return &SubagentSteerResult{Status: "accepted", RunID: runID}, nil
			},
		}
	}

	// Wire Spawn if Infra is available.
	if config.Infra != nil {
		deps.Spawn = &SubagentSpawnDeps{
			SpawnDirect: func(params SubagentSpawnParams, spawnCtx SubagentSpawnContext) (*SubagentSpawnResult, error) {
				result := config.Infra.SpawnSubagent(context.Background(), acp.SpawnSubagentParams{
					ParentSessionKey: spawnCtx.AgentSessionKey,
					Role:             params.Task,
					Model:            params.Model,
					InitialMessage:   params.Task,
				})
				if result.Error != nil {
					return &SubagentSpawnResult{Status: "error", Error: result.Error.Error()}, nil
				}
				// Send initial task message to spawned session.
				if config.SessionSendFn != nil {
					_ = config.SessionSendFn(result.SessionKey, params.Task)
				}
				return &SubagentSpawnResult{
					Status:          "accepted",
					ChildSessionKey: result.SessionKey,
					RunID:           result.AgentID,
				}, nil
			},
		}
	}

	// Wire Focus/Unfocus/Agents if SessionBindings is available.
	if config.SessionBindings != nil {
		sbs := config.SessionBindings

		deps.Focus = &SubagentFocusDeps{
			BindSession: func(params acp.SessionBindParams) (*acp.SessionBindResult, error) {
				result := sbs.Bind(params)
				return result, nil
			},
		}

		deps.Unfocus = &SubagentUnfocusDeps{
			ResolveBinding: func(channel, accountID, conversationID string) *acp.SessionBindingEntry {
				return sbs.Resolve(channel, accountID, conversationID)
			},
			Unbind: func(bindingID string) error {
				return sbs.Unbind(bindingID)
			},
		}

		deps.Agents = &SubagentAgentsDeps{
			ListBindings: func(sessionKey string) []acp.AgentBindingEntry {
				return sbs.ListForSession(sessionKey)
			},
		}
	}

	// Wire Log/GetHistory.
	if config.TranscriptLoader != nil {
		deps.Log = &SubagentLogDeps{
			GetHistory: config.TranscriptLoader,
		}
	} else {
		deps.Log = &SubagentLogDeps{
			GetHistory: func(sessionKey string, limit int) ([]ChatLogMessage, error) {
				return nil, fmt.Errorf("transcript loading not available")
			},
		}
	}

	return deps
}

// acpAgentsToRunRecords converts ACPAgent entries to SubagentRunRecord.
// Filters to agents parented by controllerKey (or all if controllerKey is empty).
func acpAgentsToRunRecords(agents []acp.ACPAgent, controllerKey string) []SubagentRunRecord {
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
func acpAgentToRunRecord(a acp.ACPAgent) SubagentRunRecord {
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
func resolveACPLabel(a acp.ACPAgent) string {
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
	registry *acp.ACPRegistry
	deps     *SubagentCommandDeps
}

// NewACPSubagentCommandHandler creates a handler wired to the given registry.
func NewACPSubagentCommandHandler(registry *acp.ACPRegistry, cfg ...ACPCommandDepsConfig) *ACPSubagentCommandHandler {
	deps := NewSubagentCommandDepsFromACP(registry, cfg...)
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
func RegisterACPSubagentRPC(router *CommandRouter, registry *acp.ACPRegistry) {
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
func FormatACPSubagentSummary(registry *acp.ACPRegistry) string {
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
func PruneStaleACPAgents(registry *acp.ACPRegistry, maxAgeMs int64) int {
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
