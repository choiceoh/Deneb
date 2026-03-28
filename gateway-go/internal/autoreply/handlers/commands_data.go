// commands_data.go — Hardcoded command metadata dataset.
// Mirrors src/auto-reply/commands-registry.data.ts (808 LOC).
// Contains the full set of built-in chat command definitions.
package handlers

// BuiltinChatCommands returns the full set of built-in chat commands.
func BuiltinChatCommands() []ChatCommandDefinition {
	return []ChatCommandDefinition{
		// Session lifecycle
		{Key: "new", NativeName: "new", Description: "Start a new session", TextAliases: []string{"/new"}, Scope: ScopeBoth, Category: CategorySession},
		{Key: "reset", NativeName: "reset", Description: "Reset the current session", TextAliases: []string{"/reset"}, Scope: ScopeBoth, Category: CategorySession},
		{Key: "fork", NativeName: "fork", Description: "Fork the current session", TextAliases: []string{"/fork"}, Scope: ScopeBoth, Category: CategorySession},
		{Key: "continue", NativeName: "continue", Description: "Continue a previous session", TextAliases: []string{"/continue"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategorySession},

		// Status & info
		{Key: "status", NativeName: "status", Description: "Show session status", TextAliases: []string{"/status"}, Scope: ScopeBoth, Category: CategoryStatus},
		{Key: "help", NativeName: "help", Description: "Show available commands", TextAliases: []string{"/help"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryStatus},
		{Key: "context", NativeName: "context", Description: "Show context usage", TextAliases: []string{"/context"}, Scope: ScopeBoth, Category: CategoryStatus},
		{Key: "info", NativeName: "info", Description: "Show agent info", TextAliases: []string{"/info"}, Scope: ScopeBoth, Category: CategoryStatus},
		{Key: "usage", NativeName: "usage", Description: "Toggle token usage display", TextAliases: []string{"/usage"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryStatus},

		// Model & thinking
		{Key: "model", NativeName: "model", Description: "Set or show the model", TextAliases: []string{"/model"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryOptions},
		{Key: "models", NativeName: "models", Description: "List available models", TextAliases: []string{"/models"}, Scope: ScopeBoth, Category: CategoryOptions},
		{Key: "think", NativeName: "think", Description: "Set thinking level", TextAliases: []string{"/think"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryOptions},
		{Key: "fast", NativeName: "fast", Description: "Toggle fast mode", TextAliases: []string{"/fast"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryOptions},
		{Key: "verbose", NativeName: "verbose", Description: "Set verbose level", TextAliases: []string{"/verbose"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryOptions},
		{Key: "reasoning", NativeName: "reasoning", Description: "Set reasoning display", TextAliases: []string{"/reasoning"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryOptions},
		{Key: "elevated", NativeName: "elevated", Description: "Set elevated mode", TextAliases: []string{"/elevated"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryOptions},

		// Config
		{Key: "config", Description: "View or edit config", TextAliases: []string{"/config"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryManagement},
		{Key: "set", Description: "Set a config value", TextAliases: []string{"/set"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryManagement},
		{Key: "unset", Description: "Remove a config value", TextAliases: []string{"/unset"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryManagement},
		{Key: "system-prompt", NativeName: "system-prompt", Description: "Set system prompt", TextAliases: []string{"/system-prompt", "/sp"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryManagement},

		// Execution
		{Key: "bash", NativeName: "bash", Description: "Execute a bash command", TextAliases: []string{"/bash", "/sh"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryTools,
			Args: []CommandArgDefinition{{Name: "command", Type: "string", CaptureRemaining: true}}},
		{Key: "approve", NativeName: "approve", Description: "Approve pending action", TextAliases: []string{"/approve", "/yes", "/y"}, Scope: ScopeBoth, Category: CategoryTools},
		{Key: "stop", NativeName: "stop", Description: "Stop current execution", TextAliases: []string{"/stop"}, Scope: ScopeBoth, Category: CategoryTools},
		{Key: "cancel", NativeName: "cancel", Description: "Cancel current execution", TextAliases: []string{"/cancel"}, Scope: ScopeBoth, Category: CategoryTools},
		{Key: "kill", NativeName: "kill", Description: "Force kill execution", TextAliases: []string{"/kill"}, Scope: ScopeBoth, Category: CategoryTools},

		// Session management
		{Key: "compact", NativeName: "compact", Description: "Compact context history", TextAliases: []string{"/compact"}, Scope: ScopeBoth, Category: CategorySession},
		{Key: "export", NativeName: "export", Description: "Export session transcript", TextAliases: []string{"/export"}, Scope: ScopeBoth, Category: CategorySession},
		{Key: "activation", NativeName: "activation", Description: "Set group activation mode", TextAliases: []string{"/activation"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategorySession},
		{Key: "send", Description: "Set send policy", TextAliases: []string{"/send"}, AcceptsArgs: true, Scope: ScopeText, Category: CategorySession},

		// Plugins & tools
		{Key: "plugins", Description: "Manage plugins", TextAliases: []string{"/plugins"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryManagement},
		{Key: "plugin", Description: "Plugin info", TextAliases: []string{"/plugin"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryManagement},
		{Key: "mcp", Description: "MCP provider management", TextAliases: []string{"/mcp"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryManagement},

		// Allowlist
		{Key: "allowlist", Description: "Manage sender allowlist", TextAliases: []string{"/allowlist"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryManagement},

		// BTW (side question)
		{Key: "btw", NativeName: "btw", Description: "Ask a side question", TextAliases: []string{"/btw"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategorySession},

		// Subagents
		{Key: "agents", NativeName: "agents", Description: "List subagents", TextAliases: []string{"/agents"}, Scope: ScopeBoth, Category: CategoryTools},
		{Key: "agent", NativeName: "agent", Description: "Subagent info", TextAliases: []string{"/agent"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryTools},
		{Key: "spawn", NativeName: "spawn", Description: "Spawn a subagent", TextAliases: []string{"/spawn"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryTools},
		{Key: "focus", NativeName: "focus", Description: "Focus on a subagent", TextAliases: []string{"/focus"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryTools},
		{Key: "unfocus", NativeName: "unfocus", Description: "Unfocus subagent", TextAliases: []string{"/unfocus"}, Scope: ScopeBoth, Category: CategoryTools},

		// ACP
		{Key: "acp", NativeName: "acp", Description: "Agent Control Protocol", TextAliases: []string{"/acp"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryTools},

		// Debug (gated)
		{Key: "debug", Description: "Debug commands", TextAliases: []string{"/debug"}, AcceptsArgs: true, Scope: ScopeText, Category: CategoryManagement},
	}
}
