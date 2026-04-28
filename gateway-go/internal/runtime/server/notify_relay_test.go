package server

import (
	"strings"
	"testing"
	"time"

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

// Test debounce: two calls within notifyDebounce return true then false.
// Without this guard a flapping failure mode would spam the monitoring chat.
func TestNotifyService_Debounce(t *testing.T) {
	n := &notifyService{lastSent: make(map[string]time.Time)}

	if !n.shouldSend("chat.delivery_failed") {
		t.Fatal("first call should send")
	}
	if n.shouldSend("chat.delivery_failed") {
		t.Fatal("second call within debounce window should NOT send")
	}
	// Distinct event names share no debounce timer.
	if !n.shouldSend("chat.compaction_stuck") {
		t.Fatal("distinct event should not be blocked by another's debounce")
	}
}

// Test formatErrorEvent: each monitored event produces a non-empty Korean
// alert with the session key and reason inlined. Regressions in
// monitoredEvents vs errorHeadlineKO would surface as empty strings here.
func TestFormatErrorEvent_AllMonitored(t *testing.T) {
	payload := map[string]any{
		"session": "telegram:42",
		"reason":  "reply_func_error",
		"error":   "context deadline exceeded",
	}
	for name := range monitoredEvents {
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
