package server

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// Test that buildStatusReport produces a Korean summary listing every
// running session and excluding terminal ones. Regression guard for the
// session list filter — silently dropping running sessions would defeat
// the whole notify_status RPC.
func TestNotifyService_BuildStatusReport_ListsRunningOnly(t *testing.T) {
	mgr := session.NewManager()
	startedAt := time.Now().Add(-3 * time.Minute).UnixMilli()

	running := &session.Session{
		Key:       "telegram:111",
		Kind:      session.KindDirect,
		Status:    session.StatusRunning,
		Label:     "메인 세션",
		StartedAt: &startedAt,
		UpdatedAt: time.Now().UnixMilli(),
	}
	done := &session.Session{
		Key:       "telegram:222",
		Kind:      session.KindDirect,
		Status:    session.StatusDone,
		Label:     "완료된 세션",
		UpdatedAt: time.Now().UnixMilli(),
	}
	if err := mgr.Set(running); err != nil {
		t.Fatalf("Set(running): %v", err)
	}
	if err := mgr.Set(done); err != nil {
		t.Fatalf("Set(done): %v", err)
	}

	n := &notifyService{sessions: mgr}
	report := n.buildStatusReport(time.Now())

	if !strings.Contains(report, "telegram:111") {
		t.Errorf("running session telegram:111 missing from report:\n%s", report)
	}
	if strings.Contains(report, "telegram:222") {
		t.Errorf("done session telegram:222 should not appear in report:\n%s", report)
	}
	if !strings.Contains(report, "메인 세션") {
		t.Errorf("running session label missing from report:\n%s", report)
	}
	if !strings.Contains(report, "활성 세션") {
		t.Errorf("expected Korean header '활성 세션', got:\n%s", report)
	}
}

// Test the empty-state message. When no sessions are running the report
// must say so explicitly; an empty body would render in Telegram as a
// blank message which the operator would interpret as "the bot is broken".
func TestNotifyService_BuildStatusReport_EmptyState(t *testing.T) {
	mgr := session.NewManager()
	n := &notifyService{sessions: mgr}
	report := n.buildStatusReport(time.Now())
	if !strings.Contains(report, "실행 중인 세션 없음") {
		t.Errorf("empty-state message missing, got:\n%s", report)
	}
}

// Test debounce: checkDebounce + markSent — first marked send succeeds,
// second within window is blocked, distinct event is unaffected.
// Without this guard a flapping failure mode would spam the monitoring chat.
func TestNotifyService_Debounce(t *testing.T) {
	n := &notifyService{lastSent: make(map[string]time.Time)}

	if !n.checkDebounce("chat.delivery_failed") {
		t.Fatal("first check should pass")
	}
	n.markSent("chat.delivery_failed")
	if n.checkDebounce("chat.delivery_failed") {
		t.Fatal("second check within debounce window should fail")
	}
	// Distinct event names share no debounce timer.
	if !n.checkDebounce("chat.compaction_stuck") {
		t.Fatal("distinct event should not be blocked by another's debounce")
	}
}

// Test formatErrorEvent: each monitored event produces a non-empty Korean
// alert with the session key and reason inlined. Regressions in
// mirroredEvents vs errorHeadlineKO would surface as empty strings here.
func TestFormatErrorEvent_AllMonitored(t *testing.T) {
	payload := map[string]any{
		"session": "telegram:42",
		"reason":  "reply_func_error",
		"error":   "context deadline exceeded",
	}
	for name := range mirroredEvents {
		body := formatErrorEvent(name, payload)
		if body == "" {
			t.Errorf("monitored event %q produced empty body", name)
			continue
		}
		if !strings.Contains(body, "telegram:42") {
			t.Errorf("event %q body missing session key:\n%s", name, body)
		}
		if !strings.HasPrefix(body, "⚠️") {
			t.Errorf("event %q body missing alert prefix:\n%s", name, body)
		}
	}
}

// Test that an unknown event renders as empty (defensive — the tap
// already filters, but defense in depth keeps a future caller from
// pushing arbitrary events through formatErrorEvent and getting garbage
// in the monitoring chat).
func TestFormatErrorEvent_UnknownReturnsEmpty(t *testing.T) {
	if got := formatErrorEvent("totally.fake.event", nil); got != "" {
		t.Errorf("unknown event should return empty, got: %q", got)
	}
}

// Test newNotifyService nil-safety: missing plugin or zero ChatID returns
// nil so callers can short-circuit registration.
func TestNewNotifyService_NilWhenDisabled(t *testing.T) {
	if got := newNotifyService(nil, nil, nil); got != nil {
		t.Error("expected nil notify service when plugin is nil")
	}
}

// Test the debounce-on-drop fix: a queue-full drop must NOT update the
// last-sent timestamp, otherwise subsequent legitimate sends would be
// suppressed for the full debounce window. Regression guard for the bug
// I caught in self-review.
func TestNotifyService_DebounceNotPoisonedByDrop(t *testing.T) {
	n := &notifyService{lastSent: make(map[string]time.Time)}

	// Sanity: first checkDebounce returns true.
	if !n.checkDebounce("evt") {
		t.Fatal("first check should pass")
	}
	// Second check (no markSent) must STILL return true — we haven't
	// recorded a successful send yet.
	if !n.checkDebounce("evt") {
		t.Error("second check before markSent should still pass (drop must not poison)")
	}
	// Now mark sent. Subsequent check inside the window must return false.
	n.markSent("evt")
	if n.checkDebounce("evt") {
		t.Error("check after markSent within window should fail")
	}
}

// Test recordActivity from an "agent" broadcast (events.AgentEvent struct
// payload — fallback path with no Publisher). tool.start sets running.
func TestNotifyService_RecordActivity_AgentStruct(t *testing.T) {
	n := newNotifyServiceForTest()
	evt := events.AgentEvent{
		Kind:       "tool.start",
		SessionKey: "telegram:777",
		RunID:      "run-1",
		Payload:    map[string]any{"tool": "gmail.search"},
	}
	n.recordActivity("agent", evt)

	got := n.activityFor("telegram:777")
	if got == nil {
		t.Fatal("expected activity entry")
	}
	if got.tool != "gmail.search" || !got.running {
		t.Errorf("got tool=%q running=%v, want gmail.search running=true", got.tool, got.running)
	}
}

// Test recordActivity from "agent.event" (publisher path — flattened map).
// Same logical event, different shape; both must populate the cache.
func TestNotifyService_RecordActivity_AgentMap(t *testing.T) {
	n := newNotifyServiceForTest()
	payload := map[string]any{
		"kind":       "tool.start",
		"sessionKey": "telegram:777",
		"runId":      "run-1",
		"payload":    map[string]any{"tool": "exec"},
	}
	n.recordActivity("agent.event", payload)

	got := n.activityFor("telegram:777")
	if got == nil || got.tool != "exec" || !got.running {
		t.Errorf("got %+v, want tool=exec running=true", got)
	}
}

// Test full lifecycle: tool.start sets running, tool.end clears it.
func TestNotifyService_RecordActivity_ToolEndClearsRunning(t *testing.T) {
	n := newNotifyServiceForTest()
	n.recordActivity("agent", events.AgentEvent{
		Kind: "tool.start", SessionKey: "s", Payload: map[string]any{"tool": "fs.read"},
	})
	n.recordActivity("agent", events.AgentEvent{
		Kind: "tool.end", SessionKey: "s", Payload: map[string]any{"isError": true},
	})
	got := n.activityFor("s")
	if got == nil || got.running {
		t.Errorf("got %+v, want running=false after tool.end", got)
	}
	if !got.isError {
		t.Errorf("got isError=%v, want true (tool.end carried error flag)", got.isError)
	}
}

// activityLineKO must produce distinct shapes for running / errored /
// completed tool states. Stale (>30min) entries return empty.
func TestNotifyService_ActivityLineKO_States(t *testing.T) {
	n := newNotifyServiceForTest()
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// No activity → empty.
	if got := n.activityLineKO("none", now); got != "" {
		t.Errorf("no-activity line: got %q, want empty", got)
	}

	// Running tool.
	n.activity["a"] = &activityEntry{tool: "exec", running: true, updated: now.Add(-12 * time.Second)}
	if got := n.activityLineKO("a", now); !strings.Contains(got, "실행 중") {
		t.Errorf("running line: got %q, want '실행 중'", got)
	}

	// Errored tool.
	n.activity["b"] = &activityEntry{tool: "exec", running: false, isError: true, updated: now.Add(-2 * time.Minute)}
	if got := n.activityLineKO("b", now); !strings.Contains(got, "실패") {
		t.Errorf("error line: got %q, want '실패'", got)
	}

	// Completed tool.
	n.activity["c"] = &activityEntry{tool: "exec", running: false, isError: false, updated: now.Add(-2 * time.Minute)}
	if got := n.activityLineKO("c", now); !strings.Contains(got, "완료") {
		t.Errorf("complete line: got %q, want '완료'", got)
	}

	// Stale (> 30 min).
	n.activity["d"] = &activityEntry{tool: "exec", running: false, updated: now.Add(-31 * time.Minute)}
	if got := n.activityLineKO("d", now); got != "" {
		t.Errorf("stale line: got %q, want empty", got)
	}
}

// Status report includes the activity line when present.
func TestNotifyService_StatusReport_IncludesActivity(t *testing.T) {
	mgr := session.NewManager()
	startedAt := time.Now().Add(-1 * time.Minute).UnixMilli()
	if err := mgr.Set(&session.Session{
		Key: "telegram:1", Status: session.StatusRunning, Label: "메인", StartedAt: &startedAt,
	}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	n := &notifyService{
		sessions: mgr,
		activity: map[string]*activityEntry{
			"telegram:1": {tool: "vega.search", running: true, updated: time.Now().Add(-3 * time.Second)},
		},
	}
	report := n.buildStatusReport(time.Now())
	if !strings.Contains(report, "vega.search") {
		t.Errorf("report missing tool name:\n%s", report)
	}
	if !strings.Contains(report, "실행 중") {
		t.Errorf("report missing running marker:\n%s", report)
	}
}

// Activity cache evicts the oldest entry once activityMaxSessions is hit.
func TestNotifyService_ActivityEviction(t *testing.T) {
	n := newNotifyServiceForTest()
	for i := range activityMaxSessions {
		key := keyN(i)
		n.activity[key] = &activityEntry{tool: "x", updated: time.Now().Add(time.Duration(i) * time.Second)}
	}
	// Trigger eviction by inserting one more.
	n.activityMu.Lock()
	n.evictIfOversizeLocked()
	n.activityMu.Unlock()
	// First map has activityMaxSessions entries; eviction triggers only at
	// >= activityMaxSessions, so still equal here. Push it over.
	n.activity[keyN(activityMaxSessions)] = &activityEntry{tool: "new", updated: time.Now().Add(time.Hour)}
	n.activityMu.Lock()
	n.evictIfOversizeLocked()
	n.activityMu.Unlock()
	if len(n.activity) > activityMaxSessions {
		t.Errorf("activity map size %d, want <= %d", len(n.activity), activityMaxSessions)
	}
	// Oldest (key 0) should be gone.
	if _, ok := n.activity["s0"]; ok {
		t.Errorf("oldest entry s0 should have been evicted")
	}
}

func keyN(i int) string { return "s" + itoa(i) }
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}

// formatHeartbeatLine returns a non-empty Korean line with all key stats.
func TestNotifyService_HeartbeatLine(t *testing.T) {
	mgr := session.NewManager()
	n := &notifyService{sessions: mgr}
	startTime := time.Now().Add(-2 * time.Minute)
	line := n.buildHeartbeatLine(startTime, time.Now())
	for _, want := range []string{"💓", "uptime", "세션", "goroutine", "mem"} {
		if !strings.Contains(line, want) {
			t.Errorf("heartbeat line missing %q:\n%s", want, line)
		}
	}
}

// slog forwarder: ERROR records produce a notify enqueue when not suppressed.
func TestNotifySlogHandler_ForwardsErrors(t *testing.T) {
	n := newNotifyServiceForTest()
	delegate := slog.NewTextHandler(&bytes.Buffer{}, nil)
	h := newNotifySlogHandler(delegate, n)

	logger := slog.New(h)
	logger.Error("disk full", "error", "no space left")

	select {
	case ev := <-n.queue:
		if ev.name != "_slog" {
			t.Errorf("got event name %q, want _slog", ev.name)
		}
		body, _ := ev.payload.(string)
		if !strings.Contains(body, "disk full") {
			t.Errorf("body missing message: %q", body)
		}
	case <-time.After(time.Second):
		t.Fatal("expected enqueue, got none within 1s")
	}
}

// slog forwarder: suppress-prefix messages are NOT forwarded.
func TestNotifySlogHandler_SuppressesSelfLog(t *testing.T) {
	n := newNotifyServiceForTest()
	delegate := slog.NewTextHandler(&bytes.Buffer{}, nil)
	h := newNotifySlogHandler(delegate, n)

	logger := slog.New(h)
	logger.Error("notify send failed", "error", "boom")

	select {
	case ev := <-n.queue:
		t.Errorf("expected no enqueue, got %+v (would loop on monitoring outage)", ev)
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

// slog forwarder: WARN records pass through to delegate but are NOT forwarded.
func TestNotifySlogHandler_IgnoresBelowError(t *testing.T) {
	n := newNotifyServiceForTest()
	var buf bytes.Buffer
	delegate := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := newNotifySlogHandler(delegate, n)
	logger := slog.New(h)
	logger.Warn("transient hiccup")

	if !strings.Contains(buf.String(), "transient hiccup") {
		t.Errorf("delegate should have received the WARN: %q", buf.String())
	}
	select {
	case ev := <-n.queue:
		t.Errorf("expected no enqueue for WARN, got %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

// swappableHandler: swap atomically updates the inner handler and
// captured loggers see the new handler immediately.
func TestSwappableHandler_Swap(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	h1 := slog.NewTextHandler(&buf1, &slog.HandlerOptions{Level: slog.LevelDebug})
	h2 := slog.NewTextHandler(&buf2, &slog.HandlerOptions{Level: slog.LevelDebug})

	swap := newSwappableHandler(h1)
	logger := slog.New(swap)
	logger.Info("before-swap")
	swap.Swap(h2)
	logger.Info("after-swap")

	if !strings.Contains(buf1.String(), "before-swap") || strings.Contains(buf1.String(), "after-swap") {
		t.Errorf("h1 saw wrong messages: %q", buf1.String())
	}
	if !strings.Contains(buf2.String(), "after-swap") || strings.Contains(buf2.String(), "before-swap") {
		t.Errorf("h2 saw wrong messages: %q", buf2.String())
	}
}

// formatSlogRecord pulls in the relevant attributes (error, session, channel)
// and prefixes by level.
func TestFormatSlogRecord_AttributeExtraction(t *testing.T) {
	r := slog.NewRecord(time.Now(), slog.LevelError, "telegram api 500", 0)
	r.AddAttrs(
		slog.String("session", "telegram:42"),
		slog.String("channel", "telegram"),
		slog.String("error", "internal server error"),
	)
	body := formatSlogRecord(r)
	for _, want := range []string{"🔴", "telegram api 500", "telegram:42", "telegram", "internal server error"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %q", want, body)
		}
	}
}

// newNotifyServiceForTest returns a notifyService with the maps initialised
// but no plugin. Sufficient for any test that doesn't actually deliver.
func newNotifyServiceForTest() *notifyService {
	return &notifyService{
		logger:   slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		queue:    make(chan notifyEvent, 16),
		lastSent: make(map[string]time.Time),
		activity: make(map[string]*activityEntry),
	}
}

// Compile guard so the unused-import detector doesn't flag context.
var _ = context.Background
