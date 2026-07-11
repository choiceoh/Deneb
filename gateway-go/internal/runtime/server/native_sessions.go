package server

import (
	"strings"

	runtimesession "github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

const nativeClientSessionPrefix = "client:"

// chatWorkspaceSessionPrefix is the LEGACY 챗봇 workspace namespace
// (chat:<uuid>). The 챗봇 mode was removed — no new chat: sessions are minted —
// but old conversations persist on disk and must stay listed/readable in the
// drawer, where they now behave as ordinary 업무 sessions.
const chatWorkspaceSessionPrefix = "chat:"

func isNativeClientSessionKey(sessionKey string) bool {
	return strings.HasPrefix(sessionKey, nativeClientSessionPrefix) &&
		strings.TrimPrefix(sessionKey, nativeClientSessionPrefix) != ""
}

// restorableTranscriptSession decides whether a transcript file found on disk
// at startup should be woken back into the session manager. The live native
// session shapes qualify: the 업무 home (client:main) and its explicit new
// conversations (client:main:<id>), plus legacy 챗봇 conversations (chat:<uuid>)
// which stay readable after the mode's removal. Retired shapes must stay dead
// so dismissed/obsolete rows don't reappear in the drawer on the next restart —
// the removed topic sessions (client:topic:*, gone since #1963) and the
// pre-main client:<uuid> format both linger on disk but should never
// resurrect. Matching bare isNativeClientSessionKey here is what kept reviving
// them: the gateway restarts every few minutes on SIGUSR1, re-scanning the
// transcript dir each time. All these sessions run on the "client" channel.
// isChatWorkspaceSessionKey reports whether sessionKey belongs to the legacy
// 챗봇 namespace (chat:<uuid>). Unlike the retired client:topic:* /
// client:<uuid> shapes, chat: conversations persist transcripts exactly like
// work sessions and stay user-visible after the mode's removal, so they must
// survive the restart rescan.
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
	return sessionKey == runtimesession.NativeWorkSessionKey ||
		strings.HasPrefix(sessionKey, runtimesession.NativeWorkSessionKey+":")
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
