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

// ParseStandardSetUnsetSlashCommand parses a slash command with standard
// set/unset/error action shapes. Custom known actions are handled via onKnownAction.
func ParseStandardSetUnsetSlashCommand(params struct {
	Raw            string
	Slash          string
	InvalidMessage string
	UsageMessage   string
	OnKnownAction  func(action, args string) (*StandardSetUnsetAction, bool)
	OnSet          func(path string, value any) *StandardSetUnsetAction
	OnUnset        func(path string) *StandardSetUnsetAction
	OnError        func(message string) *StandardSetUnsetAction
}) (*StandardSetUnsetAction, bool) {
	onSet := params.OnSet
	if onSet == nil {
		onSet = func(path string, value any) *StandardSetUnsetAction {
			return &StandardSetUnsetAction{Action: "set", Path: path, Value: value}
		}
	}
	onUnset := params.OnUnset
	if onUnset == nil {
		onUnset = func(path string) *StandardSetUnsetAction {
			return &StandardSetUnsetAction{Action: "unset", Path: path}
		}
	}
	onError := params.OnError
	if onError == nil {
		onError = func(message string) *StandardSetUnsetAction {
			return &StandardSetUnsetAction{Action: "error", Message: message}
		}
	}

	return ParseSlashCommandWithSetUnset(struct {
		Raw            string
		Slash          string
		InvalidMessage string
		UsageMessage   string
		OnKnownAction  func(action, args string) (*StandardSetUnsetAction, bool)
		OnSet          func(path string, value any) *StandardSetUnsetAction
		OnUnset        func(path string) *StandardSetUnsetAction
		OnError        func(message string) *StandardSetUnsetAction
	}{
		Raw:            params.Raw,
		Slash:          params.Slash,
		InvalidMessage: params.InvalidMessage,
		UsageMessage:   params.UsageMessage,
		OnKnownAction:  params.OnKnownAction,
		OnSet:          onSet,
		OnUnset:        onUnset,
		OnError:        onError,
	})
}
