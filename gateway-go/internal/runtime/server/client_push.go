// client_push.go — proactive push to the standalone native client.
//
// The native app keeps one long-lived SSE connection open (from its foreground
// daemon) to GET /api/v1/miniapp/events. When the gateway produces a proactive
// report for the user's 업무 topic (morning-letter, email-analysis — see
// proactive_relay.go), it fans a {title, body} frame out to every connected
// client, which raises a local notification immediately instead of waiting for
// the next heartbeat poll.
//
// Transport is SSE, not WebSocket: the flow is strictly server→client, the
// gateway already ships an SSE chat endpoint (same client-token auth, flush and
// keepalive pattern), and neither side carries a WebSocket dependency. A
// single-direction push is exactly what SSE is for.
//
// This is best-effort and pull-resilient: a client that is asleep (Android Doze)
// or disconnected simply misses the live frame and picks the report up from the
// client:main transcript the next time it opens. No delivery guarantee, no
// persistence here.
package server

import (
	"sync"
)

// clientPushEvent is one proactive notification fanned out to native clients.
type clientPushEvent struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// clientPushHub is a tiny in-memory pub/sub: native client SSE connections
// subscribe, proactive delivery publishes. Buffered per-subscriber channels
// drop frames for a slow/asleep consumer rather than blocking the publisher
// (the report is always also in the transcript, so a dropped live frame is not
// data loss).
type clientPushHub struct {
	mu   sync.Mutex
	subs map[int]chan clientPushEvent
	next int
}

func newClientPushHub() *clientPushHub {
	return &clientPushHub{subs: make(map[int]chan clientPushEvent)}
}

// subscribe registers a new consumer and returns its channel plus an unsubscribe
// func. The channel is buffered so a brief consumer stall doesn't block publish.
func (h *clientPushHub) subscribe() (<-chan clientPushEvent, func()) {
	ch := make(chan clientPushEvent, 8)
	h.mu.Lock()
	id := h.next
	h.next++
	h.subs[id] = ch
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if c, ok := h.subs[id]; ok {
			delete(h.subs, id)
			close(c)
		}
		h.mu.Unlock()
	}
}

// publish fans an event out to all current subscribers. Non-blocking: a full
// subscriber buffer drops the frame (see type doc). Snapshot-then-send under the
// lock is safe because channels are only closed by unsubscribe under the same
// lock, so a send here can never hit a closed channel.
func (h *clientPushHub) publish(ev clientPushEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs {
		select {
		case ch <- ev:
		default: // slow/asleep consumer — drop; report is still in the transcript
		}
	}
}

// subscriberCount reports how many native clients are currently connected.
// Used only for logging/diagnostics.
func (h *clientPushHub) subscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
