package server

import "strings"

const nativeClientSessionPrefix = "client:"

// chatWorkspaceSessionPrefix is the 챗봇 (chatbot) workspace namespace the native
// client mints for recall-off conversations (chat:<uuid>). Distinct from the
// client: (업무) namespace; the drawer filters the two apart.
const chatWorkspaceSessionPrefix = "chat:"

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
// at startup should be woken back into the session manager. The live native
// session shapes qualify: the 업무 home (client:main) and its explicit new
// conversations (client:main:<id>), plus every 챗봇 conversation (chat:<uuid>).
// Retired shapes must stay dead so dismissed/obsolete rows don't reappear in the
// drawer on the next restart — the removed topic sessions (client:topic:*, gone
// since #1963) and the pre-main client:<uuid> format both linger on disk but
// should never resurrect. Matching bare isNativeClientSessionKey here is what
// kept reviving them: the gateway restarts every few minutes on SIGUSR1,
// re-scanning the transcript dir each time. The chat: namespace was the inverse
// bug — being excluded, chatbot conversations vanished from the (chat:-filtered)
// drawer on every respawn even mid-conversation, since only a fresh turn would
// re-register them. Both 업무 and 챗봇 sessions run on the "client" channel.
func restorableTranscriptSession(sessionKey string) (channel string, ok bool) {
	if isRestorableNativeSessionKey(sessionKey) || isChatWorkspaceSessionKey(sessionKey) {
		return "client", true
	}
	return "", false
}

// isChatWorkspaceSessionKey reports whether sessionKey belongs to the 챗봇
// (chatbot) workspace — the chat: namespace the native client mints for
// recall-off conversations. Unlike the retired client:topic:* / client:<uuid>
// shapes, chat: is the CURRENT chatbot workspace (added with the recall-focus
// toggle), and its conversations persist transcripts exactly like work sessions,
// so they must survive the restart rescan.
func isChatWorkspaceSessionKey(sessionKey string) bool {
	return strings.HasPrefix(sessionKey, chatWorkspaceSessionPrefix) &&
		strings.TrimPrefix(sessionKey, chatWorkspaceSessionPrefix) != ""
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
