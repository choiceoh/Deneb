package session

import "strings"

const (
	// NativeWorkSessionKey is the primary native work conversation.
	NativeWorkSessionKey = "client:main"
	// DreamWorkSessionKey is the dedicated background-dream conversation.
	DreamWorkSessionKey = NativeWorkSessionKey + ":dream"
	// NativeWorkSessionTarget is the channel target portion of client:main.
	NativeWorkSessionTarget = "main"
	// HeartbeatWorkSessionKey is the ISOLATED session the 30-min heartbeat turn
	// reasons in. Kept separate from client:main so autonomous ticks never
	// assemble or compact the user's live conversation (the old client:main
	// piggyback forced a Polaris compaction of client:main every tick). The
	// heartbeat's user-facing report is delivered separately via the proactive
	// relay (RelayNative → client:main + push), so isolation costs no visibility.
	HeartbeatWorkSessionKey = "submain:heartbeat"
)

// RestorableTranscriptChannel classifies transcript keys that should be
// restored into the native session drawer after a gateway restart. It accepts
// the current client:main hierarchy and readable legacy chat conversations,
// while rejecting retired client:topic and bare client identifiers.
func RestorableTranscriptChannel(sessionKey string) (channel string, ok bool) {
	isNative := sessionKey == NativeWorkSessionKey ||
		strings.HasPrefix(sessionKey, NativeWorkSessionKey+":")
	isLegacyChat := strings.HasPrefix(sessionKey, "chat:") &&
		strings.TrimPrefix(sessionKey, "chat:") != ""
	if isNative || isLegacyChat {
		return "client", true
	}
	return "", false
}

// HeartbeatTargetSession keeps an active native conversation as the target and
// otherwise falls back to the primary work session.
func HeartbeatTargetSession(lastSessionKey string) string {
	if strings.HasPrefix(lastSessionKey, "client:") && strings.TrimPrefix(lastSessionKey, "client:") != "" {
		return lastSessionKey
	}
	return NativeWorkSessionKey
}
