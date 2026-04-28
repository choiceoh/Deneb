package server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	if got := newNotifyService(nil, nil, nil, nil); got != nil {
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

// Critical regression: a logger derived via With() BEFORE the swap must
// also forward ERROR records after the swap. Subsystems do this:
//
//	cronLogger := s.logger.With("subsystem", "cron")
//	... later ...
//	cronLogger.Error("boom")  // must reach the monitoring chat
//
// Before the lazy-attr tee, this case bypassed the wrap entirely.
func TestSwappableHandler_PreSwapWithDerivedLogger(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	swap := newSwappableHandler(base)
	rootLogger := slog.New(swap)

	// Subsystem captures derived logger BEFORE notify is wired.
	subsystem := rootLogger.With("subsystem", "cron")

	// Notify wires up later — this is the swap moment.
	n := newNotifyServiceForTest()
	swap.Swap(newNotifySlogHandler(swap.currentInner(), n))

	// Subsystem ERROR after swap must reach the notify queue.
	subsystem.Error("cron job failed", "error", "permission denied")

	select {
	case ev := <-n.queue:
		body, _ := ev.payload.(string)
		if !strings.Contains(body, "cron job failed") {
			t.Errorf("forwarded body missing message: %q", body)
		}
	case <-time.After(time.Second):
		t.Fatal("derived logger ERROR did not forward — lazy-attr tee broken")
	}

	// Delegate also got the record (with attrs preserved).
	if !strings.Contains(buf.String(), "subsystem=cron") {
		t.Errorf("delegate output missing subsystem attr: %q", buf.String())
	}
}

// Same regression for WithGroup: a group-derived logger must also forward.
func TestSwappableHandler_PreSwapWithGroupDerivedLogger(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	swap := newSwappableHandler(base)
	rootLogger := slog.New(swap)

	grouped := rootLogger.WithGroup("svc")

	n := newNotifyServiceForTest()
	swap.Swap(newNotifySlogHandler(swap.currentInner(), n))

	grouped.Error("grouped failure")
	select {
	case ev := <-n.queue:
		body, _ := ev.payload.(string)
		if !strings.Contains(body, "grouped failure") {
			t.Errorf("forwarded body missing message: %q", body)
		}
	case <-time.After(time.Second):
		t.Fatal("WithGroup-derived ERROR did not forward")
	}
}

// Chained With: each link must keep its attrs visible in the delegate
// AND each link's ERROR records must forward.
func TestSwappableHandler_ChainedWithForwardsAndPreservesAttrs(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	swap := newSwappableHandler(base)
	rootLogger := slog.New(swap)

	// Two layers of With, like: subsystem → component
	chain := rootLogger.With("subsystem", "cron").With("component", "scheduler")

	n := newNotifyServiceForTest()
	swap.Swap(newNotifySlogHandler(swap.currentInner(), n))

	chain.Error("multi-attr failure")

	select {
	case <-n.queue:
		// forwarded
	case <-time.After(time.Second):
		t.Fatal("chained With did not forward")
	}
	out := buf.String()
	if !strings.Contains(out, "subsystem=cron") || !strings.Contains(out, "component=scheduler") {
		t.Errorf("delegate missing chained attrs: %q", out)
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

// Activity cleanup: terminal session transitions must clear the activity
// entry. Without this, a tool.start without a paired tool.end (panic / kill)
// would leave the activity "running" indefinitely until LRU evicts.
func TestNotifyService_ClearsActivityOnSessionTerminate(t *testing.T) {
	mgr := session.NewManager()
	n := newNotifyServiceForTest()
	n.sessions = mgr
	n.subscribeSessionEvents()

	// Seed an active running session + activity.
	if err := mgr.Set(&session.Session{Key: "telegram:1", Status: session.StatusRunning}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	n.activity["telegram:1"] = &activityEntry{tool: "exec", running: true, updated: time.Now()}

	// Now transition the session to FAILED — should fire the bus event.
	mgr.ApplyLifecycleEvent("telegram:1", session.LifecycleEvent{
		Phase:         session.PhaseError,
		FailureReason: "panic",
	})

	// Bus is async — poll briefly for the activity entry to disappear.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n.activityMu.Lock()
		_, present := n.activity["telegram:1"]
		n.activityMu.Unlock()
		if !present {
			return // pass
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("activity entry not cleared after session terminal transition")
}

// Activity cleanup: non-terminal status changes must NOT clear the entry.
// Otherwise a brief running→running re-emit would erase live state.
func TestNotifyService_KeepsActivityOnNonTerminal(t *testing.T) {
	mgr := session.NewManager()
	n := newNotifyServiceForTest()
	n.sessions = mgr
	n.subscribeSessionEvents()

	if err := mgr.Set(&session.Session{Key: "telegram:1", Status: session.StatusRunning}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	n.activity["telegram:1"] = &activityEntry{tool: "exec", running: true, updated: time.Now()}

	// Re-emit running (no-op transition by design; we exercise the
	// guard rather than the manager's allowance).
	bus := mgr.EventBusRef()
	bus.Emit(session.Event{
		Kind:      session.EventStatusChanged,
		Key:       "telegram:1",
		OldStatus: session.StatusRunning,
		NewStatus: session.StatusRunning,
	})

	time.Sleep(50 * time.Millisecond) // let the bus drain

	n.activityMu.Lock()
	_, present := n.activity["telegram:1"]
	n.activityMu.Unlock()
	if !present {
		t.Error("activity entry was cleared on non-terminal transition")
	}
}

// Self-poll happy path: a 200 response within timeout returns ok=true and
// non-zero latency. Validates the basic HTTP roundtrip against a real
// loopback listener, not a stub.
func TestNotifyService_SelfPoll_Healthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	n := newNotifyServiceForTest()
	n.boundAddr = func() string { return addr }
	n.httpClient = &http.Client{Timeout: selfPollTimeout}

	ok, latency, err := n.selfPoll(context.Background())
	if err != nil || !ok {
		t.Fatalf("expected ok=true, got ok=%v err=%v", ok, err)
	}
	if latency <= 0 {
		t.Errorf("expected positive latency, got %v", latency)
	}
}

// Self-poll on hung mux: server doesn't respond within selfPollTimeout.
// Returns ok=false with the timeout wrapped in the error. This is the
// PRIMARY hang-detection path — without it the heartbeat would happily
// say "alive" while user requests stall.
//
// hung MUST be closed before srv.Close() so the handler returns and the
// server's in-flight wait group drains; defer order is LIFO so close(hung)
// is registered AFTER srv.Close() to run first on cleanup.
func TestNotifyService_SelfPoll_Hung(t *testing.T) {
	hung := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-hung
	}))
	defer srv.Close()
	defer close(hung)

	addr := strings.TrimPrefix(srv.URL, "http://")
	n := newNotifyServiceForTest()
	n.boundAddr = func() string { return addr }
	n.httpClient = &http.Client{Timeout: 100 * time.Millisecond}

	ok, _, err := n.selfPoll(context.Background())
	if ok || err == nil {
		t.Fatalf("expected hang detection: got ok=%v err=%v", ok, err)
	}
}

// Self-poll on 5xx: a non-2xx response means the gateway is responding
// but unhealthy. Treated identically to a hang for alerting purposes.
func TestNotifyService_SelfPoll_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	n := newNotifyServiceForTest()
	n.boundAddr = func() string { return addr }
	n.httpClient = &http.Client{Timeout: selfPollTimeout}

	ok, _, err := n.selfPoll(context.Background())
	if ok || err == nil {
		t.Fatalf("expected !ok on 500, got ok=%v err=%v", ok, err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention the status code: %v", err)
	}
}

// Self-poll skips when listener not bound. Returning ok=true (instead of
// alerting) is correct: during startup the listener legitimately doesn't
// exist yet and we don't want spurious "🚨 응답 없음" alerts on every
// boot.
func TestNotifyService_SelfPoll_NoBoundAddr(t *testing.T) {
	n := newNotifyServiceForTest()
	n.boundAddr = func() string { return "" }
	ok, latency, err := n.selfPoll(context.Background())
	if !ok || err != nil || latency != 0 {
		t.Errorf("expected silent skip: got ok=%v latency=%v err=%v", ok, latency, err)
	}
}

// Heartbeat line under high goroutine count switches prefix to ⚠️.
// Can't easily force runtime.NumGoroutine() above the threshold in a
// test, so this validates the threshold logic via direct construction
// of the format expectation: the prefix is goroutine-driven only when
// the count crosses goroutineWarnAbsolute. We assert the healthy path
// here (negative path) and the threshold constant separately.
func TestNotifyService_HeartbeatLine_HealthyPrefix(t *testing.T) {
	n := newNotifyServiceForTest()
	n.sessions = session.NewManager()
	line := n.buildHeartbeatLine(time.Now().Add(-2*time.Minute), time.Now())
	if !strings.HasPrefix(line, "💓 게이트웨이 정상") {
		t.Errorf("expected healthy prefix, got: %q", line)
	}
}

// composeHangAlert renders the operator-facing 🚨 line with the error
// truncated. Empty/nil errors get a placeholder so the message never
// looks blank.
func TestNotifyService_ComposeHangAlert(t *testing.T) {
	n := newNotifyServiceForTest()
	got := n.composeHangAlert(nil)
	if !strings.HasPrefix(got, "🚨") {
		t.Errorf("expected 🚨 prefix, got: %q", got)
	}
	if !strings.Contains(got, "응답 없음") {
		t.Errorf("expected hang phrasing, got: %q", got)
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
		logger:     slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		httpClient: &http.Client{Timeout: selfPollTimeout},
		queue:      make(chan notifyEvent, 16),
		lastSent:   make(map[string]time.Time),
		activity:   make(map[string]*activityEntry),
	}
}

// Compile guard so the unused-import detector doesn't flag context.
var _ = context.Background
