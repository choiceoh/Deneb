// debug_commands.go — /debug command parsing.
// Mirrors src/auto-reply/reply/debug-commands.ts (26 LOC).
package autoreply

// DebugCommand represents a parsed /debug action.
type DebugCommand struct {
	Action  string `json:"action"`            // "show", "reset", "set", "unset", "error"
	Path    string `json:"path,omitempty"`
	Value   any    `json:"value,omitempty"`
	Message string `json:"message,omitempty"`
}

// ParseDebugCommand parses a /debug slash command.
// Returns nil if the text is not a /debug command.
func ParseDebugCommand(raw string) *DebugCommand {
	result, ok := ParseStandardSetUnsetSlashCommand(StandardSetUnsetParams{
		Raw:            raw,
		Slash:          "/debug",
		InvalidMessage: "Invalid /debug syntax.",
		UsageMessage:   "Usage: /debug show|set|unset|reset",
		OnKnownAction: func(action, _ string) (*StandardSetUnsetAction, bool) {
			if action == "show" {
				return &StandardSetUnsetAction{Action: "show"}, true
			}
			if action == "reset" {
				return &StandardSetUnsetAction{Action: "reset"}, true
			}
			return nil, false
		},
	})
	if !ok || result == nil {
		return nil
	}
	return &DebugCommand{
		Action:  result.Action,
		Path:    result.Path,
		Value:   result.Value,
		Message: result.Message,
	}
}
