package rl

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

// SessionHook subscribes to session lifecycle events and collects
// completed sessions as training trajectories (fallback collection path).
type SessionHook struct {
	store    *Store
	sessions *session.Manager
	cfg      CollectionConfig
	logger   *slog.Logger
	unsub    func()
}

// NewSessionHook creates and subscribes a hook to the session event bus.
func NewSessionHook(
	store *Store,
	sessions *session.Manager,
	cfg CollectionConfig,
	logger *slog.Logger,
) *SessionHook {
	if logger == nil {
		logger = slog.Default()
	}
	h := &SessionHook{
		store:    store,
		sessions: sessions,
		cfg:      cfg,
		logger:   logger,
	}
	h.unsub = sessions.EventBusRef().Subscribe(h.handleEvent)
	return h
}

// Stop unsubscribes from the event bus.
func (h *SessionHook) Stop() {
	if h.unsub != nil {
		h.unsub()
		h.unsub = nil
	}
}

func (h *SessionHook) handleEvent(event session.Event) {
	if event.Kind != session.EventStatusChanged {
		return
	}
	if event.NewStatus != session.StatusDone {
		return
	}
	h.collectSession(event.Key)
}

func (h *SessionHook) collectSession(sessionKey string) {
	sess := h.sessions.Get(sessionKey)
	if sess == nil {
		return
	}
	if sess.Kind != session.KindDirect {
		return
	}
	if sess.LastOutput == "" {
		return
	}

	h.store.Add(Trajectory{
		ID:          sessionKey,
		TaskType:    "session",
		Response:    sess.LastOutput,
		UserMessage: "",
	})

	h.logger.Debug("rl: collected session trajectory", "session", sessionKey)
}

// CollectFromAgentResult is called directly from the chat pipeline when
// the full AgentResult is available (richer data than the session event).
func (h *SessionHook) CollectFromAgentResult(sessionKey string, result *agent.AgentResult, userMessage string) {
	if result == nil {
		return
	}
	if result.Turns < h.cfg.MinTurns {
		return
	}
	if len(result.ToolActivities) < h.cfg.MinToolCalls {
		return
	}

	h.store.Add(Trajectory{
		ID:          sessionKey,
		TaskType:    "session",
		UserMessage: userMessage,
		Response:    result.AllText,
		Metadata: map[string]any{
			"turns":      result.Turns,
			"tool_count": len(result.ToolActivities),
		},
	})

	h.logger.Debug("rl: collected trajectory from agent result",
		"session", sessionKey,
		"turns", result.Turns,
		"tools", len(result.ToolActivities),
	)
}
