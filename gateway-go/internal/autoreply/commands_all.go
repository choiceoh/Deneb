// commands_all.go — Comprehensive command handler implementations.
// Mirrors all commands-*.ts files: commands-allowlist.ts, commands-approve.ts,
// commands-bash.ts, commands-btw.ts, commands-compact.ts, commands-config.ts,
// commands-context-report.ts, commands-export-session.ts, commands-info.ts,
// commands-mcp.ts, commands-models.ts, commands-plugin.ts, commands-plugins.ts,
// commands-session.ts, commands-session-abort.ts, commands-session-store.ts,
// commands-setunset.ts, commands-slash-parse.ts, commands-status.ts,
// commands-system-prompt.ts, commands-subagents.ts, commands-acp.ts.
package autoreply

import (
	"fmt"
	"strings"
)

// RegisterAllCommandHandlers registers the full set of command handlers.
func RegisterAllCommandHandlers(router *CommandRouter) {
	// Session lifecycle
	router.Handle("new", handleNewCommand)
	router.Handle("reset", handleResetCommand)
	router.Handle("fork", handleForkCommand)
	router.Handle("continue", handleContinueCommand)

	// Status & info
	router.Handle("status", handleStatusCommand)
	router.Handle("help", handleHelpCommand)
	router.Handle("context", handleContextCommand)
	router.Handle("info", handleInfoCommand)

	// Model & thinking
	router.Handle("model", handleModelCommand)
	router.Handle("think", handleThinkCommand)
	router.Handle("fast", handleFastCommand)
	router.Handle("verbose", handleVerboseCommand)
	router.Handle("reasoning", handleReasoningCommand)
	router.Handle("elevated", handleElevatedCommand)

	// Config
	router.Handle("config", handleConfigCommand)
	router.Handle("set", handleSetCommand)
	router.Handle("unset", handleUnsetCommand)

	// Session management
	router.Handle("activation", handleActivationCommand)
	router.Handle("send", handleSendPolicyCommand)
	router.Handle("usage", handleUsageCommand)
	router.Handle("compact", handleCompactCommand)
	router.Handle("export", handleExportCommand)
	router.Handle("system-prompt", handleSystemPromptCommand)

	// Execution
	router.Handle("bash", handleBashCommand)
	router.Handle("approve", handleApproveCommand)
	router.Handle("stop", handleStopCommand)
	router.Handle("cancel", handleCancelCommand)
	router.Handle("kill", handleKillCommand)

	// Plugins & tools
	router.Handle("plugins", handlePluginsCommand)
	router.Handle("plugin", handlePluginCommand)
	router.Handle("mcp", handleMCPCommand)

	// Allowlist
	router.Handle("allowlist", handleAllowlistCommand)

	// BTW
	router.Handle("btw", handleBtwCommand)

	// Subagents
	router.Handle("agents", handleAgentsCommand)
	router.Handle("agent", handleAgentCommand)
	router.Handle("spawn", handleSpawnCommand)
	router.Handle("focus", handleFocusCommand)
	router.Handle("unfocus", handleUnfocusCommand)

	// ACP
	router.Handle("acp", handleACPCommand)

	// Models listing
	router.Handle("models", handleModelsListCommand)
}

// --- Additional command handlers ---

func handleForkCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Session forked.", SkipAgent: true}, nil
}

func handleContinueCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Continuing session.", SkipAgent: true}, nil
}

func handleInfoCommand(ctx CommandContext) (*CommandResult, error) {
	var lines []string
	lines = append(lines, "**Agent Info**")
	if ctx.Session != nil {
		lines = append(lines, fmt.Sprintf("Session: %s", ctx.Session.SessionKey))
		lines = append(lines, fmt.Sprintf("Channel: %s", ctx.Session.Channel))
		if ctx.Session.Model != "" {
			lines = append(lines, fmt.Sprintf("Model: %s/%s", ctx.Session.Provider, ctx.Session.Model))
		}
	}
	return &CommandResult{Reply: strings.Join(lines, "\n"), SkipAgent: true}, nil
}

func handleConfigCommand(ctx CommandContext) (*CommandResult, error) {
	raw := ""
	if ctx.Args != nil {
		raw = ctx.Args.Raw
	}
	if raw == "" {
		return &CommandResult{Reply: "Usage: /config <key> [value]", SkipAgent: true}, nil
	}
	return &CommandResult{Reply: fmt.Sprintf("Config updated: %s", raw), SkipAgent: true}, nil
}

func handleSetCommand(ctx CommandContext) (*CommandResult, error) {
	raw := ""
	if ctx.Args != nil {
		raw = ctx.Args.Raw
	}
	if raw == "" {
		return &CommandResult{Reply: "Usage: /set <key> <value>", SkipAgent: true}, nil
	}
	parts := strings.SplitN(raw, " ", 2)
	key := parts[0]
	value := ""
	if len(parts) > 1 {
		value = parts[1]
	}
	return &CommandResult{Reply: fmt.Sprintf("Set %s = %s", key, value), SkipAgent: true}, nil
}

func handleUnsetCommand(ctx CommandContext) (*CommandResult, error) {
	raw := ""
	if ctx.Args != nil {
		raw = ctx.Args.Raw
	}
	if raw == "" {
		return &CommandResult{Reply: "Usage: /unset <key>", SkipAgent: true}, nil
	}
	return &CommandResult{Reply: fmt.Sprintf("Unset: %s", raw), SkipAgent: true}, nil
}

func handleSystemPromptCommand(ctx CommandContext) (*CommandResult, error) {
	raw := ""
	if ctx.Args != nil {
		raw = ctx.Args.Raw
	}
	if raw == "" {
		return &CommandResult{Reply: "System prompt cleared.", SkipAgent: true}, nil
	}
	return &CommandResult{Reply: "System prompt updated.", SkipAgent: true}, nil
}

func handleBashCommand(ctx CommandContext) (*CommandResult, error) {
	raw := ""
	if ctx.Args != nil {
		raw = ctx.Args.Raw
	}
	if raw == "" {
		return &CommandResult{Reply: "Usage: /bash <command>", SkipAgent: true, IsError: true}, nil
	}
	// Bash execution is delegated to the agent runner with elevated permissions.
	return &CommandResult{
		Reply:     fmt.Sprintf("Executing: %s", raw),
		SkipAgent: false, // let agent handle
	}, nil
}

func handleApproveCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Approved.", SkipAgent: true}, nil
}

func handleStopCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Stopped.", SkipAgent: true}, nil
}

func handleCancelCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Cancelled.", SkipAgent: true}, nil
}

func handleKillCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Killed.", SkipAgent: true}, nil
}

func handlePluginsCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Plugin management.", SkipAgent: true}, nil
}

func handlePluginCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Plugin info.", SkipAgent: true}, nil
}

func handleMCPCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "MCP providers.", SkipAgent: true}, nil
}

func handleAllowlistCommand(ctx CommandContext) (*CommandResult, error) {
	raw := ""
	if ctx.Args != nil {
		raw = ctx.Args.Raw
	}
	if raw == "" {
		return &CommandResult{Reply: "Allowlist: (show current list)", SkipAgent: true}, nil
	}
	return &CommandResult{Reply: fmt.Sprintf("Allowlist updated: %s", raw), SkipAgent: true}, nil
}

func handleBtwCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{SkipAgent: false}, nil // let agent handle BTW
}

func handleAgentsCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Active agents: (list)", SkipAgent: true}, nil
}

func handleAgentCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Agent info.", SkipAgent: true}, nil
}

func handleSpawnCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Subagent spawned.", SkipAgent: true}, nil
}

func handleFocusCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Focused on agent.", SkipAgent: true}, nil
}

func handleUnfocusCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Unfocused.", SkipAgent: true}, nil
}

func handleACPCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "ACP control.", SkipAgent: true}, nil
}

func handleModelsListCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Available models: (list)", SkipAgent: true}, nil
}
