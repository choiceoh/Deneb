// Package nativepush owns best-effort native-client push fan-out.
//
// The native app keeps one long-lived SSE connection open (from its foreground
// daemon) to GET /api/v1/miniapp/events. Gateway producers publish {title, body}
// frames here so the app can raise a local notification immediately instead of
// waiting for the next heartbeat poll.
//
// Transport is SSE, not WebSocket: the flow is strictly server->client, the
// gateway already ships an SSE chat endpoint (same client-token auth, flush and
// keepalive pattern), and neither side carries a WebSocket dependency. A
// single-direction push is exactly what SSE is for.
//
// This is best-effort and pull-resilient: a client that is asleep (Android Doze)
// or disconnected simply misses the live frame and picks the report up from the
// durable transcript/work-feed path the next time it opens. No delivery
// guarantee, no persistence here.
package nativepush

import (
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/push"
)

// Event is one proactive frame delivered to native clients.
type Event struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// Optional deep-link target for the desktop proactive panel: Kind is a pane
	// key and Ref the resource id (or wiki path) the nudge opens. Empty when the
	// event has no navigable target (informational — e.g. an error or image push).
	Kind string `json:"kind,omitempty"`
	Ref  string `json:"ref,omitempty"`
	// Data carries a structured command for non-notification frames — e.g. a
	// phone action (Kind=PushKindPhoneAction) the app executes as an Intent
	// rather than rendering as a notification.
	Data map[string]string `json:"data,omitempty"`
}

// Push deep-link kinds — these strings MUST match the andromeda View pane keys
// (andromeda/src/types.ts) so the desktop ProactivePanel can route a nudge to a
// pane. Add a kind only when the publish site has a real target for it.
const (
	// PushKindWorkfeed opens a work-feed item from a native push.
	PushKindWorkfeed = "workfeed"
	// PushKindFleet opens the fleet pane from a native push.
	PushKindFleet = "fleet"
	// PushKindPhoneAction identifies a native phone command frame.
	PushKindPhoneAction = "phone_action"
	// PushKindWorkspace identifies a desktop workstation-control frame.
	PushKindWorkspace = "workspace"
	// PushKindComputer identifies a desktop computer-use command frame (the
	// computer tool: screenshot/click/type on the host OS). Ref carries the
	// dispatch id the desktop echoes back on miniapp.computer.result.
	PushKindComputer = "computer"
)

// ClientKind identifies the device class behind an events subscription.
type ClientKind int

const (
	// KindUnknown is the safe default for an unidentified client.
	KindUnknown ClientKind = iota
	// KindMobile identifies the Android native client.
	KindMobile
	// KindDesktop identifies the desktop client.
	KindDesktop
)

// ClientKindFromHeader maps the native client-kind header to a device class.
func ClientKindFromHeader(v string) ClientKind {
	switch v {
	case "mobile":
		return KindMobile
	case "desktop":
		return KindDesktop
	default:
		return KindUnknown
	}
}

type pushSub struct {
	ch   chan Event
	kind ClientKind
}

// Hub fans proactive events out to connected native clients.
type Hub struct {
	mu   sync.Mutex
	subs map[int]pushSub
	next int
}

// NewHub constructs an empty native-client push hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[int]pushSub)}
}

// Subscribe registers a native client until the returned function is called.
func (h *Hub) Subscribe(kind ClientKind) (<-chan Event, func()) {
	ch := make(chan Event, 8)
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

// Publish fans an event out without blocking on slow subscribers.
func (h *Hub) Publish(ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.subs {
		select {
		case s.ch <- ev:
		default: // slow/asleep consumer — drop; report is still in durable state
		}
	}
}

// SubscriberCount reports all connected native clients.
func (h *Hub) SubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// MobileSubscriberCount reports connected Android clients.
func (h *Hub) MobileSubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, s := range h.subs {
		if s.kind == KindMobile {
			n++
		}
	}
	return n
}

// DesktopSubscriberCount reports connected desktop (Andromeda) clients.
func (h *Hub) DesktopSubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, s := range h.subs {
		if s.kind == KindDesktop {
			n++
		}
	}
	return n
}

// PublishWithFallback fans ev out to all connected SSE clients and, when no
// mobile client holds a live connection, also delivers {Title, Body} via FCM so
// a backgrounded or fully-closed phone still raises the notification. fcm may be
// nil (FCM not configured) — then it is SSE-only.
func PublishWithFallback(hub *Hub, fcm *push.Notifier, ev Event) {
	if hub == nil {
		return
	}
	ev = FormatHUDPush(ev)
	hub.Publish(ev)
	if fcm != nil && hub.MobileSubscriberCount() == 0 {
		fcm.DeliverFallback(ev.Title, ev.Body)
	}
}
