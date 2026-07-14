package chat

// broadcastPayload fans a typed event body out when a broadcaster is wired.
// Payload stays `any` at the chat/toolport layer so pipeline code does not
// import runtime/events (layering / Health Bench upward-import rule). The
// server adapter converts to events.EventPayload at the composition root.
func broadcastPayload(fn BroadcastFunc, event string, payload any) (int, []error) {
	if fn == nil {
		return 0, nil
	}
	return fn(event, payload)
}
