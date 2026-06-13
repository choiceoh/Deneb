// Subagent lifecycle actions: kill, log, send/steer, spawn.
package subagent

import (
	"fmt"
	"strconv"
	"strings"
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
		return StopWithText("📜 사용법: /subagents log <id|#> [개수]")
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
		return StopWithText("⚠️ 로그를 불러올 수 없습니다.")
	}

	messages, err := deps.GetHistory(entry.ChildSessionKey, limit)
	if err != nil {
		return StopWithText(fmt.Sprintf("⚠️ 로그 불러오기 실패: %s", err))
	}

	header := fmt.Sprintf("📜 하위 작업 로그: %s", FormatRunLabel(*entry))
	if len(messages) == 0 {
		return StopWithText(header + "\n(메시지 없음)")
	}

	lines := []string{header}
	for _, msg := range messages {
		label := "사용자"
		if msg.Role == "assistant" {
			label = "어시스턴트"
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
				return StopWithText("사용법: /subagents steer <id|#> <메시지>")
			}
			return StopWithText(fmt.Sprintf("사용법: %s <id|#> <메시지>", ctx.HandledPrefix))
		}
		return StopWithText("사용법: /subagents send <id|#> <메시지>")
	}

	entry, errMsg := ResolveSubagentTarget(ctx.Runs, target)
	if entry == nil {
		return StopWithText(fmt.Sprintf("⚠️ %s", errMsg))
	}

	if steerRequested && entry.EndedAt > 0 {
		return StopWithText(fmt.Sprintf("%s은(는) 이미 종료되었습니다.", FormatRunLabel(*entry)))
	}

	if deps == nil {
		return StopWithText("⚠️ 전송 기능을 사용할 수 없습니다.")
	}

	if steerRequested {
		if deps.SteerRun == nil {
			return StopWithText("⚠️ 조정 기능을 사용할 수 없습니다.")
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
			return StopWithText(fmt.Sprintf("%s을(를) 조정했습니다 (run %s).", FormatRunLabel(*entry), runPrefix))
		case "done":
			if result.Text != "" {
				return StopWithText(result.Text)
			}
		case "error":
			return StopWithText(fmt.Sprintf("전송 실패: %s", result.Error))
		case "forbidden":
			return StopWithText(fmt.Sprintf("⚠️ %s", result.Error))
		}
		return StopWithText(fmt.Sprintf("⚠️ %s", result.Error))
	}

	if deps.SendMessage == nil {
		return StopWithText("⚠️ 전송 기능을 사용할 수 없습니다.")
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
		return StopWithText(fmt.Sprintf("⏳ 하위 작업이 아직 실행 중입니다 (run %s).", runPrefix))
	case "error":
		return StopWithText(fmt.Sprintf("⚠️ 하위 작업 오류: %s (run %s).", result.Error, runPrefix))
	case "forbidden":
		return StopWithText(fmt.Sprintf("⚠️ %s", result.Error))
	}
	if result.ReplyText != "" {
		return StopWithText(result.ReplyText)
	}
	return StopWithText(fmt.Sprintf("✅ %s에 전달했습니다 (run %s).", FormatRunLabel(*entry), runPrefix))
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
	Task       string
	AgentID    string
	Model      string
	Thinking   string
	Mode       string // "run" or "session"
	Cleanup    string // "delete" or "keep"
	ToolPreset string // tool preset: "researcher", "implementer", "verifier"
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
		return StopWithText("사용법: /subagents spawn <agentId> <작업> [--model <모델>] [--thinking <수준>] [--tool-preset <프리셋>]")
	}

	agentID := restTokens[0]
	var taskParts []string
	var model, thinking, toolPreset string

	for i := 1; i < len(restTokens); i++ {
		switch {
		case restTokens[i] == "--model" && i+1 < len(restTokens):
			i++
			model = restTokens[i]
		case restTokens[i] == "--thinking" && i+1 < len(restTokens):
			i++
			thinking = restTokens[i]
		case restTokens[i] == "--tool-preset" && i+1 < len(restTokens):
			i++
			toolPreset = restTokens[i]
		default:
			taskParts = append(taskParts, restTokens[i])
		}
	}
	task := strings.TrimSpace(strings.Join(taskParts, " "))
	if agentID == "" || task == "" {
		return StopWithText("사용법: /subagents spawn <agentId> <작업> [--model <모델>] [--thinking <수준>] [--tool-preset <프리셋>]")
	}

	if deps == nil || deps.SpawnDirect == nil {
		return StopWithText("⚠️ 실행 기능을 사용할 수 없습니다.")
	}

	result, err := deps.SpawnDirect(
		SubagentSpawnParams{
			Task:       task,
			AgentID:    agentID,
			Model:      model,
			Thinking:   thinking,
			Mode:       "run",
			Cleanup:    "keep",
			ToolPreset: toolPreset,
		},
		SubagentSpawnContext{
			AgentSessionKey: ctx.RequesterKey,
			AgentChannel:    ctx.Channel,
			AgentAccountID:  ctx.AccountID,
			AgentThreadID:   ctx.ThreadID,
		},
	)
	if err != nil {
		return StopWithText(fmt.Sprintf("실행 실패: %s", err))
	}
	if result.Status == "accepted" {
		runPrefix := result.RunID
		if len(runPrefix) > 8 {
			runPrefix = runPrefix[:8]
		}
		return StopWithText(
			fmt.Sprintf("하위 작업 %s을(를) 시작했습니다 (세션 %s, run %s).", agentID, result.ChildSessionKey, runPrefix),
		)
	}
	errText := result.Error
	if errText == "" {
		errText = result.Status
	}
	return StopWithText(fmt.Sprintf("실행 실패: %s", errText))
}
