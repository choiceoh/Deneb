// commands_handlers_agents.go — /agents command handler.
package handlers

import (
	"fmt"
	"strings"

	subagentpkg "github.com/choiceoh/deneb/gateway-go/internal/autoreply/subagent"
)

func handleAgentsCommand(ctx CommandContext) (*CommandResult, error) {
	var runs []subagentpkg.SubagentRunRecord
	if ctx.Deps != nil && ctx.Deps.SubagentRuns != nil {
		runs = ctx.Deps.SubagentRuns()
	}

	result := subagentpkg.BuildSubagentList(runs, subagentpkg.RecentWindowMinutes, 110)

	lines := []string{"active subagents:", "-----"}
	if len(result.Active) == 0 {
		lines = append(lines, "(none)")
	} else {
		for _, e := range result.Active {
			lines = append(lines, e.Line)
		}
	}
	lines = append(lines, "", fmt.Sprintf("recent subagents (last %dm):", subagentpkg.RecentWindowMinutes), "-----")
	if len(result.Recent) == 0 {
		lines = append(lines, "(none)")
	} else {
		for _, e := range result.Recent {
			lines = append(lines, e.Line)
		}
	}

	return &CommandResult{Reply: strings.Join(lines, "\n"), SkipAgent: true}, nil
}
