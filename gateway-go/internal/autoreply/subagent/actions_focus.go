// Subagent focus and agent management actions.
package subagent

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/acp"
)

// ---------------------------------------------------------------------------
// action-focus
// ---------------------------------------------------------------------------

// SubagentFocusDeps provides dependencies for the focus action.
type SubagentFocusDeps struct {
	BindSession func(params acp.SessionBindParams) (*acp.SessionBindResult, error)
}

// HandleSubagentsFocusAction binds a conversation to a subagent session.
func HandleSubagentsFocusAction(ctx *SubagentsCommandContext, deps *SubagentFocusDeps) *SubagentCommandResult {
	channel := ctx.Channel
	if channel != "telegram" {
		return StopWithText("⚠️ /focus is only available on Telegram.")
	}

	token := strings.TrimSpace(strings.Join(ctx.RestTokens, " "))
	if token == "" {
		return StopWithText("Usage: /focus <subagent-label|session-key|session-id|session-label>")
	}

	// Resolve target from runs.
	entry, _ := ResolveSubagentTarget(ctx.Runs, token)
	if entry == nil {
		return StopWithText(fmt.Sprintf("⚠️ Unable to resolve focus target: %s", token))
	}

	conversationID := ctx.ThreadID
	if conversationID == "" {
		if channel == "telegram" {
			return StopWithText("⚠️ /focus on Telegram requires a topic context in groups, or a direct-message conversation.")
		}
		return StopWithText("⚠️ Could not resolve a conversation for /focus.")
	}

	if deps == nil || deps.BindSession == nil {
		return StopWithText("⚠️ Focus not available.")
	}

	label := FormatRunLabel(*entry)
	result, err := deps.BindSession(acp.SessionBindParams{
		TargetSessionKey: entry.ChildSessionKey,
		TargetKind:       "subagent",
		Channel:          channel,
		AccountID:        ctx.AccountID,
		ConversationID:   conversationID,
		Placement:        "current",
		Label:            label,
		BoundBy:          ctx.SenderID,
	})
	if err != nil {
		return StopWithText("⚠️ Failed to bind this conversation to the target session.")
	}

	return StopWithText(
		fmt.Sprintf("✅ bound this conversation to %s (subagent).", result.TargetKey),
	)
}

// ---------------------------------------------------------------------------
// action-unfocus
// ---------------------------------------------------------------------------

// SubagentUnfocusDeps provides dependencies for the unfocus action.
type SubagentUnfocusDeps struct {
	ResolveBinding func(channel, accountID, conversationID string) *acp.SessionBindingEntry
	Unbind         func(bindingID string) error
}

// HandleSubagentsUnfocusAction unbinds a conversation from its session.
func HandleSubagentsUnfocusAction(ctx *SubagentsCommandContext, deps *SubagentUnfocusDeps) *SubagentCommandResult {
	channel := ctx.Channel
	if channel != "telegram" {
		return StopWithText("⚠️ /unfocus is only available on Telegram.")
	}

	conversationID := ctx.ThreadID
	if conversationID == "" {
		return StopWithText("⚠️ /unfocus on Telegram requires a topic context in groups, or a direct-message conversation.")
	}

	if deps == nil || deps.ResolveBinding == nil {
		return StopWithText("⚠️ Unfocus not available.")
	}

	binding := deps.ResolveBinding(channel, ctx.AccountID, conversationID)
	if binding == nil {
		return StopWithText("ℹ️ This conversation is not currently focused.")
	}

	// Check bound-by permission.
	if binding.BoundBy != "" && binding.BoundBy != "system" && ctx.SenderID != "" && ctx.SenderID != binding.BoundBy {
		return StopWithText(fmt.Sprintf("⚠️ Only %s can unfocus this conversation.", binding.BoundBy))
	}

	if deps.Unbind == nil {
		return StopWithText("⚠️ Unfocus not available.")
	}
	if err := deps.Unbind(binding.BindingID); err != nil {
		return StopWithText(fmt.Sprintf("⚠️ Failed to unfocus: %s", err))
	}
	return StopWithText("✅ Conversation unfocused.")
}

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
						if b.Channel == "telegram" {
							bindingText = fmt.Sprintf("conversation:%s", b.ConversationID)
						} else if b.Channel == "telegram" {
							bindingText = fmt.Sprintf("conversation:%s", b.ConversationID)
						} else {
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
