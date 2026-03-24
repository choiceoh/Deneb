package chat

import (
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// HandleBridgeEvent processes an event frame received from the Node.js bridge.
// Chat streaming events are relayed to WS clients via the broadcast function.
// Non-chat events are passed through to broadcastRaw if set.
func (h *Handler) HandleBridgeEvent(ev *protocol.EventFrame) {
	switch ev.Event {
	case "chat":
		h.handleChatStateEvent(ev)
	case "chat.delta":
		h.handleChatDelta(ev)
	default:
		if h.broadcastRaw != nil {
			h.broadcastRaw(ev.Event, mustMarshal(ev))
		}
	}
}

// handleChatStateEvent relays chat state events (started, done, aborted, error)
// and cleans up abort entries when a run completes.
func (h *Handler) handleChatStateEvent(ev *protocol.EventFrame) {
	var payload struct {
		ClientRunID string `json:"clientRunId"`
		SessionKey  string `json:"sessionKey"`
		State       string `json:"state"`
	}
	if err := json.Unmarshal(ev.Payload, &payload); err == nil {
		switch payload.State {
		case "done", "error", "aborted":
			h.cleanupAbort(payload.ClientRunID)
		}
	}

	// Relay to all WS clients.
	if h.broadcastRaw != nil {
		h.broadcastRaw(ev.Event, mustMarshal(ev))
	}
}

// handleChatDelta relays streaming text deltas to WS clients.
func (h *Handler) handleChatDelta(ev *protocol.EventFrame) {
	// Relay delta to WS clients.
	if h.broadcastRaw != nil {
		h.broadcastRaw(ev.Event, mustMarshal(ev))
	}
}

// mustMarshal marshals v to JSON, returning empty object on error.
func mustMarshal(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return data
}
