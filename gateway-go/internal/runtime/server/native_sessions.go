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

func restorableTranscriptSession(sessionKey string) (channel string, ok bool) {
	if isNativeClientSessionKey(sessionKey) {
		return "client", true
	}
	if _, ok := parseTelegramChatID(sessionKey); ok {
		return "telegram", true
	}
	return "", false
}

type resumableSessionTarget struct {
	Channel string
	To      string
}

func resumableSessionForMarker(sessionKey string) (resumableSessionTarget, bool) {
	if isNativeClientSessionKey(sessionKey) {
		return resumableSessionTarget{Channel: "client"}, true
	}
	if chatID, ok := parseTelegramChatID(sessionKey); ok {
		return resumableSessionTarget{Channel: "telegram", To: chatID}, true
	}
	return resumableSessionTarget{}, false
}

func shouldRecordChatActivity(sessionKey string) bool {
	if isNativeClientSessionKey(sessionKey) {
		return true
	}
	_, ok := parseTelegramChatID(sessionKey)
	return ok
}

func (s *Server) recordChatActivity(sessionKey string) {
	if s == nil || s.activity == nil || !shouldRecordChatActivity(sessionKey) {
		return
	}
	s.activity.TouchSession(sessionKey)
}
