package server

import (
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
)

// eventPayloadFromAny converts pipeline/domain broadcast payloads into the
// typed events.EventPayload used by the runtime broadcaster.
func eventPayloadFromAny(payload any) events.EventPayload {
	switch v := payload.(type) {
	case nil:
		return events.EventPayload{}
	case events.EventPayload:
		return v
	case json.RawMessage:
		return events.PayloadFromRaw(v)
	case []byte:
		return events.PayloadFromRaw(v)
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return events.EventPayload{}
		}
		return events.PayloadFromRaw(raw)
	}
}
