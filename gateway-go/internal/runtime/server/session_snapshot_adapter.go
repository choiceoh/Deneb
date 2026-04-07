package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// Compile-time interface compliance.
var _ events.SessionSnapshotProvider = (*sessionSnapshotAdapter)(nil)

// sessionSnapshotAdapter implements events.SessionSnapshotProvider by
// delegating to the session.Manager. It maps session.Session fields to
// events.SessionSnapshot for event enrichment.
type sessionSnapshotAdapter struct {
	sessions *session.Manager
}

func (a *sessionSnapshotAdapter) GetSessionSnapshot(sessionKey string) *events.SessionSnapshot {
	s := a.sessions.Get(sessionKey)
	if s == nil {
		return nil
	}

	snap := &events.SessionSnapshot{
		SessionKey:     s.Key,
		SessionID:      s.SessionID,
		Kind:           string(s.Kind),
		Channel:        s.Channel,
		Label:          s.Label,
		Status:         string(s.Status),
		Model:          s.Model,
		StartedAt:      s.StartedAt,
		EndedAt:        s.EndedAt,
		RuntimeMs:      s.RuntimeMs,
		TotalTokens:    s.TotalTokens,
		AbortedLastRun: s.AbortedLastRun,
	}

	return snap
}
