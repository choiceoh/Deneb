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
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
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

// heartbeatInterval drives the periodic liveness ping to the monitoring
// chat. Five minutes is a compromise: short enough that an operator
// notices a hung gateway within a coffee break, long enough that it
// doesn't drown the chat. The first beat skips a fixed warmup window
// after start so boot-time activity has time to settle.
const heartbeatInterval = 5 * time.Minute

// heartbeatWarmup suppresses the heartbeat for this long after start so
// the operator doesn't see a blast of "alive" pings during boot, where
// every subsystem is logging its own initialization noise.
const heartbeatWarmup = 30 * time.Second

// mirroredEvents enumerates the broadcast event names that the notifier
// mirrors to the secondary chat. Limited to events that signal an actual
// user-observable problem (delivery dropped, context broken, compaction
// looping). Routine `sessions.changed` / `session.tool` traffic is
// excluded — that would drown the operator in noise.
var mirroredEvents = map[string]struct{}{
	"chat.delivery_failed":                {},
	"chat.media_delivery_failed":          {},
	"chat.context_overflow_unrecoverable": {},
	"chat.compaction_stuck":               {},
}

// activityMaxSessions caps the per-session activity cache to keep memory
// bounded across long uptimes. When exceeded, the oldest entries are
// evicted on insert. 64 sessions is well above the realistic working set
// for a single-user deployment.
const activityMaxSessions = 64

// activityEntry is the snapshot of in-flight tool activity for one session,
// updated whenever an `agent` (tool.start/tool.end/run.start/run.end) or
// `session.tool` event fires for that session.
type activityEntry struct {
	tool    string    // tool name from the most recent tool.start
	running bool      // true between tool.start and tool.end / run.end
	isError bool      // last tool's error flag (post-result)
	updated time.Time // wall-clock time of the last update
	runID   string    // current run identifier; stable across a single turn
}

// notifyService composes the status-snapshot, error-mirror, in-flight
// activity tracking, and operator log forwarding behaviors against a
// single Telegram plugin. Constructed once during early registration;
// lifecycle bound to the server's ShutdownCtx.
//
// Lock hierarchy (acquire in this order; never reverse):
//
//	notifyService.debounceMu  →  (independent: per-mutex critical sections)
//	notifyService.activityMu  →  (independent)
//
// The two mutexes are never held simultaneously.
type notifyService struct {
	plugin   *telegram.Plugin
	sessions *session.Manager
	logger   *slog.Logger

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
		activity: make(map[string]*activityEntry),
	}
}

// start spawns the worker goroutine and the heartbeat ticker. Both exit
// when ctx is cancelled (typically server shutdown). Idempotent: caller
// drives lifecycle, so passing a never-cancelled context simply leaks the
// goroutines until process exit, which is acceptable for the gateway's
// single-binary deployment.
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
	go n.runHeartbeat(ctx)
}

// runHeartbeat fires a liveness ping every heartbeatInterval. The point
// is detection of "gateway is alive but the broadcast taps are silent
// because nothing's happening" vs "gateway is hung and even broadcasts
// stopped". Without this, an operator can't distinguish the two from the
// monitoring chat alone.
func (n *notifyService) runHeartbeat(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			n.logger.Error("panic in heartbeat loop", "panic", r)
		}
	}()
	// Warmup delay so boot-time noise doesn't trigger the first beat.
	select {
	case <-ctx.Done():
		return
	case <-time.After(heartbeatWarmup):
	}

	t := time.NewTicker(heartbeatInterval)
	defer t.Stop()

	startTime := time.Now()
	// Send one beat immediately after warmup so the operator gets a
	// "monitoring channel is wired" confirmation without waiting 5 min.
	n.enqueueHeartbeat(startTime)

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.enqueueHeartbeat(startTime)
		}
	}
}

// enqueueHeartbeat formats the current liveness summary and pushes it
// onto the worker queue. Drops silently on overflow — heartbeats are
// best-effort. Uses the same per-event debounce key so a manual force
// (future) can't double-fire within the interval.
func (n *notifyService) enqueueHeartbeat(startTime time.Time) {
	body := n.buildHeartbeatLine(startTime, time.Now())
	if !n.checkDebounce("_heartbeat") {
		return
	}
	select {
	case n.queue <- notifyEvent{name: "_heartbeat", payload: body}:
		n.markSent("_heartbeat")
	default:
		// Silent drop on overflow — sending another log here would loop
		// back through the slog forwarder.
	}
}

// buildHeartbeatLine composes the "I'm alive" message. Includes uptime,
// goroutine count, allocated memory, active session count. Numbers come
// from runtime stats; a single beat reads them once for consistency.
func (n *notifyService) buildHeartbeatLine(startTime, now time.Time) string {
	uptime := humanDuration(now.Sub(startTime))
	goroutines := runtime.NumGoroutine()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	allocMB := ms.Alloc / (1024 * 1024)

	sessionCount := 0
	if n.sessions != nil {
		sessionCount = n.sessions.Count()
	}

	return fmt.Sprintf(
		"💓 게이트웨이 정상 — uptime %s, 세션 %d, goroutine %d, mem %dMB",
		uptime, sessionCount, goroutines, allocMB,
	)
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

// recordActivity updates the in-flight activity cache from broadcast events.
// Three event sources contribute:
//
//   - "agent" — fallback path (no Publisher). Payload is events.AgentEvent
//     struct with Kind in {tool.start, tool.end, run.start, run.end}.
//   - "agent.event" — Publisher-mediated path. Payload is map[string]any
//     with the same fields flattened ({"kind": "...", "sessionKey": "...",
//     "payload": map{...}}).
//   - "session.tool" — fires AFTER a tool result; used to record the
//     final error flag.
//
// All other events are ignored. The function is cheap (single map write)
// and safe to call from the broadcast hot path.
func (n *notifyService) recordActivity(event string, payload any) {
	switch event {
	case "agent", "agent.event":
		n.recordAgentActivity(payload)
	case "session.tool":
		n.recordToolResult(payload)
	}
}

// agentEventFields normalises both AgentEvent struct and its publisher
// map[string]any rendering into the four fields the activity recorder
// needs. Returns ok=false when the payload doesn't carry an actionable
// session key.
func agentEventFields(payload any) (kind, sessionKey, runID string, sub any, ok bool) {
	switch v := payload.(type) {
	case events.AgentEvent:
		if v.SessionKey == "" {
			return "", "", "", nil, false
		}
		return v.Kind, v.SessionKey, v.RunID, v.Payload, true
	case map[string]any:
		sk := stringField(v, "sessionKey")
		if sk == "" {
			return "", "", "", nil, false
		}
		return stringField(v, "kind"), sk, stringField(v, "runId"), v["payload"], true
	default:
		return "", "", "", nil, false
	}
}

func (n *notifyService) recordAgentActivity(payload any) {
	kind, sessionKey, runID, sub, ok := agentEventFields(payload)
	if !ok {
		return
	}
	n.activityMu.Lock()
	defer n.activityMu.Unlock()
	n.evictIfOversizeLocked()
	entry := n.activity[sessionKey]
	if entry == nil {
		entry = &activityEntry{}
		n.activity[sessionKey] = entry
	}
	entry.updated = time.Now()
	entry.runID = runID
	switch kind {
	case "tool.start":
		entry.tool = stringFromAgentPayload(sub, "tool")
		entry.running = true
		entry.isError = false
	case "tool.end":
		entry.running = false
		if b, ok := boolFromAgentPayload(sub, "isError"); ok {
			entry.isError = b
		}
	case "run.start":
		entry.tool = ""
		entry.running = false
		entry.isError = false
	case "run.end":
		entry.running = false
	}
}

func (n *notifyService) recordToolResult(payload any) {
	fields, ok := payload.(map[string]any)
	if !ok {
		return
	}
	sessionKey := stringField(fields, "sessionKey")
	if sessionKey == "" {
		return
	}
	n.activityMu.Lock()
	defer n.activityMu.Unlock()
	n.evictIfOversizeLocked()
	entry := n.activity[sessionKey]
	if entry == nil {
		entry = &activityEntry{}
		n.activity[sessionKey] = entry
	}
	entry.tool = stringField(fields, "tool")
	entry.running = false
	if v, ok := fields["isError"]; ok {
		if b, ok := v.(bool); ok {
			entry.isError = b
		}
	}
	entry.updated = time.Now()
}

// evictIfOversizeLocked drops the oldest activity entries when the cache
// exceeds activityMaxSessions. Caller must hold activityMu. O(n) on
// eviction; runs only when the cap is exceeded so amortized cost is low.
func (n *notifyService) evictIfOversizeLocked() {
	if len(n.activity) < activityMaxSessions {
		return
	}
	var oldestKey string
	var oldestT time.Time
	for k, e := range n.activity {
		if oldestKey == "" || e.updated.Before(oldestT) {
			oldestKey = k
			oldestT = e.updated
		}
	}
	if oldestKey != "" {
		delete(n.activity, oldestKey)
	}
}

// activityFor returns a copy of the activity entry for the session, or nil
// if no activity has been recorded. Returning a copy lets the caller render
// without holding the lock.
func (n *notifyService) activityFor(sessionKey string) *activityEntry {
	n.activityMu.Lock()
	defer n.activityMu.Unlock()
	e := n.activity[sessionKey]
	if e == nil {
		return nil
	}
	cp := *e
	return &cp
}

// stringFromAgentPayload pulls a string field out of AgentEvent.Payload,
// which is a map[string]any in the chat pipeline's emit calls.
func stringFromAgentPayload(p any, key string) string {
	m, ok := p.(map[string]any)
	if !ok {
		return ""
	}
	return stringField(m, key)
}

// boolFromAgentPayload pulls a bool field out of AgentEvent.Payload. The
// second return distinguishes "field absent" (false, false) from "field
// present and false" (false, true) so callers don't unintentionally clear
// a previous error flag.
func boolFromAgentPayload(p any, key string) (value, ok bool) {
	m, isMap := p.(map[string]any)
	if !isMap {
		return false, false
	}
	v, present := m[key]
	if !present {
		return false, false
	}
	b, isBool := v.(bool)
	return b, isBool
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

// deliver formats the event into Korean prose and sends it via the plugin.
// Failures are logged at Error per logging.md (this IS the user-monitoring
// surface — its own failures are real). Bounded sub-timeout (10s) keeps a
// stuck Telegram API from blocking the worker's queue indefinitely.
//
// Two event flavors:
//   - broadcast events (chat.*) → formatErrorEvent renders the payload
//   - "_slog" events → payload is the already-formatted body string
//   - "_heartbeat" events → payload is the already-formatted body string
func (n *notifyService) deliver(ctx context.Context, ev notifyEvent) {
	switch ev.name {
	case "_slog", "_heartbeat":
		body, _ := ev.payload.(string)
		if body == "" {
			return
		}
		sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if _, err := n.plugin.SendNotification(sendCtx, body); err != nil {
			n.logger.Error("notify send failed", "event", ev.name, "error", err)
		}
		return
	}

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
		if line := n.activityLineKO(s.Key, now); line != "" {
			fmt.Fprintf(&b, "  %s\n", line)
		}
		if s.Model != "" {
			fmt.Fprintf(&b, "  모델: %s\n", s.Model)
		}
		if s.LastOutput != "" {
			fmt.Fprintf(&b, "  최근 응답: %s\n", truncate(s.LastOutput, 120))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// activityLineKO renders the per-session in-flight tool activity as a
// Korean status line, or "" when nothing has been recorded for the
// session. Distinguishes:
//
//   - running tool: "🔧 X 도구 실행 중 (12초째)"
//   - errored tool: "✗ X 도구 실패 (5분 전)"
//   - completed tool: "✓ X 도구 완료 (5분 전)"
//
// Activity older than 30 minutes is suppressed — stale state from a
// long-idle session would mislead the operator.
func (n *notifyService) activityLineKO(sessionKey string, now time.Time) string {
	e := n.activityFor(sessionKey)
	if e == nil || e.tool == "" {
		return ""
	}
	age := now.Sub(e.updated)
	if age > 30*time.Minute {
		return ""
	}
	switch {
	case e.running:
		return fmt.Sprintf("🔧 %s 도구 실행 중 (%s째)", e.tool, humanDuration(age))
	case e.isError:
		return fmt.Sprintf("✗ %s 도구 실패 (%s 전)", e.tool, humanDuration(age))
	default:
		return fmt.Sprintf("✓ %s 도구 완료 (%s 전)", e.tool, humanDuration(age))
	}
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
// alongside mirroredEvents so adding a new monitored event requires both
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
