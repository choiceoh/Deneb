// commands_subagents_acp.go — CommandRouter wiring for ACP subagent commands.
// Type definitions and registry wiring have moved to internal/autoreply/subagent.
// This file retains only the parts that depend on CommandRouter/CommandResult.
package handlers

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/acp"
	subagentpkg "github.com/choiceoh/deneb/gateway-go/internal/autoreply/subagent"
)

// Re-export ACP wiring types from subagent package.
type (
	ACPCommandDepsConfig      = subagentpkg.ACPCommandDepsConfig
	ACPSubagentCommandHandler = subagentpkg.ACPSubagentCommandHandler
)

// Re-export ACP wiring functions from subagent package.

func NewSubagentCommandDepsFromACP(registry *acp.ACPRegistry, cfg ...ACPCommandDepsConfig) *SubagentCommandDeps {
	return subagentpkg.NewSubagentCommandDepsFromACP(registry, cfg...)
}

func NewACPSubagentCommandHandler(registry *acp.ACPRegistry, cfg ...ACPCommandDepsConfig) *ACPSubagentCommandHandler {
	return subagentpkg.NewACPSubagentCommandHandler(registry, cfg...)
}

func FormatACPSubagentSummary(registry *acp.ACPRegistry) string {
	return subagentpkg.FormatACPSubagentSummary(registry)
}

func PruneStaleACPAgents(registry *acp.ACPRegistry, maxAgeMs int64) int {
	return subagentpkg.PruneStaleACPAgents(registry, maxAgeMs)
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
