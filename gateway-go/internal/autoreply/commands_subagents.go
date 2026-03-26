// commands_subagents.go — Subagent command router.
// Mirrors src/auto-reply/reply/commands-subagents.ts (94 LOC).
package autoreply

import (
	"fmt"
	"strings"
)

// SubagentsAction identifies the action of a /agents command.
type SubagentsAction string

const (
	SubagentsHelp    SubagentsAction = "help"
	SubagentsAgents  SubagentsAction = "agents"
	SubagentsFocus   SubagentsAction = "focus"
	SubagentsUnfocus SubagentsAction = "unfocus"
	SubagentsList    SubagentsAction = "list"
	SubagentsKill    SubagentsAction = "kill"
	SubagentsInfo    SubagentsAction = "info"
	SubagentsLog     SubagentsAction = "log"
	SubagentsSend    SubagentsAction = "send"
	SubagentsSteer   SubagentsAction = "steer"
	SubagentsSpawn   SubagentsAction = "spawn"
)

// SubagentRun describes an active subagent run.
type SubagentRun struct {
	ID         string `json:"id"`
	AgentID    string `json:"agentId"`
	SessionKey string `json:"sessionKey"`
	Status     string `json:"status"` // "running", "done", "failed", "killed"
	StartedAt  int64  `json:"startedAt"`
}

// SubagentsCommandContext holds the context for subagent command handling.
type SubagentsCommandContext struct {
	RequesterKey string
	Runs         []SubagentRun
	RestTokens   []string
	HandledPrefix string
}

var subagentsCommandPrefixes = []string{"/agents", "/subagents", "/agent", "/subagent"}

// ResolveSubagentsPrefix returns the handled command prefix if the text starts
// with a known subagent command, or empty string if not.
func ResolveSubagentsPrefix(normalized string) string {
	lower := strings.ToLower(normalized)
	for _, prefix := range subagentsCommandPrefixes {
		if lower == prefix || strings.HasPrefix(lower, prefix+" ") || strings.HasPrefix(lower, prefix+":") {
			return prefix
		}
	}
	return ""
}

// ResolveSubagentsAction determines the action from the command tokens.
func ResolveSubagentsAction(prefix string, tokens []string) SubagentsAction {
	if len(tokens) == 0 {
		// Default action for short prefixes.
		if prefix == "/agents" || prefix == "/subagents" {
			return SubagentsList
		}
		return SubagentsHelp
	}

	switch strings.ToLower(tokens[0]) {
	case "help", "?":
		return SubagentsHelp
	case "agents", "available":
		return SubagentsAgents
	case "focus", "switch":
		return SubagentsFocus
	case "unfocus", "unswitch", "detach":
		return SubagentsUnfocus
	case "list", "ls", "runs":
		return SubagentsList
	case "kill", "stop", "abort":
		return SubagentsKill
	case "info", "status":
		return SubagentsInfo
	case "log", "logs":
		return SubagentsLog
	case "send", "msg", "message":
		return SubagentsSend
	case "steer":
		return SubagentsSteer
	case "spawn", "start", "run":
		return SubagentsSpawn
	default:
		return SubagentsHelp
	}
}

// SubagentsCommandResult holds the result of a /agents command.
type SubagentsCommandResult struct {
	ShouldContinue bool
	ReplyText      string
}

// HandleSubagentsHelp returns the help text for subagent commands.
func HandleSubagentsHelp() SubagentsCommandResult {
	help := `Available subagent commands:
/agents list — List active subagent runs
/agents agents — List available subagents
/agents spawn <agent> [message] — Spawn a new subagent
/agents send <id> <message> — Send a message to a subagent
/agents steer <id> <message> — Steer a subagent
/agents focus <id> — Focus on a subagent
/agents unfocus — Unfocus from current subagent
/agents kill <id> — Terminate a subagent run
/agents info <id> — Show subagent run info
/agents log <id> — View subagent logs`
	return SubagentsCommandResult{ReplyText: help}
}

// HandleSubagentsList lists active subagent runs.
func HandleSubagentsList(ctx SubagentsCommandContext) SubagentsCommandResult {
	if len(ctx.Runs) == 0 {
		return SubagentsCommandResult{ReplyText: "No active subagent runs."}
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Active subagent runs (%d):", len(ctx.Runs)))
	for _, run := range ctx.Runs {
		lines = append(lines, fmt.Sprintf("- %s [%s] agent=%s", run.ID, run.Status, run.AgentID))
	}
	return SubagentsCommandResult{ReplyText: strings.Join(lines, "\n")}
}

// HandleSubagentsCommand routes a subagent command to the appropriate handler.
func HandleSubagentsCommand(normalized string, ctx SubagentsCommandContext) *SubagentsCommandResult {
	prefix := ResolveSubagentsPrefix(normalized)
	if prefix == "" {
		return nil
	}

	rest := strings.TrimSpace(normalized[len(prefix):])
	tokens := strings.Fields(rest)
	action := ResolveSubagentsAction(prefix, tokens)

	if len(tokens) > 0 {
		ctx.RestTokens = tokens[1:] // Skip action token.
	}
	ctx.HandledPrefix = prefix

	switch action {
	case SubagentsHelp:
		result := HandleSubagentsHelp()
		return &result
	case SubagentsList:
		result := HandleSubagentsList(ctx)
		return &result
	case SubagentsAgents:
		result := SubagentsCommandResult{ReplyText: "Available subagents: (use /agents spawn <agent> to start one)"}
		return &result
	case SubagentsKill:
		if len(ctx.RestTokens) == 0 {
			result := SubagentsCommandResult{ReplyText: "Usage: /agents kill <run-id>"}
			return &result
		}
		result := SubagentsCommandResult{ReplyText: fmt.Sprintf("Kill requested for run %s.", ctx.RestTokens[0])}
		return &result
	case SubagentsInfo:
		if len(ctx.RestTokens) == 0 {
			result := SubagentsCommandResult{ReplyText: "Usage: /agents info <run-id>"}
			return &result
		}
		result := SubagentsCommandResult{ReplyText: fmt.Sprintf("Info for run %s.", ctx.RestTokens[0])}
		return &result
	case SubagentsSpawn:
		if len(ctx.RestTokens) == 0 {
			result := SubagentsCommandResult{ReplyText: "Usage: /agents spawn <agent-id> [message]"}
			return &result
		}
		result := SubagentsCommandResult{ReplyText: fmt.Sprintf("Spawning subagent %s...", ctx.RestTokens[0])}
		return &result
	default:
		result := HandleSubagentsHelp()
		return &result
	}
}
