// Subagent agent-listing action (/agents, /subagents agents).
package subagent

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/acp"
)

// ---------------------------------------------------------------------------
// action-agents
// ---------------------------------------------------------------------------

// SubagentAgentsDeps provides dependencies for the agents action.
type SubagentAgentsDeps struct {
	ListBindings func(sessionKey string) []acp.AgentBindingEntry
}

// HandleSubagentsAgentsAction displays active agents and their bindings.
func HandleSubagentsAgentsAction(ctx *SubagentsCommandContext, deps *SubagentAgentsDeps) *SubagentCommandResult {
	sorted := SortSubagentRuns(ctx.Runs)
	lines := []string{"agents:", "-----"}

	if len(sorted) == 0 {
		lines = append(lines, "(none)")
	} else {
		idx := 1
		for _, entry := range sorted {
			// Show active runs, or runs with bindings.
			if entry.EndedAt > 0 {
				continue
			}
			bindingText := "no binding"
			if deps != nil && deps.ListBindings != nil {
				bindings := deps.ListBindings(entry.ChildSessionKey)
				for _, b := range bindings {
					if b.Status == "active" && b.Channel == ctx.Channel && b.AccountID == ctx.AccountID {
						switch b.Channel {
						case "telegram":
							bindingText = fmt.Sprintf("conversation:%s", b.ConversationID)
						default:
							bindingText = fmt.Sprintf("binding:%s", b.ConversationID)
						}
						break
					}
				}
			}
			lines = append(lines, fmt.Sprintf("%d. %s (%s)", idx, FormatRunLabel(entry), bindingText))
			idx++
		}
		if idx == 1 {
			lines = append(lines, "(none)")
		}
	}

	return StopWithText(strings.Join(lines, "\n"))
}
