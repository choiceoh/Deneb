package server

import (
	arSession "github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// SessionManager groups session-lifecycle dependencies: the session store
// and autoreply session subsystems.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type SessionManager struct {
	sessions *session.Manager

	// Autoreply session subsystems.
	abortMemory    *arSession.AbortMemory    // tracks recently aborted sessions for dedup
	historyTracker *arSession.HistoryTracker // per-session conversation history
	sessionUsage   *arSession.SessionUsage   // aggregate token usage for /status reporting
}
