// mcp_commands.go — /mcp command parsing.
// Mirrors src/auto-reply/reply/mcp-commands.ts (24 LOC).
package commands

// McpCommand represents a parsed /mcp action.
type McpCommand struct {
	Action  string `json:"action"` // "show", "set", "unset", "error"
	Name    string `json:"name,omitempty"`
	Value   any    `json:"value,omitempty"`
	Message string `json:"message,omitempty"`
}

// ParseMcpCommand parses a /mcp slash command.
// Returns nil if the text is not a /mcp command.
func ParseMcpCommand(raw string) *McpCommand {
	result, ok := ParseStandardSetUnsetSlashCommand(StandardSetUnsetParams{
		Raw:            raw,
		Slash:          "/mcp",
		InvalidMessage: "Invalid /mcp syntax.",
		UsageMessage:   "Usage: /mcp show|set|unset",
		OnKnownAction: func(action, args string) (*StandardSetUnsetAction, bool) {
			if action == "show" || action == "get" {
				name := ""
				if args != "" {
					name = args
				}
				return &StandardSetUnsetAction{Action: "show", Name: name}, true
			}
			return nil, false
		},
		OnSet: func(name string, value any) *StandardSetUnsetAction {
			return &StandardSetUnsetAction{Action: "set", Name: name, Value: value}
		},
		OnUnset: func(name string) *StandardSetUnsetAction {
			return &StandardSetUnsetAction{Action: "unset", Name: name}
		},
	})
	if !ok || result == nil {
		return nil
	}
	return &McpCommand{
		Action:  result.Action,
		Name:    result.Name,
		Value:   result.Value,
		Message: result.Message,
	}
}
