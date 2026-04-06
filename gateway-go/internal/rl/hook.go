package rl

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

// SessionHook subscribes to session lifecycle events and collects
// completed sessions as training trajectories.
type SessionHook struct {
	store    *TrajectoryStore
	sessions *session.Manager
	cfg      CollectionConfig
	logger   *slog.Logger
	unsub    func()
}

// NewSessionHook creates and subscribes a hook to the session event bus.
func NewSessionHook(
	store *TrajectoryStore,
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

	// Only collect direct (user-initiated) sessions.
	if sess.Kind != session.KindDirect {
		return
	}

	// LastOutput contains the assistant's final response text.
	if sess.LastOutput == "" {
		return
	}

	h.store.Collect(&Trajectory{
		ID:          sessionKey,
		Response:    sess.LastOutput,
		Environment: "korean_quality",
	})

	h.logger.Debug("rl: collected trajectory", "session", sessionKey)
}

// CollectFromAgentResult is called directly from the chat pipeline when
// the full AgentResult is available (richer data than the session event).
// This is the preferred collection path — the EventBus hook is a fallback.
func (h *SessionHook) CollectFromAgentResult(sessionKey string, result *agent.AgentResult, userMessage string) {
	if result == nil {
		return
	}

	// Apply collection filters.
	if result.Turns < h.cfg.MinTurns {
		return
	}
	if len(result.ToolActivities) < h.cfg.MinToolCalls {
		return
	}

	// Build tool call records.
	var toolCalls []ToolCallRecord
	for _, ta := range result.ToolActivities {
		toolCalls = append(toolCalls, ToolCallRecord{
			Name:    ta.Name,
			Success: !ta.IsError,
		})
	}

	h.store.Collect(&Trajectory{
		ID:          sessionKey,
		Prompt:      userMessage,
		Response:    result.AllText,
		ToolCalls:   toolCalls,
		Turns:       result.Turns,
		Environment: "korean_quality",
	})

	h.logger.Debug("rl: collected trajectory from agent result",
		"session", sessionKey,
		"turns", result.Turns,
		"tools", len(result.ToolActivities),
	)
}
