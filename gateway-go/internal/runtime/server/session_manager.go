package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/domainbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/pipebind"
)

// SessionManager groups session-lifecycle dependencies: the session store
// and autoreply session subsystems.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type SessionManager struct {
	sessions *domainbind.Manager

	// Autoreply session subsystems.
	abortMemory    *pipebind.AbortMemory    // tracks recently aborted sessions for dedup
	historyTracker *pipebind.HistoryTracker // per-session conversation history
	sessionUsage   *pipebind.SessionUsage   // aggregate token usage for /status reporting
}
