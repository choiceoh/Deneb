// commands_setunset_standard.go — Standard set/unset slash command wrapper.
// Mirrors src/auto-reply/reply/commands-setunset-standard.ts (23 LOC).
package autoreply

// StandardSetUnsetAction is a typed action from a set/unset slash command.
type StandardSetUnsetAction struct {
	Action  string `json:"action"`  // "show", "set", "unset", "error", or custom
	Path    string `json:"path,omitempty"`
	Value   any    `json:"value,omitempty"`
	Name    string `json:"name,omitempty"`
	Message string `json:"message,omitempty"`
}

// StandardSetUnsetParams holds the parameters for parsing a standard set/unset command.
type StandardSetUnsetParams = SetUnsetSlashParams[*StandardSetUnsetAction]

// ParseStandardSetUnsetSlashCommand parses a slash command with standard
// set/unset/error action shapes. Custom known actions are handled via onKnownAction.
// Nil callbacks get default implementations that produce standard action shapes.
func ParseStandardSetUnsetSlashCommand(params StandardSetUnsetParams) (*StandardSetUnsetAction, bool) {
	if params.OnSet == nil {
		params.OnSet = func(path string, value any) *StandardSetUnsetAction {
			return &StandardSetUnsetAction{Action: "set", Path: path, Value: value}
		}
	}
	if params.OnUnset == nil {
		params.OnUnset = func(path string) *StandardSetUnsetAction {
			return &StandardSetUnsetAction{Action: "unset", Path: path}
		}
	}
	if params.OnError == nil {
		params.OnError = func(message string) *StandardSetUnsetAction {
			return &StandardSetUnsetAction{Action: "error", Message: message}
		}
	}
	return ParseSlashCommandWithSetUnset(params)
}
