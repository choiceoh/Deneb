package server

import (
	arSession "github.com/choiceoh/deneb/gateway-go/internal/autoreply/session"
	handlerpresence "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/presence"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/transcript"
)

// SessionManager groups session-lifecycle dependencies: the session store,
// key cache, transcript writer, and per-session RPC state (presence, heartbeat).
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type SessionManager struct {
	sessions       *session.Manager
	keyCache       *session.KeyCache
	transcript     *transcript.Writer
	presenceStore  *handlerpresence.Store
	heartbeatState *handlerpresence.HeartbeatState

	// Autoreply session subsystems.
	abortMemory    *arSession.AbortMemory    // tracks recently aborted sessions for dedup
	historyTracker *arSession.HistoryTracker  // per-session conversation history
	sessionUsage   *arSession.SessionUsage    // aggregate token usage for /status reporting
}
