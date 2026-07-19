package notify

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

// Test debounce: checkDebounce + markSent — first marked send succeeds,
// second within window is blocked, distinct event is unaffected.
// Without this guard a flapping failure mode would spam the monitoring chat.
func TestNotifyServiceDebounceDeniesRepeatWithinWindow(t *testing.T) {
	n := &Service{lastSent: make(map[string]time.Time)}

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

func TestFormatErrorEvent_ToolFailedPayload(t *testing.T) {
	body := formatErrorEvent("chat.tool_failed", map[string]any{
		"sessionKey": "client:main",
		"tool":       "gmail",
		"reason":     "mutation_tool_in_band_failure",
		"error":      "발송 실패: 550 mailbox unavailable",
	})
	for _, want := range []string{"도구 실행 실패", "client:main", "gmail", "발송 실패"} {
		if !strings.Contains(body, want) {
			t.Fatalf("tool_failed body missing %q:\n%s", want, body)
		}
	}
}

// Test newNotifyService nil-safety: missing plugin or zero ChatID returns
// nil so callers can short-circuit registration.
func TestNewNotifyService_NilWhenDisabled(t *testing.T) {
	if got := NewService(nil, nil, nil, nil); got != nil {
		t.Error("expected nil notify service when plugin is nil")
	}
}

// Test the debounce-on-drop fix: a queue-full drop must NOT update the
// last-sent timestamp, otherwise subsequent legitimate sends would be
// suppressed for the full debounce window. Regression guard for the bug
// I caught in self-review.
func TestNotifyServiceDebounceUnaffectedWithoutMarkSent(t *testing.T) {
	n := &Service{lastSent: make(map[string]time.Time)}

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

// formatHeartbeatLine returns a non-empty Korean line with all key stats.
func TestNotifyServiceHeartbeatLineFormatsKeyStats(t *testing.T) {
	mgr := session.NewManager()
	n := &Service{sessions: mgr}
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
	h := NewSlogHandler(delegate, n)

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
func TestNotifySlogHandlerIgnoresSelfLogMessages(t *testing.T) {
	n := newNotifyServiceForTest()
	delegate := slog.NewTextHandler(&bytes.Buffer{}, nil)
	h := NewSlogHandler(delegate, n)

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
	h := NewSlogHandler(delegate, n)
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
	swap := NewSwappableHandler(base)
	rootLogger := slog.New(swap)

	// Subsystem captures derived logger BEFORE notify is wired.
	subsystem := rootLogger.With("subsystem", "cron")

	// Notify wires up later — this is the swap moment.
	n := newNotifyServiceForTest()
	swap.Swap(NewSlogHandler(swap.currentInner(), n))

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
	swap := NewSwappableHandler(base)
	rootLogger := slog.New(swap)

	grouped := rootLogger.WithGroup("svc")

	n := newNotifyServiceForTest()
	swap.Swap(NewSlogHandler(swap.currentInner(), n))

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
	swap := NewSwappableHandler(base)
	rootLogger := slog.New(swap)

	// Two layers of With, like: subsystem → component
	chain := rootLogger.With("subsystem", "cron").With("component", "scheduler")

	n := newNotifyServiceForTest()
	swap.Swap(NewSlogHandler(swap.currentInner(), n))

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
func TestSwappableHandlerSwapUpdatesInnerHandler(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	h1 := slog.NewTextHandler(&buf1, &slog.HandlerOptions{Level: slog.LevelDebug})
	h2 := slog.NewTextHandler(&buf2, &slog.HandlerOptions{Level: slog.LevelDebug})

	swap := NewSwappableHandler(h1)
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

// Self-poll happy path: a 200 response within timeout returns ok=true and
// non-zero latency. Validates the basic HTTP roundtrip against a real
// loopback listener, not a stub.
func TestNotifyServiceSelfPollWhenHealthyReturnsOK(t *testing.T) {
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
func TestNotifyServiceSelfPollDetectsTimeout(t *testing.T) {
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
func TestNotifyServiceSelfPollReturnsErrorOnNonOK(t *testing.T) {
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
func TestNotifyServiceSelfPollSkipsWhenAddrMissing(t *testing.T) {
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
func TestNotifyServiceHeartbeatLineWhenHealthyPrefix(t *testing.T) {
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
func TestNotifyServiceComposeHangAlertRendersPlaceholderForNilError(t *testing.T) {
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
func newNotifyServiceForTest() *Service {
	return &Service{
		logger:     slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		httpClient: &http.Client{Timeout: selfPollTimeout},
		queue:      make(chan notifyEvent, 16),
		lastSent:   make(map[string]time.Time),
	}
}

// Compile guard so the unused-import detector doesn't flag context.
var _ = context.Background
