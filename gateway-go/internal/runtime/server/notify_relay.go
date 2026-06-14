// notify_relay.go — Secondary monitoring: status snapshots, error mirror, self-health heartbeat.
//
// The gateway watches itself and surfaces two kinds of monitoring signal to
// connected native clients (via clientPushHub) plus the operator log:
//
//  1. Status snapshots — buildStatusReport formats "what is the gateway
//     doing right now?" as a Korean summary of running sessions for
//     connected native clients.
//
//  2. Error mirrors — automatic. The notifier registers a Broadcaster.Tap
//     and forwards user-impacting events (chat.delivery_failed,
//     chat.media_delivery_failed, chat.tool_failed,
//     chat.context_overflow_unrecoverable, chat.compaction_stuck) to connected
//     native clients and logs them at Error.
//
// A periodic heartbeat self-polls /health so a hung HTTP mux is caught even
// when the broadcast taps fall silent; hang alerts log at Error (not pushed
// every tick, to avoid spamming the client with liveness pings).
//
// Both push paths fan out asynchronously through a buffered channel + worker
// goroutine so the broadcast hot path is never blocked. Per-event-type
// debounce (30s) coalesces a noisy failure mode into summary-grade signals.
//
// (The Telegram secondary-chat that originally received these was retired with
// the bot; delivery now targets connected native clients.)
//
// This file holds the service core (construction, lifecycle, tap filter,
// debounce, delivery). The heartbeat lives in notify_heartbeat.go, activity
// tracking in notify_activity.go, and Korean formatting in notify_status.go.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// notifyEventQueueSize bounds the worker's inbound channel so a flood of
// broadcasts cannot grow unbounded memory. When the channel is full we
// drop the event and log a Warn — losing one mirror is preferable to OOM.
const notifyEventQueueSize = 32

// notifyDebounce is the minimum interval between two notifications of the
// same event name. Distinct event names are unaffected by each other's
// debounce timers.
const notifyDebounce = 30 * time.Second

// mirroredEvents enumerates the broadcast event names that the notifier
// mirrors to connected native clients. Limited to events that signal an actual
// user-observable problem (delivery dropped, mutation failed, context broken,
// compaction looping). Routine `sessions.changed` / `session.tool` traffic is
// excluded — that would drown the operator in noise.
var mirroredEvents = map[string]struct{}{
	"chat.delivery_failed":                {},
	"chat.media_delivery_failed":          {},
	"chat.tool_failed":                    {},
	"chat.context_overflow_unrecoverable": {},
	"chat.compaction_stuck":               {},
}

// notifyService composes the error-mirror, in-flight activity tracking, and
// self-health probing behaviors. Critical events (delivery failures, compaction
// stuck) are pushed to connected native clients via clientPushHub and logged at
// Error. The Telegram secondary-chat monitoring was retired with the bot.
//
// Lock hierarchy (acquire in this order; never reverse):
//
//	notifyService.debounceMu  →  (independent: per-mutex critical sections)
//	notifyService.activityMu  →  (independent)
//
// The two mutexes are never held simultaneously.
type notifyService struct {
	pushHub  *clientPushHub
	sessions *session.Manager
	logger   *slog.Logger

	// boundAddr returns the gateway's resolved listen address for the
	// self-poll probe (e.g. "127.0.0.1:18789"). Returns "" before the
	// HTTP listener has bound; the heartbeat skips self-poll in that
	// case. Function-typed (not a value) so notifyService can be
	// constructed before the listener starts.
	boundAddr func() string

	// httpClient is the dedicated client for self-poll. Has its own
	// short timeout independent of any per-request context so a hung
	// roundtripper can't outlive the heartbeat tick.
	httpClient *http.Client

	queue chan notifyEvent

	debounceMu sync.Mutex
	lastSent   map[string]time.Time

	// In-flight activity tracking, populated from `agent` and
	// `session.tool` broadcast taps. Lets buildStatusReport answer
	// "what tool is the main session running RIGHT NOW" instead of
	// only "what was the last assistant text".
	activityMu sync.Mutex
	activity   map[string]*activityEntry
}

// notifyEvent is the worker's inbound message envelope.
type notifyEvent struct {
	name    string
	payload any
}

// newNotifyService builds the service. Returns nil only when sessions is nil
// (nothing useful to monitor). boundAddr is invoked on each heartbeat self-poll.
func newNotifyService(sessions *session.Manager, logger *slog.Logger, pushHub *clientPushHub, boundAddr func() string) *notifyService {
	if sessions == nil {
		return nil
	}
	return &notifyService{
		pushHub:   pushHub,
		sessions:  sessions,
		logger:    logger,
		boundAddr: boundAddr,
		httpClient: &http.Client{
			Timeout: selfPollTimeout,
		},
		queue:    make(chan notifyEvent, notifyEventQueueSize),
		lastSent: make(map[string]time.Time),
		activity: make(map[string]*activityEntry),
	}
}

// subscribeSessionEvents wires the activity cache to the session manager's
// event bus so terminal transitions (DONE/FAILED/KILLED/TIMEOUT) clear
// stale entries. Without this, a tool.start without a paired tool.end
// (panic, kill, abort) leaves the activity entry "running" until LRU
// eviction — which would lie to the operator about the session's state.
//
// The subscribe runs in its own goroutine inside the EventBus; cleanup
// only fires for terminal transitions, so non-terminal noise (CREATED,
// running→running) costs ~1 nanosecond per event.
func (n *notifyService) subscribeSessionEvents() {
	if n.sessions == nil {
		return
	}
	n.sessions.EventBusRef().Subscribe(func(e session.Event) {
		if e.Kind != session.EventStatusChanged && e.Kind != session.EventDeleted {
			return
		}
		// Terminal: DONE / FAILED / KILLED / TIMEOUT — anything that
		// isn't running anymore. Also clear on Deleted (GC) so a
		// re-created session under the same key starts clean.
		if e.Kind == session.EventStatusChanged && !session.IsTerminal(e.NewStatus) {
			return
		}
		n.clearActivity(e.Key)
	})
}

// clearActivity removes the activity entry for a session key. Safe to
// call when the entry doesn't exist — used by the lifecycle subscriber
// without nil-checking.
func (n *notifyService) clearActivity(sessionKey string) {
	n.activityMu.Lock()
	delete(n.activity, sessionKey)
	n.activityMu.Unlock()
}

// start spawns the worker goroutine and the heartbeat ticker. Both exit
// when ctx is cancelled (typically server shutdown). Idempotent: caller
// drives lifecycle, so passing a never-cancelled context simply leaks the
// goroutines until process exit, which is acceptable for the gateway's
// single-binary deployment.
func (n *notifyService) start(ctx context.Context) {
	n.subscribeSessionEvents()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				n.logger.Error("panic in notify worker", "panic", r)
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-n.queue:
				if !ok {
					return
				}
				n.deliver(ctx, ev)
			}
		}
	}()
	go n.runHeartbeat(ctx)
}

// tap is the broadcaster Tap callback for *mirroring* events. It first
// records activity for in-flight introspection (agent/session.tool), then
// filters to the monitored error set + debounce + enqueue.
//
// Activity recording runs even when the event isn't mirrored, so the
// status snapshot still has fresh data on routine traffic.
func (n *notifyService) tap(event string, payload any) {
	n.recordActivity(event, payload)

	if _, want := mirroredEvents[event]; !want {
		return
	}
	if !n.checkDebounce(event) {
		return
	}
	select {
	case n.queue <- notifyEvent{name: event, payload: payload}:
		n.markSent(event)
	default:
		n.logger.Warn("notify queue full, dropping event", "event", event)
	}
}

// checkDebounce returns true when at least notifyDebounce has elapsed since
// the last *successful* send for the given event name. Does not update the
// timestamp — caller must call markSent on successful enqueue. Splitting the
// read from the write means a queue-full drop no longer poisons the next
// 30s of debounce.
func (n *notifyService) checkDebounce(event string) bool {
	n.debounceMu.Lock()
	defer n.debounceMu.Unlock()
	if last, ok := n.lastSent[event]; ok && time.Since(last) < notifyDebounce {
		return false
	}
	return true
}

// markSent records that an event was enqueued. Called only after the worker
// queue accepted it so transient queue-full drops don't suppress later
// genuine sends. Race window: two concurrent taps may both pass
// checkDebounce and both enqueue — acceptable; we'll send at most twice
// before the timestamp settles.
func (n *notifyService) markSent(event string) {
	n.debounceMu.Lock()
	n.lastSent[event] = time.Now()
	n.debounceMu.Unlock()
}

// deliver formats the event and delivers it: heartbeats/slog events go to the
// logger (Error for hang alerts, Info otherwise); user-impacting broadcast
// events are pushed to connected native clients via pushHub and logged at Error.
func (n *notifyService) deliver(_ context.Context, ev notifyEvent) {
	switch ev.name {
	case "_heartbeat":
		body, _ := ev.payload.(string)
		if body == "" {
			return
		}
		// Hang alerts prefix with 🚨 — log at Error so they surface in
		// journald monitoring without spamming the native client with
		// every 5-min liveness ping.
		if strings.HasPrefix(body, "🚨") {
			n.logger.Error("gateway health alert", "body", body)
		} else {
			n.logger.Info("gateway heartbeat", "body", body)
		}
		return
	case "_slog":
		body, _ := ev.payload.(string)
		if body != "" {
			n.logger.Error("notify slog forwarded", "body", body)
		}
		return
	}

	body := formatErrorEvent(ev.name, ev.payload)
	if body == "" {
		return
	}
	// Log the error and push a preview to connected native clients.
	n.logger.Error("gateway error event", "event", ev.name, "body", body)
	if n.pushHub != nil {
		n.pushHub.publish(clientPushEvent{
			Title: "⚠️ Deneb 오류",
			Body:  truncate(body, 120),
		})
	}
}
