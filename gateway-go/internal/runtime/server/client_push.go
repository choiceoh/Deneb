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

	"github.com/choiceoh/deneb/gateway-go/internal/domain/push"
)

// clientPushEvent is one proactive notification fanned out to native clients.
type clientPushEvent struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// Optional deep-link target for the desktop proactive panel: Kind is a pane
	// key and Ref the resource id (or wiki path) the nudge opens. Empty when the
	// event has no navigable target (informational — e.g. an error or image push).
	Kind string `json:"kind,omitempty"`
	Ref  string `json:"ref,omitempty"`
	// Data carries a structured command for non-notification frames — e.g. a
	// phone action (Kind=pushKindPhoneAction) the app executes as an Intent
	// rather than rendering as a notification.
	Data map[string]string `json:"data,omitempty"`
}

// Push deep-link kinds — these strings MUST match the andromeda View pane keys
// (andromeda/src/types.ts) so the desktop ProactivePanel can route a nudge to a
// pane. Add a kind only when the publish site has a real target for it.
const (
	pushKindWorkfeed = "workfeed"
	pushKindFleet    = "fleet"
	// pushKindPhoneAction marks a frame the app executes as an Android Intent
	// (open_url/share/…) rather than rendering as a notification — Data carries
	// the action + args.
	pushKindPhoneAction = "phone_action"
)

// clientKind identifies the device class behind an /events subscription so the
// FCM fallback can fire on "no MOBILE client connected" rather than "no client
// at all" — a connected desktop (Andromeda) must not suppress the phone's push.
// Unknown (no X-Deneb-Client-Kind header) is treated as non-mobile: it never
// suppresses the phone fallback, the safe default for an unidentified client.
type clientKind int

const (
	kindUnknown clientKind = iota
	kindMobile
	kindDesktop
)

// clientKindFromHeader maps the X-Deneb-Client-Kind request header to a kind.
func clientKindFromHeader(v string) clientKind {
	switch v {
	case "mobile":
		return kindMobile
	case "desktop":
		return kindDesktop
	default:
		return kindUnknown
	}
}

// clientPushHub is a tiny in-memory pub/sub: native client SSE connections
// subscribe, proactive delivery publishes. Buffered per-subscriber channels
// drop frames for a slow/asleep consumer rather than blocking the publisher
// (the report is always also in the transcript, so a dropped live frame is not
// data loss).
type pushSub struct {
	ch   chan clientPushEvent
	kind clientKind
}

type clientPushHub struct {
	mu   sync.Mutex
	subs map[int]pushSub
	next int
}

func newClientPushHub() *clientPushHub {
	return &clientPushHub{subs: make(map[int]pushSub)}
}

// subscribe registers a new consumer of the given device kind and returns its
// channel plus an unsubscribe func. The channel is buffered so a brief consumer
// stall doesn't block publish.
func (h *clientPushHub) subscribe(kind clientKind) (<-chan clientPushEvent, func()) {
	ch := make(chan clientPushEvent, 8)
	h.mu.Lock()
	id := h.next
	h.next++
	h.subs[id] = pushSub{ch: ch, kind: kind}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if s, ok := h.subs[id]; ok {
			delete(h.subs, id)
			close(s.ch)
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
	for _, s := range h.subs {
		select {
		case s.ch <- ev:
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

// mobileSubscriberCount reports how many connected clients identified as mobile
// (the Android app). The FCM fallback keys on THIS, not subscriberCount: a
// backgrounded phone that has dropped its SSE must still get the push even when
// a desktop client is connected, so a non-zero desktop count must not suppress
// it (see publishProactive).
func (h *clientPushHub) mobileSubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, s := range h.subs {
		if s.kind == kindMobile {
			n++
		}
	}
	return n
}

// publishProactive fans ev out to all connected SSE clients and, when no MOBILE
// client holds a live connection, also delivers {Title, Body} via FCM so a
// backgrounded or fully-closed phone still raises the notification. fcm may be
// nil (FCM not configured) — then it is SSE-only. Centralizes the delivery
// predicate so every user-facing push (proactive reports, weekly-report images,
// gateway error events, fleet alerts) reaches a phone whose SSE is down.
func publishProactive(hub *clientPushHub, fcm *push.Notifier, ev clientPushEvent) {
	if hub == nil {
		return
	}
	hub.publish(ev)
	if fcm != nil && hub.mobileSubscriberCount() == 0 {
		fcm.DeliverFallback(ev.Title, ev.Body)
	}
}
