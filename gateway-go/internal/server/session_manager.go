package server

import (
	"os"
	"path/filepath"

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
	sessionWAL     *session.WAL
	keyCache       *session.KeyCache
	transcript     *transcript.Writer
	presenceStore  *handlerpresence.Store
	heartbeatState *handlerpresence.HeartbeatState

	// Autoreply session subsystems.
	abortMemory    *arSession.AbortMemory    // tracks recently aborted sessions for dedup
	historyTracker *arSession.HistoryTracker // per-session conversation history
	sessionUsage   *arSession.SessionUsage   // aggregate token usage for /status reporting
}

// startSessionWAL initializes the session WAL for crash recovery.
// Replays existing entries to restore sessions, then subscribes to future mutations.
func (s *Server) startSessionWAL() {
	home, err := os.UserHomeDir()
	if err != nil {
		s.logger.Warn("session wal: cannot determine home dir", "error", err)
		return
	}
	walDir := filepath.Join(home, ".deneb")

	wal := session.NewWAL(s.sessions, session.WALConfig{
		Dir:    walDir,
		Logger: s.logger,
	})
	if err := wal.Start(); err != nil {
		s.logger.Warn("session wal: start failed, continuing without persistence", "error", err)
		return
	}
	s.sessionWAL = wal
}
