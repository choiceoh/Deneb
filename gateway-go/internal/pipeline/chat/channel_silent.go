package chat

import "github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"

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
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	for _, a := range activities {
		if _, ok := set[a.Name]; ok {
			return true
		}
	}
	return false
}
