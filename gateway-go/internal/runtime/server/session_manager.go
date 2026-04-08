package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/transcript"
	arSession "github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// SessionManager groups session-lifecycle dependencies: the session store,
// transcript writer, and autoreply session subsystems.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type SessionManager struct {
	sessions   *session.Manager
	transcript *transcript.Writer

	// Autoreply session subsystems.
	abortMemory    *arSession.AbortMemory    // tracks recently aborted sessions for dedup
	historyTracker *arSession.HistoryTracker // per-session conversation history
	sessionUsage   *arSession.SessionUsage   // aggregate token usage for /status reporting
}

