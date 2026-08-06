package push

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type loggedRecord struct {
	level   slog.Level
	message string
}

// recordHandler is a slog.Handler that publishes each record to a channel,
// letting tests await the notifier's terminal log line deterministically instead
// of sleeping on the async delivery goroutine.
type recordHandler struct{ ch chan loggedRecord }

func (h recordHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h recordHandler) Handle(_ context.Context, r slog.Record) error {
	select {
	case h.ch <- loggedRecord{level: r.Level, message: r.Message}:
	default:
	}
	return nil
}
func (h recordHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h recordHandler) WithGroup(string) slog.Handler      { return h }

func recordingLogger() (*slog.Logger, chan loggedRecord) {
	ch := make(chan loggedRecord, 16)
	return slog.New(recordHandler{ch: ch}), ch
}

// waitForLog blocks until a log line matching want appears, or fails.
func waitForLog(t *testing.T, ch chan loggedRecord, want string) loggedRecord {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case rec := <-ch:
			if rec.message == want {
				return rec
			}
		case <-deadline:
			t.Fatalf("timed out waiting for log %q", want)
		}
	}
}

type fakeStore struct {
	mu     sync.Mutex
	tokens []DeviceToken
	pruned []string
}

func (s *fakeStore) Tokens() []DeviceToken {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]DeviceToken(nil), s.tokens...)
}

func (s *fakeStore) Prune(tokens []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruned = append(s.pruned, tokens...)
	return len(tokens), nil
}

func (s *fakeStore) prunedTokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.pruned...)
}

// fakeSender returns a scripted SendResult per device token.
type fakeSender struct {
	results  map[string]SendResult
	readyErr error
}

func (f fakeSender) Send(_ context.Context, deviceToken, _, _ string, _ map[string]string) SendResult {
	if r, ok := f.results[deviceToken]; ok {
		return r
	}
	return SendResult{OK: true}
}

func (f fakeSender) Ready(context.Context) error {
	return f.readyErr
}

func TestNotifier_NilSafe(t *testing.T) {
	var n *Notifier
	n.DeliverFallback("t", "b") // must not panic
}

func TestNotifierDeliverFallbackNoopsWithEmptyTokenList(t *testing.T) {
	logger, ch := recordingLogger()
	n := NewNotifier(NotifierDeps{
		Store:  &fakeStore{},
		Sender: fakeSender{},
		Logger: logger,
	})
	if n == nil {
		t.Fatal("notifier nil")
	}
	n.DeliverFallback("t", "b")
	waitForLog(t, ch, "push fallback: no registered device tokens; skipping FCM")
}

func TestNotifierDeliverFallbackSucceedsWithoutPruning(t *testing.T) {
	logger, ch := recordingLogger()
	store := &fakeStore{tokens: []DeviceToken{{Token: "a"}, {Token: "b"}}}
	n := NewNotifier(NotifierDeps{
		Store:  store,
		Sender: fakeSender{results: map[string]SendResult{"a": {OK: true}, "b": {OK: true}}},
		Logger: logger,
		Broadcast: func(string, rawJSON) {
			t.Error("broadcast should not fire on full delivery")
		},
	})
	n.DeliverFallback("t", "b")
	waitForLog(t, ch, "push fallback delivered")
	if got := store.prunedTokens(); len(got) != 0 {
		t.Errorf("pruned = %v, want none", got)
	}
}

func TestNotifierPartialDeliveryEvictsDeadTokens(t *testing.T) {
	logger, ch := recordingLogger()
	store := &fakeStore{tokens: []DeviceToken{{Token: "live"}, {Token: "dead"}}}
	n := NewNotifier(NotifierDeps{
		Store: store,
		Sender: fakeSender{results: map[string]SendResult{
			"live": {OK: true},
			"dead": {Permanent: true},
		}},
		Logger: logger,
	})
	n.DeliverFallback("t", "b")
	waitForLog(t, ch, "push fallback partial delivery")
	if got := store.prunedTokens(); len(got) != 1 || got[0] != "dead" {
		t.Errorf("pruned = %v, want [dead]", got)
	}
}

func TestNotifier_AllFailedBroadcastsAndErrors(t *testing.T) {
	logger, ch := recordingLogger()
	store := &fakeStore{tokens: []DeviceToken{{Token: "a"}, {Token: "b"}}}

	bc := make(chan map[string]any, 1)
	n := NewNotifier(NotifierDeps{
		Store: store,
		Sender: fakeSender{results: map[string]SendResult{
			"a": {AuthFailed: true},
			"b": {AuthFailed: true},
		}},
		Logger: logger,
		Broadcast: func(event string, payload rawJSON) {
			if event == "push.delivery_failed" {
				var m map[string]any
				if err := json.Unmarshal(payload, &m); err != nil {
					t.Errorf("unmarshal broadcast payload: %v", err)
					return
				}
				bc <- m
			}
		},
	})
	n.DeliverFallback("t", "b")

	select {
	case payload := <-bc:
		if payload["reason"] != "fcm_auth_failed" {
			t.Errorf("reason = %v, want fcm_auth_failed", payload["reason"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
	rec := waitForLog(t, ch, "push fallback failed: proactive notification not delivered to any device")
	if rec.level != slog.LevelError {
		t.Errorf("level = %v, want Error", rec.level)
	}
}

func TestNotifier_TransientFallbackFailureWarnsNotErrors(t *testing.T) {
	logger, ch := recordingLogger()
	store := &fakeStore{tokens: []DeviceToken{{Token: "a"}}}

	bc := make(chan map[string]any, 1)
	n := NewNotifier(NotifierDeps{
		Store: store,
		Sender: fakeSender{results: map[string]SendResult{
			"a": {Transient: true, Err: errors.New("dns unavailable")},
		}},
		Logger: logger,
		Broadcast: func(event string, payload rawJSON) {
			if event == "push.delivery_failed" {
				var m map[string]any
				if err := json.Unmarshal(payload, &m); err != nil {
					t.Errorf("unmarshal broadcast payload: %v", err)
					return
				}
				bc <- m
			}
		},
	})
	n.DeliverFallback("t", "b")

	select {
	case payload := <-bc:
		if payload["reason"] != "fcm_unavailable" {
			t.Errorf("reason = %v, want fcm_unavailable", payload["reason"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
	rec := waitForLog(t, ch, "push fallback failed: proactive notification not delivered to any device")
	if rec.level != slog.LevelWarn {
		t.Errorf("level = %v, want Warn", rec.level)
	}
}

func TestNotifier_MixedHardAndTransientFailureStaysError(t *testing.T) {
	logger, ch := recordingLogger()
	store := &fakeStore{tokens: []DeviceToken{{Token: "a"}, {Token: "b"}}}

	bc := make(chan map[string]any, 1)
	n := NewNotifier(NotifierDeps{
		Store: store,
		Sender: fakeSender{results: map[string]SendResult{
			"a": {Transient: true, Err: errors.New("dns unavailable")},
			"b": {Err: errors.New("payload rejected")},
		}},
		Logger: logger,
		Broadcast: func(event string, payload rawJSON) {
			if event == "push.delivery_failed" {
				var m map[string]any
				if err := json.Unmarshal(payload, &m); err != nil {
					t.Errorf("unmarshal broadcast payload: %v", err)
					return
				}
				bc <- m
			}
		},
	})
	n.DeliverFallback("t", "b")

	select {
	case payload := <-bc:
		if payload["reason"] != "fcm_send_failed" {
			t.Errorf("reason = %v, want fcm_send_failed", payload["reason"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
	rec := waitForLog(t, ch, "push fallback failed: proactive notification not delivered to any device")
	if rec.level != slog.LevelError {
		t.Errorf("level = %v, want Error", rec.level)
	}
}

func TestNotifierDeliveryEnabledProbesSender(t *testing.T) {
	logger, ch := recordingLogger()
	healthy := NewNotifier(NotifierDeps{
		Store:  &fakeStore{},
		Sender: fakeSender{},
		Logger: logger,
	})
	if !healthy.DeliveryEnabled(context.Background()) {
		t.Fatal("healthy sender reported delivery disabled")
	}

	unavailable := NewNotifier(NotifierDeps{
		Store:  &fakeStore{},
		Sender: fakeSender{readyErr: errors.New("token endpoint unavailable")},
		Logger: logger,
	})
	if unavailable.DeliveryEnabled(context.Background()) {
		t.Fatal("unavailable sender reported delivery enabled")
	}
	rec := waitForLog(t, ch, "FCM push fallback unavailable; background SSE remains required")
	if rec.level != slog.LevelWarn {
		t.Errorf("level = %v, want Warn", rec.level)
	}
}
