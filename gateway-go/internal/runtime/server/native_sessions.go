package server

import (
	"strings"

	runtimesession "github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

const nativeClientSessionPrefix = "client:"

func isNativeClientSessionKey(sessionKey string) bool {
	return runtimesession.IsClientSession(sessionKey)
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

func resumableSessionForMarker(sessionKey string) (resumableSessionTarget, bool) {
	// Delegated sub-agent runs are client: sessions (KindDirect) but are scratch
	// workspaces, not user conversations — resuming one re-dispatches tools and
	// can duplicate side effects (emails sent twice, files written twice).
	if runtimesession.IsSpawnedChildKey(sessionKey) {
		return resumableSessionTarget{}, false
	}
	if isNativeClientSessionKey(sessionKey) {
		return resumableSessionTarget{Channel: "client"}, true
	}
	return resumableSessionTarget{}, false
}

func shouldRecordChatActivity(sessionKey string) bool {
	return isNativeClientSessionKey(sessionKey)
}

func (s *Server) recordChatActivity(sessionKey string) {
	if s == nil || s.activity == nil || !shouldRecordChatActivity(sessionKey) {
		return
	}
	s.activity.TouchSession(sessionKey)
}
