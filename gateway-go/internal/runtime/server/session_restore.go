package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
)

// restoreAndWakeSessions scans the transcript directory for persisted user
// sessions and restores them to the in-memory session manager.
//
// Called once at startup after all channel plugins have had a chance to start.
func (s *Server) restoreAndWakeSessions(_ context.Context) {
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

	// Stored conversation titles survive the restart (the manager is in-memory
	// and hot-swap restarts are frequent); sessions the store doesn't know get
	// queued for a one-shot title backfill below. See session_labels.go.
	storedLabels := map[string]string{}
	if path, pathErr := sessionLabelStorePath(); pathErr == nil {
		storedLabels = loadSessionLabels(path)
	}
	// Restore user rename-pins too, so the auto-titler keeps its hands off a
	// user-chosen name across the restart (session_labels.go).
	storedPins := map[string]bool{}
	if pinsPath, pinsErr := sessionPinsStorePath(); pinsErr == nil {
		storedPins = loadSessionPins(pinsPath)
	}
	storedModels := map[string]string{}
	if modelsPath, modelsErr := sessionModelsStorePath(); modelsErr == nil {
		storedModels = loadSessionModels(modelsPath)
	}
	storedListPins := map[string]bool{}
	if listPinsPath, listPinsErr := sessionListPinsStorePath(); listPinsErr == nil {
		storedListPins = loadSessionListPins(listPinsPath)
	}
	s.restoreSessionFocus()
	var untitled []string

	var restored int
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		sessionKey := strings.TrimSuffix(name, ".jsonl")
		channel, ok := session.RestorableTranscriptChannel(sessionKey)
		if !ok {
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

		label := storedLabels[sessionKey]
		if label == "" && chat.IsAutoTitleSessionKey(sessionKey) {
			untitled = append(untitled, sessionKey)
		}

		if err := s.sessions.Set(&session.Session{
			Key:         sessionKey,
			Kind:        session.KindDirect,
			Status:      session.StatusDone,
			Channel:     channel,
			Label:       label,
			LabelPinned: storedPins[sessionKey],
			Pinned:      storedListPins[sessionKey],
			Model:       storedModels[sessionKey],
			UpdatedAt:   updatedAt,
		}); err != nil {
			s.logger.Warn("session restore: failed to restore session",
				"session", sessionKey, "error", err)
			continue
		}
		restored++
	}

	s.backfillSessionTitles(transcriptDir, sortSessionKeysNewestFirst(transcriptDir, untitled))

	if restored == 0 {
		return
	}

	s.logger.Info("session restore: restored sessions", "count", restored)
}
