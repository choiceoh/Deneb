package server

import (
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	arSession "github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/session"
)

// SessionManager groups session-lifecycle dependencies: the session store
// and autoreply session subsystems.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
//
// Lock hierarchy (acquire in this order; never reverse):
//
//	sessionExtrasMu is independent of session.Manager.mu — never hold both.
type SessionManager struct {
	sessions *session.Manager

	// Autoreply session subsystems.
	abortMemory    *arSession.AbortMemory    // tracks recently aborted sessions for dedup
	historyTracker *arSession.HistoryTracker // per-session conversation history
	sessionUsage   *arSession.SessionUsage   // aggregate token usage for /status reporting

	// sessionExtrasMu guards forgotten keys and the cross-device focus key.
	sessionExtrasMu      sync.Mutex
	forgottenSessionKeys map[string]struct{}
	sessionFocusKey      string
}
