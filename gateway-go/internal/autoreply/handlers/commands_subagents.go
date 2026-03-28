// commands_subagents.go — Main subagent command dispatcher.
// Mirrors src/auto-reply/reply/commands-subagents.ts (95 LOC).
//
// This file dispatches /subagents, /kill, /steer, /tell, /focus, /unfocus,
// and /agents commands to their respective action handlers.
package handlers

import (
	"strings"
)

// SubagentCommandDeps aggregates all dependencies for subagent commands.
type SubagentCommandDeps struct {
	// ListRuns returns subagent runs controlled by the given session key.
	ListRuns func(controllerKey string) []SubagentRunRecord
	// Kill deps.
	Kill *SubagentKillDeps
	// Log deps.
	Log *SubagentLogDeps
	// Send/steer deps.
	Send *SubagentSendDeps
	// Spawn deps.
	Spawn *SubagentSpawnDeps
	// Focus deps.
	Focus *SubagentFocusDeps
	// Unfocus deps.
	Unfocus *SubagentUnfocusDeps
	// Agents deps.
	Agents *SubagentAgentsDeps
}

// HandleSubagentsCommand is the main entry point for subagent commands.
// Returns nil if the message is not a subagent command.
func HandleSubagentsCommand(
	normalized string,
	sessionKey string,
	channel string,
	accountID string,
	threadID string,
	senderID string,
	isGroup bool,
	isAuthorized bool,
	deps *SubagentCommandDeps,
) *SubagentCommandResult {
	handledPrefix := ResolveHandledPrefix(normalized)
	if handledPrefix == "" {
		return nil
	}

	if !isAuthorized {
		return &SubagentCommandResult{ShouldStop: true}
	}

	rest := strings.TrimSpace(normalized[len(handledPrefix):])
	restTokens := splitTokens(rest)

	action, restTokens := ResolveSubagentsAction(handledPrefix, restTokens)
	if action == "" {
		return HandleSubagentsHelpAction()
	}

	requesterKey := sessionKey
	if requesterKey == "" {
		return subagentStopWithText("⚠️ Missing session key.")
	}

	// Resolve runs from deps.
	var runs []SubagentRunRecord
	if deps != nil && deps.ListRuns != nil {
		runs = deps.ListRuns(requesterKey)
	}

	ctx := &SubagentsCommandContext{
		HandledPrefix: handledPrefix,
		RequesterKey:  requesterKey,
		Runs:          runs,
		RestTokens:    restTokens,
		SessionKey:    sessionKey,
		Channel:       channel,
		AccountID:     accountID,
		ThreadID:      threadID,
		SenderID:      senderID,
		IsGroup:       isGroup,
	}

	switch action {
	case SubagentsActionHelp:
		return HandleSubagentsHelpAction()
	case SubagentsActionAgents:
		return HandleSubagentsAgentsAction(ctx, deps.Agents)
	case SubagentsActionFocus:
		return HandleSubagentsFocusAction(ctx, deps.Focus)
	case SubagentsActionUnfocus:
		return HandleSubagentsUnfocusAction(ctx, deps.Unfocus)
	case SubagentsActionList:
		return HandleSubagentsListAction(ctx)
	case SubagentsActionKill:
		return HandleSubagentsKillAction(ctx, deps.Kill)
	case SubagentsActionInfo:
		return HandleSubagentsInfoAction(ctx)
	case SubagentsActionLog:
		return HandleSubagentsLogAction(ctx, deps.Log)
	case SubagentsActionSend:
		return HandleSubagentsSendAction(ctx, false, deps.Send)
	case SubagentsActionSteer:
		return HandleSubagentsSendAction(ctx, true, deps.Send)
	case SubagentsActionSpawn:
		return HandleSubagentsSpawnAction(ctx, deps.Spawn)
	default:
		return HandleSubagentsHelpAction()
	}
}

// splitTokens splits a string into non-empty whitespace-delimited tokens.
func splitTokens(s string) []string {
	fields := strings.Fields(s)
	tokens := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			tokens = append(tokens, f)
		}
	}
	return tokens
}
