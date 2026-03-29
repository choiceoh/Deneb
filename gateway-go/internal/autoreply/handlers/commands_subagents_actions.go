// commands_subagents_actions.go — Remaining subagent command action handlers.
package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/acp"
)

// ---------------------------------------------------------------------------
// action-log
// ---------------------------------------------------------------------------

// SubagentLogDeps provides dependencies for the log action.
type SubagentLogDeps struct {
	GetHistory func(sessionKey string, limit int) ([]ChatLogMessage, error)
}

// ChatLogMessage represents a message in the chat log.
type ChatLogMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// HandleSubagentsLogAction displays the message history of a subagent.
func HandleSubagentsLogAction(ctx *SubagentsCommandContext, deps *SubagentLogDeps) *SubagentCommandResult {
	target := ""
	if len(ctx.RestTokens) > 0 {
		target = ctx.RestTokens[0]
	}
	if target == "" {
		return subagentStopWithText("📜 Usage: /subagents log <id|#> [limit]")
	}

	// Parse optional limit.
	limit := 20
	for _, token := range ctx.RestTokens[1:] {
		if n, err := strconv.Atoi(token); err == nil && n > 0 {
			limit = n
			if limit > 200 {
				limit = 200
			}
			break
		}
	}

	entry, errMsg := ResolveSubagentTarget(ctx.Runs, target)
	if entry == nil {
		return subagentStopWithText(fmt.Sprintf("⚠️ %s", errMsg))
	}

	if deps == nil || deps.GetHistory == nil {
		return subagentStopWithText("⚠️ Log not available.")
	}

	messages, err := deps.GetHistory(entry.ChildSessionKey, limit)
	if err != nil {
		return subagentStopWithText(fmt.Sprintf("⚠️ Failed to load log: %s", err))
	}

	header := fmt.Sprintf("📜 Subagent log: %s", FormatRunLabel(*entry))
	if len(messages) == 0 {
		return subagentStopWithText(header + "\n(no messages)")
	}

	lines := []string{header}
	for _, msg := range messages {
		label := "User"
		if msg.Role == "assistant" {
			label = "Assistant"
		}
		lines = append(lines, fmt.Sprintf("%s: %s", label, msg.Content))
	}
	return subagentStopWithText(strings.Join(lines, "\n"))
}

// ---------------------------------------------------------------------------
// action-send / action-steer
// ---------------------------------------------------------------------------

// SubagentSendDeps provides dependencies for the send/steer action.
type SubagentSendDeps struct {
	SendMessage func(sessionKey string, message string) (*SubagentSendResult, error)
	SteerRun    func(runID string, message string) (*SubagentSteerResult, error)
}

// SubagentSendResult holds the result of sending a message to a subagent.
type SubagentSendResult struct {
	Status    string // "ok", "timeout", "error", "forbidden"
	RunID     string
	ReplyText string
	Error     string
}

// SubagentSteerResult holds the result of steering a subagent.
type SubagentSteerResult struct {
	Status string // "accepted", "done", "error", "forbidden"
	RunID  string
	Text   string
	Error  string
}

// HandleSubagentsSendAction sends a message to or steers a subagent.
func HandleSubagentsSendAction(ctx *SubagentsCommandContext, steerRequested bool, deps *SubagentSendDeps) *SubagentCommandResult {
	target := ""
	if len(ctx.RestTokens) > 0 {
		target = ctx.RestTokens[0]
	}
	message := ""
	if len(ctx.RestTokens) > 1 {
		message = strings.Join(ctx.RestTokens[1:], " ")
	}
	message = strings.TrimSpace(message)

	if target == "" || message == "" {
		if steerRequested {
			if ctx.HandledPrefix == SubagentsCmdPrefix {
				return subagentStopWithText("Usage: /subagents steer <id|#> <message>")
			}
			return subagentStopWithText(fmt.Sprintf("Usage: %s <id|#> <message>", ctx.HandledPrefix))
		}
		return subagentStopWithText("Usage: /subagents send <id|#> <message>")
	}

	entry, errMsg := ResolveSubagentTarget(ctx.Runs, target)
	if entry == nil {
		return subagentStopWithText(fmt.Sprintf("⚠️ %s", errMsg))
	}

	if steerRequested && entry.EndedAt > 0 {
		return subagentStopWithText(fmt.Sprintf("%s is already finished.", FormatRunLabel(*entry)))
	}

	if deps == nil {
		return subagentStopWithText("⚠️ Send not available.")
	}

	if steerRequested {
		if deps.SteerRun == nil {
			return subagentStopWithText("⚠️ Steer not available.")
		}
		result, err := deps.SteerRun(entry.RunID, message)
		if err != nil {
			return subagentStopWithText(fmt.Sprintf("⚠️ %s", err))
		}
		switch result.Status {
		case "accepted":
			runPrefix := result.RunID
			if len(runPrefix) > 8 {
				runPrefix = runPrefix[:8]
			}
			return subagentStopWithText(fmt.Sprintf("steered %s (run %s).", FormatRunLabel(*entry), runPrefix))
		case "done":
			if result.Text != "" {
				return subagentStopWithText(result.Text)
			}
		case "error":
			return subagentStopWithText(fmt.Sprintf("send failed: %s", result.Error))
		case "forbidden":
			return subagentStopWithText(fmt.Sprintf("⚠️ %s", result.Error))
		}
		return subagentStopWithText(fmt.Sprintf("⚠️ %s", result.Error))
	}

	if deps.SendMessage == nil {
		return subagentStopWithText("⚠️ Send not available.")
	}
	result, err := deps.SendMessage(entry.ChildSessionKey, message)
	if err != nil {
		return subagentStopWithText(fmt.Sprintf("⚠️ %s", err))
	}
	runPrefix := result.RunID
	if len(runPrefix) > 8 {
		runPrefix = runPrefix[:8]
	}
	switch result.Status {
	case "timeout":
		return subagentStopWithText(fmt.Sprintf("⏳ Subagent still running (run %s).", runPrefix))
	case "error":
		return subagentStopWithText(fmt.Sprintf("⚠️ Subagent error: %s (run %s).", result.Error, runPrefix))
	case "forbidden":
		return subagentStopWithText(fmt.Sprintf("⚠️ %s", result.Error))
	}
	if result.ReplyText != "" {
		return subagentStopWithText(result.ReplyText)
	}
	return subagentStopWithText(fmt.Sprintf("✅ Sent to %s (run %s).", FormatRunLabel(*entry), runPrefix))
}

// ---------------------------------------------------------------------------
// action-spawn
// ---------------------------------------------------------------------------

// SubagentSpawnDeps provides dependencies for the spawn action.
type SubagentSpawnDeps struct {
	SpawnDirect func(params SubagentSpawnParams, context SubagentSpawnContext) (*SubagentSpawnResult, error)
}

// SubagentSpawnParams holds params for spawning a subagent.
type SubagentSpawnParams struct {
	Task     string
	AgentID  string
	Model    string
	Thinking string
	Mode     string // "run" or "session"
	Cleanup  string // "delete" or "keep"
}

// SubagentSpawnContext holds routing context for the spawned subagent.
type SubagentSpawnContext struct {
	AgentSessionKey string
	AgentChannel    string
	AgentAccountID  string
	AgentTo         string
	AgentThreadID   string
	AgentGroupID    string
}

// SubagentSpawnResult holds the result of spawning a subagent.
type SubagentSpawnResult struct {
	Status          string // "accepted", "forbidden", "error"
	ChildSessionKey string
	RunID           string
	Error           string
}

// HandleSubagentsSpawnAction spawns a new subagent.
func HandleSubagentsSpawnAction(ctx *SubagentsCommandContext, deps *SubagentSpawnDeps) *SubagentCommandResult {
	restTokens := ctx.RestTokens
	if len(restTokens) == 0 {
		return subagentStopWithText("Usage: /subagents spawn <agentId> <task> [--model <model>] [--thinking <level>]")
	}

	agentID := restTokens[0]
	var taskParts []string
	var model, thinking string

	for i := 1; i < len(restTokens); i++ {
		if restTokens[i] == "--model" && i+1 < len(restTokens) {
			i++
			model = restTokens[i]
		} else if restTokens[i] == "--thinking" && i+1 < len(restTokens) {
			i++
			thinking = restTokens[i]
		} else {
			taskParts = append(taskParts, restTokens[i])
		}
	}
	task := strings.TrimSpace(strings.Join(taskParts, " "))
	if agentID == "" || task == "" {
		return subagentStopWithText("Usage: /subagents spawn <agentId> <task> [--model <model>] [--thinking <level>]")
	}

	if deps == nil || deps.SpawnDirect == nil {
		return subagentStopWithText("⚠️ Spawn not available.")
	}

	result, err := deps.SpawnDirect(
		SubagentSpawnParams{
			Task:     task,
			AgentID:  agentID,
			Model:    model,
			Thinking: thinking,
			Mode:     "run",
			Cleanup:  "keep",
		},
		SubagentSpawnContext{
			AgentSessionKey: ctx.RequesterKey,
			AgentChannel:    ctx.Channel,
			AgentAccountID:  ctx.AccountID,
			AgentThreadID:   ctx.ThreadID,
		},
	)
	if err != nil {
		return subagentStopWithText(fmt.Sprintf("Spawn failed: %s", err))
	}
	if result.Status == "accepted" {
		runPrefix := result.RunID
		if len(runPrefix) > 8 {
			runPrefix = runPrefix[:8]
		}
		return subagentStopWithText(
			fmt.Sprintf("Spawned subagent %s (session %s, run %s).", agentID, result.ChildSessionKey, runPrefix),
		)
	}
	errText := result.Error
	if errText == "" {
		errText = result.Status
	}
	return subagentStopWithText(fmt.Sprintf("Spawn failed: %s", errText))
}

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
	if channel != "discord" && channel != "telegram" {
		return subagentStopWithText("⚠️ /focus is only available on Discord and Telegram.")
	}

	token := strings.TrimSpace(strings.Join(ctx.RestTokens, " "))
	if token == "" {
		return subagentStopWithText("Usage: /focus <subagent-label|session-key|session-id|session-label>")
	}

	// Resolve target from runs.
	entry, _ := ResolveSubagentTarget(ctx.Runs, token)
	if entry == nil {
		return subagentStopWithText(fmt.Sprintf("⚠️ Unable to resolve focus target: %s", token))
	}

	conversationID := ctx.ThreadID
	if conversationID == "" {
		if channel == "telegram" {
			return subagentStopWithText("⚠️ /focus on Telegram requires a topic context in groups, or a direct-message conversation.")
		}
		return subagentStopWithText("⚠️ Could not resolve a conversation for /focus.")
	}

	if deps == nil || deps.BindSession == nil {
		return subagentStopWithText("⚠️ Focus not available.")
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
		labelNoun := "conversation"
		if channel == "discord" {
			labelNoun = "thread"
		}
		return subagentStopWithText(fmt.Sprintf("⚠️ Failed to bind this %s to the target session.", labelNoun))
	}

	return subagentStopWithText(
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
	if channel != "discord" && channel != "telegram" {
		return subagentStopWithText("⚠️ /unfocus is only available on Discord and Telegram.")
	}

	conversationID := ctx.ThreadID
	if conversationID == "" {
		if channel == "discord" {
			return subagentStopWithText("⚠️ /unfocus must be run inside a Discord thread.")
		}
		return subagentStopWithText("⚠️ /unfocus on Telegram requires a topic context in groups, or a direct-message conversation.")
	}

	if deps == nil || deps.ResolveBinding == nil {
		return subagentStopWithText("⚠️ Unfocus not available.")
	}

	binding := deps.ResolveBinding(channel, ctx.AccountID, conversationID)
	if binding == nil {
		noun := "conversation"
		if channel == "discord" {
			noun = "thread"
		}
		return subagentStopWithText(fmt.Sprintf("ℹ️ This %s is not currently focused.", noun))
	}

	// Check bound-by permission.
	if binding.BoundBy != "" && binding.BoundBy != "system" && ctx.SenderID != "" && ctx.SenderID != binding.BoundBy {
		noun := "conversation"
		if channel == "discord" {
			noun = "thread"
		}
		return subagentStopWithText(fmt.Sprintf("⚠️ Only %s can unfocus this %s.", binding.BoundBy, noun))
	}

	if deps.Unbind == nil {
		return subagentStopWithText("⚠️ Unfocus not available.")
	}
	if err := deps.Unbind(binding.BindingID); err != nil {
		return subagentStopWithText(fmt.Sprintf("⚠️ Failed to unfocus: %s", err))
	}
	if channel == "discord" {
		return subagentStopWithText("✅ Thread unfocused.")
	}
	return subagentStopWithText("✅ Conversation unfocused.")
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
						if b.Channel == "discord" {
							bindingText = fmt.Sprintf("thread:%s", b.ConversationID)
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

	return subagentStopWithText(strings.Join(lines, "\n"))
}
