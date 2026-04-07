package types

import "strings"

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
