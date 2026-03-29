// Subagent lifecycle actions: kill, log, send/steer, spawn.
package subagent

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/session"
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
		return StopWithText("📜 Usage: /subagents log <id|#> [limit]")
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
		return StopWithText(fmt.Sprintf("⚠️ %s", errMsg))
	}

	if deps == nil || deps.GetHistory == nil {
		return StopWithText("⚠️ Log not available.")
	}

	messages, err := deps.GetHistory(entry.ChildSessionKey, limit)
	if err != nil {
		return StopWithText(fmt.Sprintf("⚠️ Failed to load log: %s", err))
	}

	header := fmt.Sprintf("📜 Subagent log: %s", FormatRunLabel(*entry))
	if len(messages) == 0 {
		return StopWithText(header + "\n(no messages)")
	}

	lines := []string{header}
	for _, msg := range messages {
		label := "User"
		if msg.Role == "assistant" {
			label = "Assistant"
		}
		lines = append(lines, fmt.Sprintf("%s: %s", label, msg.Content))
	}
	return StopWithText(strings.Join(lines, "\n"))
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
				return StopWithText("Usage: /subagents steer <id|#> <message>")
			}
			return StopWithText(fmt.Sprintf("Usage: %s <id|#> <message>", ctx.HandledPrefix))
		}
		return StopWithText("Usage: /subagents send <id|#> <message>")
	}

	entry, errMsg := ResolveSubagentTarget(ctx.Runs, target)
	if entry == nil {
		return StopWithText(fmt.Sprintf("⚠️ %s", errMsg))
	}

	if steerRequested && entry.EndedAt > 0 {
		return StopWithText(fmt.Sprintf("%s is already finished.", FormatRunLabel(*entry)))
	}

	if deps == nil {
		return StopWithText("⚠️ Send not available.")
	}

	if steerRequested {
		if deps.SteerRun == nil {
			return StopWithText("⚠️ Steer not available.")
		}
		result, err := deps.SteerRun(entry.RunID, message)
		if err != nil {
			return StopWithText(fmt.Sprintf("⚠️ %s", err))
		}
		switch result.Status {
		case "accepted":
			runPrefix := result.RunID
			if len(runPrefix) > 8 {
				runPrefix = runPrefix[:8]
			}
			return StopWithText(fmt.Sprintf("steered %s (run %s).", FormatRunLabel(*entry), runPrefix))
		case "done":
			if result.Text != "" {
				return StopWithText(result.Text)
			}
		case "error":
			return StopWithText(fmt.Sprintf("send failed: %s", result.Error))
		case "forbidden":
			return StopWithText(fmt.Sprintf("⚠️ %s", result.Error))
		}
		return StopWithText(fmt.Sprintf("⚠️ %s", result.Error))
	}

	if deps.SendMessage == nil {
		return StopWithText("⚠️ Send not available.")
	}
	result, err := deps.SendMessage(entry.ChildSessionKey, message)
	if err != nil {
		return StopWithText(fmt.Sprintf("⚠️ %s", err))
	}
	runPrefix := result.RunID
	if len(runPrefix) > 8 {
		runPrefix = runPrefix[:8]
	}
	switch result.Status {
	case "timeout":
		return StopWithText(fmt.Sprintf("⏳ Subagent still running (run %s).", runPrefix))
	case "error":
		return StopWithText(fmt.Sprintf("⚠️ Subagent error: %s (run %s).", result.Error, runPrefix))
	case "forbidden":
		return StopWithText(fmt.Sprintf("⚠️ %s", result.Error))
	}
	if result.ReplyText != "" {
		return StopWithText(result.ReplyText)
	}
	return StopWithText(fmt.Sprintf("✅ Sent to %s (run %s).", FormatRunLabel(*entry), runPrefix))
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
		return StopWithText("Usage: /subagents spawn <agentId> <task> [--model <model>] [--thinking <level>]")
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
		return StopWithText("Usage: /subagents spawn <agentId> <task> [--model <model>] [--thinking <level>]")
	}

	if deps == nil || deps.SpawnDirect == nil {
		return StopWithText("⚠️ Spawn not available.")
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
		return StopWithText(fmt.Sprintf("Spawn failed: %s", err))
	}
	if result.Status == "accepted" {
		runPrefix := result.RunID
		if len(runPrefix) > 8 {
			runPrefix = runPrefix[:8]
		}
		return StopWithText(
			fmt.Sprintf("Spawned subagent %s (session %s, run %s).", agentID, result.ChildSessionKey, runPrefix),
		)
	}
	errText := result.Error
	if errText == "" {
		errText = result.Status
	}
	return StopWithText(fmt.Sprintf("Spawn failed: %s", errText))
}
