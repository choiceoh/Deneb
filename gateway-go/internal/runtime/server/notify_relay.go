// notify_relay.go — Secondary-chat monitoring: status snapshots + error mirror.
//
// When telegram.notificationChatID is configured (and differs from the main
// chatID), the gateway routes two kinds of monitoring traffic to that chat:
//
//  1. Status snapshots — on demand via the telegram.notify_status RPC. The
//     caller asks "what is the main session doing right now?" and we emit a
//     Korean summary of running sessions to the monitoring chat.
//
//  2. Error mirrors — automatic. The notifier registers a Broadcaster.Tap
//     and forwards user-impacting events (chat.delivery_failed,
//     chat.media_delivery_failed, chat.context_overflow_unrecoverable,
//     chat.compaction_stuck) to the monitoring chat.
//
// Both paths fan out asynchronously through a buffered channel + worker
// goroutine so the broadcast hot path is never blocked on Telegram HTTP.
//
// Per-event-type debounce (30s) keeps a noisy failure mode from spamming
// the monitoring chat. The monitoring chat is meant for summary-grade
// signals; high-frequency repeats are coalesced.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
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

// monitoredEvents enumerates the broadcast event names that the notifier
// mirrors to the secondary chat. Limited to events that signal an actual
// user-observable problem (delivery dropped, context broken, compaction
// looping). Routine `sessions.changed` / `session.tool` traffic is
// excluded — that would drown the operator in noise.
var monitoredEvents = map[string]struct{}{
	"chat.delivery_failed":                {},
	"chat.media_delivery_failed":          {},
	"chat.context_overflow_unrecoverable": {},
	"chat.compaction_stuck":               {},
}

// notifyService composes the status-snapshot and error-mirror behaviors
// against a single Telegram plugin. Constructed once during early
// registration; lifecycle bound to the server's ShutdownCtx.
type notifyService struct {
	plugin   *telegram.Plugin
	sessions *session.Manager
	logger   *slog.Logger

	queue chan notifyEvent

	debounceMu sync.Mutex
	lastSent   map[string]time.Time
}

// notifyEvent is the worker's inbound message envelope.
type notifyEvent struct {
	name    string
	payload any
}

// newNotifyService builds the service. Returns nil when the plugin has no
// configured notification chat ID — disables monitoring entirely without
// allocating a worker goroutine. Callers must nil-check before use.
func newNotifyService(plug *telegram.Plugin, sessions *session.Manager, logger *slog.Logger) *notifyService {
	if plug == nil || plug.NotificationChatID() == 0 {
		return nil
	}
	return &notifyService{
		plugin:   plug,
		sessions: sessions,
		logger:   logger,
		queue:    make(chan notifyEvent, notifyEventQueueSize),
		lastSent: make(map[string]time.Time),
	}
}

// start spawns the worker goroutine. The worker exits when ctx is cancelled
// (typically server shutdown). Idempotent: caller drives lifecycle, so
// passing a never-cancelled context simply leaks the goroutine until process
// exit, which is acceptable for the gateway's single-binary deployment.
func (n *notifyService) start(ctx context.Context) {
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
}

// tap is a Broadcaster.Tap callback. Filters events to the monitored set,
// applies debounce, then enqueues for async delivery. Non-blocking: if the
// queue is full, drops the event with a Warn log.
func (n *notifyService) tap(event string, payload any) {
	if _, want := monitoredEvents[event]; !want {
		return
	}
	if !n.shouldSend(event) {
		return
	}
	select {
	case n.queue <- notifyEvent{name: event, payload: payload}:
	default:
		n.logger.Warn("notify queue full, dropping event", "event", event)
	}
}

// shouldSend returns true when at least notifyDebounce has elapsed since the
// last send for the given event name. Updates the last-sent timestamp on
// success so the caller can rely on a single check per event.
func (n *notifyService) shouldSend(event string) bool {
	now := time.Now()
	n.debounceMu.Lock()
	defer n.debounceMu.Unlock()
	if last, ok := n.lastSent[event]; ok && now.Sub(last) < notifyDebounce {
		return false
	}
	n.lastSent[event] = now
	return true
}

// deliver formats the event into Korean prose and sends it via the plugin.
// Failures are logged at Error per logging.md (this IS the user-monitoring
// surface — its own failures are real). Bounded sub-timeout (10s) keeps a
// stuck Telegram API from blocking the worker's queue indefinitely.
func (n *notifyService) deliver(ctx context.Context, ev notifyEvent) {
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	body := formatErrorEvent(ev.name, ev.payload)
	if body == "" {
		return
	}
	sent, err := n.plugin.SendNotification(sendCtx, body)
	if err != nil {
		n.logger.Error("notify send failed", "event", ev.name, "error", err)
		return
	}
	if !sent {
		// Plugin reported "monitoring disabled" — config likely changed
		// mid-run. Log Debug; the next event will hit the same branch
		// until reconfigured.
		n.logger.Debug("notify skipped (monitoring disabled)", "event", ev.name)
	}
}

// notifyStatusSnapshotFunc returns a closure suitable for
// handlertelegram.NotifyDeps.SendStatusSnapshot. Returns nil when n is nil,
// so NotifyMethods declines to register telegram.notify_status entirely
// when monitoring is disabled.
func notifyStatusSnapshotFunc(n *notifyService) func(context.Context) (bool, error) {
	if n == nil {
		return nil
	}
	return n.SendStatusSnapshot
}

// SendStatusSnapshot composes a Korean status report and pushes it to the
// monitoring chat. Returns (delivered=true, nil) on success; (false, nil)
// when monitoring is disabled (no chat ID, plugin not running). Errors
// surface real send failures so the RPC caller sees them.
func (n *notifyService) SendStatusSnapshot(ctx context.Context) (bool, error) {
	body := n.buildStatusReport(time.Now())
	sent, err := n.plugin.SendNotification(ctx, body)
	if err != nil {
		return false, err
	}
	return sent, nil
}

// buildStatusReport formats the current session manager state as a Korean
// summary. Public-shaped (lowercase b on the function but unexported pkg)
// so unit tests can assert formatting without spinning up a Telegram client.
func (n *notifyService) buildStatusReport(now time.Time) string {
	if n.sessions == nil {
		return "📡 게이트웨이 상태\n세션 매니저 미초기화."
	}
	all := n.sessions.List()
	running := make([]*session.Session, 0, len(all))
	for _, s := range all {
		if s == nil {
			continue
		}
		if s.Status == session.StatusRunning {
			running = append(running, s)
		}
	}

	var b strings.Builder
	b.WriteString("📡 게이트웨이 상태 — ")
	b.WriteString(now.Format("2006-01-02 15:04:05"))
	b.WriteString("\n")
	if len(running) == 0 {
		b.WriteString("실행 중인 세션 없음. (대기 상태)")
		return b.String()
	}

	// Newest session first — most recently active is most likely what the
	// user wants to see.
	sort.SliceStable(running, func(i, j int) bool {
		return running[i].UpdatedAt > running[j].UpdatedAt
	})

	fmt.Fprintf(&b, "활성 세션 %d개:\n", len(running))
	for _, s := range running {
		label := s.Label
		if label == "" {
			label = "(라벨 없음)"
		}
		started := ""
		if s.StartedAt != nil {
			elapsed := now.Sub(time.UnixMilli(*s.StartedAt))
			started = fmt.Sprintf(", %s 경과", humanDuration(elapsed))
		}
		fmt.Fprintf(&b, "• %s — %s%s\n", s.Key, label, started)
		if s.LastOutput != "" {
			fmt.Fprintf(&b, "  최근 응답: %s\n", truncate(s.LastOutput, 120))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatErrorEvent renders a monitored broadcast event as a Korean alert
// line. Returns "" when the event isn't recognized — defensive guard for
// the tap filter (which already excludes unknowns).
func formatErrorEvent(event string, payload any) string {
	fields, _ := payload.(map[string]any)

	headline := errorHeadlineKO(event)
	if headline == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("⚠️ ")
	b.WriteString(headline)
	if sess := stringField(fields, "session"); sess != "" {
		fmt.Fprintf(&b, "\n세션: %s", sess)
	}
	if reason := stringField(fields, "reason"); reason != "" {
		fmt.Fprintf(&b, "\n원인: %s", reason)
	}
	if errMsg := stringField(fields, "error"); errMsg != "" {
		fmt.Fprintf(&b, "\n에러: %s", truncate(errMsg, 200))
	}
	return b.String()
}

// errorHeadlineKO maps the broadcast event name to a Korean headline. Kept
// alongside monitoredEvents so adding a new monitored event requires both
// the filter and the headline to be updated together.
func errorHeadlineKO(event string) string {
	switch event {
	case "chat.delivery_failed":
		return "채팅 응답 전달 실패"
	case "chat.media_delivery_failed":
		return "미디어 전달 실패"
	case "chat.context_overflow_unrecoverable":
		return "컨텍스트 오버플로 (복구 불가)"
	case "chat.compaction_stuck":
		return "컨텍스트 압축 중단"
	default:
		return ""
	}
}

// stringField returns the field value as a string, or "" when missing.
// Tolerates nil maps so the caller need not guard.
func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// truncate clamps s to maxRunes runes (not bytes) and appends ellipsis.
// Korean text is multi-byte; rune count keeps the cap visually predictable.
func truncate(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

// humanDuration formats a duration as Korean shorthand: "30초", "5분",
// "2시간 13분". Coarse on purpose — the monitoring chat shows snapshots,
// not millisecond-grade telemetry.
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d초", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d분", int(d.Minutes()))
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) - hours*60
	if mins == 0 {
		return fmt.Sprintf("%d시간", hours)
	}
	return fmt.Sprintf("%d시간 %d분", hours, mins)
}
