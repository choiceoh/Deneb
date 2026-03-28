package autoreply

import (
	"regexp"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/commands"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

var activationCmdRe = regexp.MustCompile(`(?i)^/activation(?:\s+([a-zA-Z]+))?\s*$`)

// ParseActivationCommand parses an /activation command.
// Returns hasCommand=true if the text is an activation command, with an
// optional mode if one was specified.
// This function stays in autoreply because it depends on *CommandRegistry.
func ParseActivationCommand(raw string, registry *commands.CommandRegistry) (hasCommand bool, mode types.GroupActivationMode) {
	if raw == "" {
		return false, ""
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false, ""
	}
	normalized := trimmed
	if registry != nil {
		normalized = registry.NormalizeCommandBody(trimmed, "")
	}
	m := activationCmdRe.FindStringSubmatch(normalized)
	if m == nil {
		return false, ""
	}
	if m[1] != "" {
		mode, ok := types.NormalizeGroupActivation(m[1])
		if ok {
			return true, mode
		}
	}
	return true, ""
}
