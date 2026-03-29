package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/tokens"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// startupHeartbeatDelay gives Telegram polling time to finish connecting
// before we send heartbeat messages into restored sessions.
const startupHeartbeatDelay = 3 * time.Second

// restoreAndWakeSessions scans the transcript directory for persisted Telegram
// sessions, restores them to the in-memory session manager, then fires a
// startup heartbeat so the agent can check HEARTBEAT.md and resume any
// pending work from before the restart.
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

	var restored []string
	for _, entry := range entries {
		name := entry.Name()
		// Only restore Telegram sessions; other kinds (cron:, btw:, etc.) are transient.
		if !strings.HasPrefix(name, "telegram:") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		sessionKey := strings.TrimSuffix(name, ".jsonl")

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
		restored = append(restored, sessionKey)
	}

	if len(restored) == 0 {
		return
	}

	s.logger.Info("session restore: restored sessions", "count", len(restored))

	// Send a startup heartbeat to each restored session after a brief delay.
	// The delay gives the Telegram channel enough time to connect so replies
	// can be delivered.
	s.safeGo("session-restore:heartbeat", func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(startupHeartbeatDelay):
		}

		for _, sessionKey := range restored {
			if ctx.Err() != nil {
				return
			}
			chatID := strings.TrimPrefix(sessionKey, "telegram:")
			s.sendStartupHeartbeat(ctx, sessionKey, chatID)
		}
	})
}

// sendStartupHeartbeat sends a one-time startup heartbeat to a restored
// Telegram session. The agent checks HEARTBEAT.md and replies HEARTBEAT_OK
// if nothing needs attention; that reply is suppressed by the delivery
// pipeline so the user only sees a message if real work is pending.
func (s *Server) sendStartupHeartbeat(ctx context.Context, sessionKey, chatID string) {
	if s.chatHandler == nil {
		return
	}

	req, err := protocol.NewRequestFrame(
		"startup-heartbeat-"+chatID,
		"chat.send",
		map[string]any{
			"sessionKey": sessionKey,
			"message":    tokens.HeartbeatPrompt,
			"delivery": map[string]any{
				"channel": "telegram",
				"to":      chatID,
			},
		},
	)
	if err != nil {
		s.logger.Error("session restore: failed to build heartbeat request",
			"session", sessionKey, "error", err)
		return
	}

	hbCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	resp := s.chatHandler.Send(hbCtx, req)
	if resp != nil && !resp.OK {
		s.logger.Warn("session restore: heartbeat failed",
			"session", sessionKey, "error", resp.Error)
	} else {
		s.logger.Info("session restore: heartbeat sent", "session", sessionKey)
	}
}
