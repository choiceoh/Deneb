package server

import "strings"

const nativeClientSessionPrefix = "client:"

func isNativeClientSessionKey(sessionKey string) bool {
	return strings.HasPrefix(sessionKey, nativeClientSessionPrefix) &&
		strings.TrimPrefix(sessionKey, nativeClientSessionPrefix) != ""
}

func heartbeatTargetSessionKey(lastSessionKey string) string {
	if isNativeClientSessionKey(lastSessionKey) {
		return lastSessionKey
	}
	return nativeWorkSessionKey
}

// restorableTranscriptSession decides whether a transcript file found on disk
// at startup should be woken back into the session manager. Only the live
// native session shapes qualify: the single home session (client:main) and
// explicit new conversations (client:main:<id>). Retired shapes must stay dead
// so dismissed/obsolete rows don't reappear in the drawer on the next restart —
// the removed topic sessions (client:topic:*, gone since #1963) and the pre-main
// client:<uuid> format both linger on disk but should never resurrect. Matching
// bare isNativeClientSessionKey here is what kept reviving them: the gateway
// restarts every few minutes on SIGUSR1, re-scanning the transcript dir each time.
func restorableTranscriptSession(sessionKey string) (channel string, ok bool) {
	if isRestorableNativeSessionKey(sessionKey) {
		return "client", true
	}
	return "", false
}

// isRestorableNativeSessionKey reports whether sessionKey is a currently-valid
// native session shape: exactly client:main, or a client:main:<id> sub-session.
// Legacy/retired keys (client:topic:*, bare client:<uuid>) return false so the
// startup restore cannot revive them. This is intentionally stricter than
// isNativeClientSessionKey, which still governs activity/heartbeat/resume paths.
func isRestorableNativeSessionKey(sessionKey string) bool {
	return sessionKey == nativeWorkSessionKey ||
		strings.HasPrefix(sessionKey, nativeWorkSessionKey+":")
}

type resumableSessionTarget struct {
	Channel string
	To      string
}

func resumableSessionForMarker(sessionKey string) (resumableSessionTarget, bool) {
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
