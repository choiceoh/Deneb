// commands_data.go — Hardcoded command metadata dataset.
// Contains the full set of built-in chat command definitions.
// Native (Telegram) commands are limited to the ones actively used:
// status, model, compact, reset, verbose.
package handlers

// BuiltinChatCommands returns the full set of built-in chat commands.
func BuiltinChatCommands() []ChatCommandDefinition {
	return []ChatCommandDefinition{
		// Session lifecycle
		{Key: "reset", NativeName: "reset", Description: "Reset the current session", TextAliases: []string{"/reset"}, Scope: ScopeBoth, Category: CategorySession},
		{Key: "stop", Description: "Stop current execution", TextAliases: []string{"/stop"}, Scope: ScopeText, Category: CategoryTools},
		{Key: "cancel", Description: "Cancel current execution", TextAliases: []string{"/cancel"}, Scope: ScopeText, Category: CategoryTools},
		{Key: "kill", Description: "Force kill execution", TextAliases: []string{"/kill"}, Scope: ScopeText, Category: CategoryTools},
		{Key: "compact", NativeName: "compact", Description: "Compact context history", TextAliases: []string{"/compact"}, Scope: ScopeBoth, Category: CategorySession},

		// Status & info
		{Key: "status", NativeName: "status", Description: "Show session status", TextAliases: []string{"/status"}, Scope: ScopeBoth, Category: CategoryStatus},

		// Model
		{Key: "model", NativeName: "model", Description: "Set or show the model", TextAliases: []string{"/model"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryOptions},
		{Key: "verbose", NativeName: "verbose", Description: "Set verbose level", TextAliases: []string{"/verbose"}, AcceptsArgs: true, Scope: ScopeBoth, Category: CategoryOptions},

		// Subagents
		{Key: "agents", Description: "List active subagents", TextAliases: []string{"/agents"}, Scope: ScopeText, Category: CategoryTools},

		// Monitoring
		{Key: "zerocalls", Description: "Show RPC methods with zero calls", TextAliases: []string{"/zerocalls", "/zc"}, Scope: ScopeText, Category: CategoryStatus},

		// Help & info
		{Key: "help", Description: "Show available commands", TextAliases: []string{"/help"}, Scope: ScopeText, Category: CategoryStatus},
		{Key: "commands", Description: "List commands (paginated)", TextAliases: []string{"/commands"}, AcceptsArgs: true, Args: []CommandArgDefinition{{Name: "page", Description: "Page number (0-indexed)", Type: "number"}}, Scope: ScopeText, Category: CategoryStatus},

		// Routine shortcuts (rewrite → agent passthrough)
		{Key: "morning", NativeName: "morning", Description: "모닝레터 발송", TextAliases: []string{"/morning"}, Scope: ScopeBoth, Category: CategoryTools},
	}
}
