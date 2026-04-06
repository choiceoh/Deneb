package chat

import "github.com/choiceoh/deneb/gateway-go/internal/agent"

// shouldSilenceForChannel returns true if any of the tool activities match a
// channel-silent tool for the given channel. When true, the chat delivery
// should be suppressed — the tool executed normally but the response should
// not be sent as a chat message.
func shouldSilenceForChannel(channel string, activities []agent.ToolActivity) bool {
	if channel == "" || len(activities) == 0 {
		return false
	}
	names, ok := channelSilentTools[channel]
	if !ok || len(names) == 0 {
		return false
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	for _, a := range activities {
		if set[a.Name] {
			return true
		}
	}
	return false
}
