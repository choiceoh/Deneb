package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

// restoreAndWakeSessions scans the transcript directory for persisted Telegram
// sessions and restores them to the in-memory session manager.
//
// Called once at startup after all channel plugins have had a chance to start.
func (s *Server) restoreAndWakeSessions(ctx context.Context) {
	home, err := os.UserHomeDir()
	if err != nil {
		s.logger.Warn("session restore: cannot determine home dir", "error", err)
		return
	}
	transcriptDir := filepath.Join(home, ".deneb", "transcripts")

	entries, err := os.ReadDir(transcriptDir)
	if err != nil {
		if !os.IsNotExist(err) {
			s.logger.Warn("session restore: cannot read transcript dir", "error", err)
		}
		return
	}

	var restored int
	for _, entry := range entries {
		name := entry.Name()
		// Only restore Telegram sessions; other kinds (cron:, btw:, etc.) are transient.
		if !strings.HasPrefix(name, "telegram:") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		sessionKey := strings.TrimSuffix(name, ".jsonl")

		// Only restore main sessions (telegram:<chatID>), not sub-sessions
		// (telegram:<chatID>:<name>:<ts>) which are transient agent tasks.
		chatID := strings.TrimPrefix(sessionKey, "telegram:")
		if strings.Contains(chatID, ":") {
			continue
		}

		// Skip sessions already in memory (should not occur at startup, but be safe).
		if s.sessions.Get(sessionKey) != nil {
			continue
		}

		// Use transcript file mod time as updatedAt so the session appears
		// with its most-recent activity timestamp rather than "now".
		var updatedAt int64
		if info, infoErr := entry.Info(); infoErr == nil {
			updatedAt = info.ModTime().UnixMilli()
		} else {
			updatedAt = time.Now().UnixMilli()
		}

		if err := s.sessions.Set(&session.Session{
			Key:       sessionKey,
			Kind:      session.KindDirect,
			Status:    session.StatusDone,
			Channel:   "telegram",
			UpdatedAt: updatedAt,
		}); err != nil {
			s.logger.Warn("session restore: failed to restore session",
				"session", sessionKey, "error", err)
			continue
		}
		restored++
	}

	if restored == 0 {
		return
	}

	s.logger.Info("session restore: restored sessions", "count", restored)
}
