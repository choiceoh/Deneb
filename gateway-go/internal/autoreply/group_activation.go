package autoreply

import (
	"regexp"
	"strings"
)

// GroupActivationMode controls when the bot responds in group chats.
type GroupActivationMode string

const (
	ActivationMention GroupActivationMode = "mention" // respond only when mentioned
	ActivationAlways  GroupActivationMode = "always"  // respond to all messages
)

// NormalizeGroupActivation validates and normalizes a group activation mode string.
func NormalizeGroupActivation(raw string) (GroupActivationMode, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "mention":
		return ActivationMention, true
	case "always":
		return ActivationAlways, true
	default:
		return "", false
	}
}

var activationCmdRe = regexp.MustCompile(`(?i)^/activation(?:\s+([a-zA-Z]+))?\s*$`)

// ParseActivationCommand parses an /activation command.
// Returns hasCommand=true if the text is an activation command, with an
// optional mode if one was specified.
func ParseActivationCommand(raw string, registry *CommandRegistry) (hasCommand bool, mode GroupActivationMode) {
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
		mode, ok := NormalizeGroupActivation(m[1])
		if ok {
			return true, mode
		}
	}
	return true, ""
}
