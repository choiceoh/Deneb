package serverchat

import (
	"strings"

	runtimesession "github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

const nativeClientSessionPrefix = "client:"

func isNativeClientSessionKey(sessionKey string) bool {
	return strings.HasPrefix(sessionKey, nativeClientSessionPrefix) &&
		strings.TrimPrefix(sessionKey, nativeClientSessionPrefix) != ""
}

// isRestorableNativeSessionKey reports whether sessionKey is a currently-valid
// native session shape: exactly client:main, or a client:main:<id> sub-session.
// Legacy/retired keys (client:topic:*, bare client:<uuid>) return false so the
// startup restore cannot revive them. This is intentionally stricter than
// isNativeClientSessionKey, which still governs activity/heartbeat/resume paths.
func isRestorableNativeSessionKey(sessionKey string) bool {
	return sessionKey == runtimesession.NativeWorkSessionKey ||
		strings.HasPrefix(sessionKey, runtimesession.NativeWorkSessionKey+":")
}

type resumableSessionTarget struct {
	Channel string
	To      string
}

func ResumableSessionForMarker(sessionKey string) (resumableSessionTarget, bool) {
	if isNativeClientSessionKey(sessionKey) {
		return resumableSessionTarget{Channel: "client"}, true
	}
	return resumableSessionTarget{}, false
}

func shouldRecordChatActivity(sessionKey string) bool {
	return isNativeClientSessionKey(sessionKey)
}

func (m *Manager) recordChatActivity(sessionKey string) {
	if m == nil || m.Host == nil || m.Host.Activity() == nil || !shouldRecordChatActivity(sessionKey) {
		return
	}
	m.Host.Activity().TouchSession(sessionKey)
}
