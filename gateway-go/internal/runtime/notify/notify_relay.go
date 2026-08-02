// notify_relay.go — Secondary monitoring: status snapshots, error mirror, self-health heartbeat.
//
// The gateway watches itself and surfaces two kinds of monitoring signal to
// connected native clients (via nativepush.Hub) plus the operator log:
//
//  1. Error mirrors — automatic. The notifier registers a Broadcaster.Tap
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
// debounce, delivery). The heartbeat lives in notify_heartbeat.go and Korean
// formatting in notify_status.go.
package notify

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
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

// Service composes the error-mirror, in-flight activity tracking, and
// self-health probing behaviors. Critical events (delivery failures, compaction
// stuck) are pushed to connected native clients via nativepush.Hub and logged at
// Error. The Telegram secondary-chat monitoring was retired with the bot.
//
// Lock hierarchy (acquire in this order; never reverse):
//
//	notifyService.debounceMu  →  (independent: per-mutex critical sections)
//	notifyService.activityMu  →  (independent)
//
// The two mutexes are never held simultaneously.
type Service struct {
	// push mirrors user-impacting errors to the native delivery layer. Keeping
	// transport behind a callback makes monitoring independent of the HTTP/SSE
	// server while preserving its live-push plus FCM fallback policy.
	push     func(title, body string)
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

	// Dependency (sidecar) probes woven into each heartbeat — see
	// notify_deps.go. depMu guards all three fields; late-bound via
	// SetDependencyChecks because dependency handles are built after the
	// notify service (Session phase vs Early phase). depDown maps a dep
	// name to when its outage was first observed (zero/absent = healthy);
	// depStateFile persists that map across restarts so a standing outage
	// does not re-alert on every deploy.
	depMu        sync.Mutex
	depChecks    []DepCheck
	depDown      map[string]time.Time
	depStateFile string
}

// notifyEvent is the worker's inbound message envelope.
type notifyEvent struct {
	name    string
	payload any
}

// NewService builds the service. Returns nil only when sessions is nil
// (nothing useful to monitor). boundAddr is invoked on each heartbeat self-poll.
func NewService(sessions *session.Manager, logger *slog.Logger, push func(title, body string), boundAddr func() string) *Service {
	if sessions == nil {
		return nil
	}
	return &Service{
		push:      push,
		sessions:  sessions,
		logger:    logger,
		boundAddr: boundAddr,
		httpClient: &http.Client{
			Timeout: selfPollTimeout,
		},
		queue:    make(chan notifyEvent, notifyEventQueueSize),
		lastSent: make(map[string]time.Time),
	}
}

// start spawns the worker goroutine and the heartbeat ticker. Both exit
// when ctx is cancelled (typically server shutdown). Idempotent: caller
// drives lifecycle, so passing a never-cancelled context simply leaks the
// goroutines until process exit, which is acceptable for the gateway's
// single-binary deployment.
func (n *Service) Start(ctx context.Context) {
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
// filters to the monitored error set + debounce + enqueue.
func (n *Service) Tap(event string, payload events.EventPayload) {
	if _, want := mirroredEvents[event]; !want {
		return
	}
	if !n.checkDebounce(event) {
		return
	}
	select {
	case n.queue <- notifyEvent{name: event, payload: payload.Bytes()}:
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
func (n *Service) checkDebounce(event string) bool {
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
func (n *Service) markSent(event string) {
	n.debounceMu.Lock()
	n.lastSent[event] = time.Now()
	n.debounceMu.Unlock()
}

// deliver formats the event and delivers it: heartbeats/slog events go to the
// logger (Error for hang alerts, Info otherwise); user-impacting broadcast
// events are pushed to connected native clients via pushHub and logged at Error.
func (n *Service) deliver(_ context.Context, ev notifyEvent) {
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

	// Dependency transition alerts ("_dep_<name>", see notify_deps.go): a
	// sidecar going down is operator-actionable NOW, so unlike the quiet
	// heartbeat these push to the native client. Recovery logs at Info and
	// pushes too — it closes the loop the down-alert opened.
	if strings.HasPrefix(ev.name, "_dep_") {
		body, _ := ev.payload.(string)
		if body == "" {
			return
		}
		if strings.HasPrefix(body, "🔌") {
			n.logger.Error("sidecar health alert", "body", body)
		} else {
			n.logger.Info("sidecar health recovered", "body", body)
		}
		if n.push != nil {
			n.push("🔌 사이드카 상태", truncate(body, 120))
		}
		return
	}

	body := formatErrorEvent(ev.name, ev.payload)
	if body == "" {
		return
	}
	// Log the error and push a preview to connected native clients, with an FCM
	// fallback so a backgrounded/closed phone still sees user-impacting failures.
	n.logger.Error("gateway error event", "event", ev.name, "body", body)
	if n.push != nil {
		n.push("⚠️ Deneb 오류", truncate(body, 120))
	}
}
