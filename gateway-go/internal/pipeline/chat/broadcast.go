package chat

import (
	"encoding/json"

)

// broadcastPayload fans a typed event body out when a broadcaster is wired.
// Payload stays `any` at the chat layer; we marshal to rawJSON here
// so pipeline code does not import runtime/events.
func broadcastPayload(fn BroadcastFunc, event string, payload any) (int, []error) {
	if fn == nil {
		return 0, nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, []error{err}
	}
	return fn(event, raw)
}
