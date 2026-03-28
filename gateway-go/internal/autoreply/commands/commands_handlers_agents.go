// commands_handlers_agents.go — Subagent and ACP command handlers.
package commands

import (
	"fmt"
	"strings"
)

func handleAgentsCommand(ctx CommandContext) (*CommandResult, error) {
	runs := resolveSubagentRuns(ctx)
	active, recent := BuildSubagentRunListEntries(runs, RecentWindowMinutes, 110)

	lines := []string{"active subagents:", "-----"}
	if len(active) == 0 {
		lines = append(lines, "(none)")
	} else {
		for _, e := range active {
			lines = append(lines, e.Line)
		}
	}
	lines = append(lines, "", fmt.Sprintf("recent subagents (last %dm):", RecentWindowMinutes), "-----")
	if len(recent) == 0 {
		lines = append(lines, "(none)")
	} else {
		for _, e := range recent {
			lines = append(lines, e.Line)
		}
	}
	return &CommandResult{Reply: strings.Join(lines, "\n"), SkipAgent: true}, nil
}

func handleAgentCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "ℹ️ Usage: /agent <id|#>", SkipAgent: true, IsError: true}, nil
	}

	runs := resolveSubagentRuns(ctx)
	entry, errResult := ResolveSubagentEntryForToken(runs, raw)
	if errResult != nil {
		return errResult, nil
	}
	return &CommandResult{
		Reply:     FormatSubagentInfo(entry, 0),
		SkipAgent: true,
	}, nil
}

func handleSpawnCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "Usage: /spawn <task>", SkipAgent: true, IsError: true}, nil
	}
	// Spawn is delegated to the agent runtime.
	return &CommandResult{SkipAgent: false}, nil
}

func handleFocusCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "Usage: /focus <subagent-label|session-key>", SkipAgent: true, IsError: true}, nil
	}

	runs := resolveSubagentRuns(ctx)
	entry, errResult := ResolveSubagentEntryForToken(runs, raw)
	if errResult != nil {
		return errResult, nil
	}
	return &CommandResult{
		Reply:     fmt.Sprintf("🎯 Focused on `%s` (%s).", FormatRunLabel(*entry), entry.ChildSessionKey),
		SkipAgent: true,
	}, nil
}

func handleUnfocusCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "🔓 Unfocused. Replies will go to the main session.", SkipAgent: true}, nil
}

func handleACPCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "🔗 ACP (Agent Control Protocol) status.", SkipAgent: true}, nil
	}
	return &CommandResult{Reply: fmt.Sprintf("🔗 ACP: %s", raw), SkipAgent: true}, nil
}

// resolveSubagentRuns retrieves subagent runs from deps if available.
func resolveSubagentRuns(ctx CommandContext) []*SubagentRunRecord {
	if ctx.Deps != nil && ctx.Deps.SubagentRuns != nil {
		return ctx.Deps.SubagentRuns()
	}
	return nil
}
