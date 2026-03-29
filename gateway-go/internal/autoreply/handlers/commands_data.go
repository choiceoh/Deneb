// commands_data.go — Hardcoded command metadata dataset.
// Contains the full set of built-in chat command definitions.
// Native (Telegram) commands are limited to the ones actively used:
// status, model, models, compact, reset, verbose.
package handlers

// BuiltinChatCommands returns the full set of built-in chat commands.
func BuiltinChatCommands() []ChatCommandDefinition {
	return []ChatCommandDefinition{
		// Session lifecycle
		{Key: "new", Description: "Start a new session", TextAliases: []string{"/new"}, Scope: ScopeText, Category: CategorySession},
		{Key: "reset", NativeName: "reset", Description: "Reset the current session", TextAliases: []string{"/reset"}, Scope: ScopeBoth, Category: CategorySession},
		{Key: "fork", Description: "Fork the current session", TextAliases: []string{"/fork"}, Scope: ScopeText, Category: CategorySession},
		{Key: "continue", Description: "Continue a previous session", TextAliases: []string{"/continue"}, AcceptsArgs: true, Scope: ScopeText, Category: CategorySession},

		// Status & info
		{Key: "status", NativeName: "status", Description: "Show session status", TextAliases: []string{"/status"}, Scope: ScopeBoth, Category: CategoryStatus},
		{Key: "help", Description: "Show available commands", TextAliases: []string{"/help"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryStatus},
		{Key: "context", Description: "Show context usage", TextAliases: []string{"/context"}, Scope: ScopeText, Category: CategoryStatus},
		{Key: "info", Description: "Show agent info", TextAliases: []string{"/info"}, Scope: ScopeText, Category: CategoryStatus},
		{Key: "usage", Description: "Toggle token usage display", TextAliases: []string{"/usage"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryStatus},

		// Model & thinking
		{Key: "model", NativeName: "model", Description: "Set or show the model", TextAliases: []string{"/model"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryOptions},
		{Key: "think", Description: "Set thinking level", TextAliases: []string{"/think"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryOptions},
		{Key: "fast", Description: "Toggle fast mode", TextAliases: []string{"/fast"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryOptions},
		{Key: "verbose", NativeName: "verbose", Description: "Set verbose level", TextAliases: []string{"/verbose"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryOptions},
		{Key: "reasoning", Description: "Set reasoning display", TextAliases: []string{"/reasoning"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryOptions},
		{Key: "elevated", Description: "Set elevated mode", TextAliases: []string{"/elevated"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryOptions},

		// Config
		{Key: "config", Description: "View or edit config", TextAliases: []string{"/config"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryManagement},
		{Key: "set", Description: "Set a config value", TextAliases: []string{"/set"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryManagement},
		{Key: "unset", Description: "Remove a config value", TextAliases: []string{"/unset"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryManagement},
		{Key: "system-prompt", Description: "Set system prompt", TextAliases: []string{"/system-prompt", "/sp"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryManagement},

		// Execution
		{Key: "bash", Description: "Execute a bash command", TextAliases: []string{"/bash", "/sh"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryTools,
			Args: []CommandArgDefinition{{Name: "command", Type: "string", CaptureRemaining: true}}},
		{Key: "approve", Description: "Approve pending action", TextAliases: []string{"/approve", "/yes", "/y"}, Scope: ScopeText, Category: CategoryTools},
		{Key: "stop", Description: "Stop current execution", TextAliases: []string{"/stop"}, Scope: ScopeText, Category: CategoryTools},
		{Key: "cancel", Description: "Cancel current execution", TextAliases: []string{"/cancel"}, Scope: ScopeText, Category: CategoryTools},
		{Key: "kill", Description: "Force kill execution", TextAliases: []string{"/kill"}, Scope: ScopeText, Category: CategoryTools},

		// Session management
		{Key: "compact", NativeName: "compact", Description: "Compact context history", TextAliases: []string{"/compact"}, Scope: ScopeBoth, Category: CategorySession},
		{Key: "export", Description: "Export session transcript", TextAliases: []string{"/export"}, Scope: ScopeText, Category: CategorySession},
		{Key: "activation", Description: "Set group activation mode", TextAliases: []string{"/activation"}, AcceptsArgs: true, Scope: ScopeText, Category: CategorySession},
		{Key: "send", Description: "Set send policy", TextAliases: []string{"/send"}, AcceptsArgs: true, Scope: ScopeText, Category: CategorySession},

		// Plugins & tools
		{Key: "plugins", Description: "Manage plugins", TextAliases: []string{"/plugins"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryManagement},
		{Key: "plugin", Description: "Plugin info", TextAliases: []string{"/plugin"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryManagement},
		{Key: "mcp", Description: "MCP provider management", TextAliases: []string{"/mcp"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryManagement},

		// Allowlist
		{Key: "allowlist", Description: "Manage sender allowlist", TextAliases: []string{"/allowlist"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryManagement},

		// Subagents
		{Key: "agents", Description: "List active subagents", TextAliases: []string{"/agents"}, Scope: ScopeText, Category: CategoryTools},

		// ACP
		{Key: "acp", Description: "Agent Control Protocol", TextAliases: []string{"/acp"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryTools},

		// Debug (gated)
		{Key: "debug", Description: "Debug commands", TextAliases: []string{"/debug"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryManagement},
	}
}
