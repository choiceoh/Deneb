package push

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// recordHandler is a slog.Handler that publishes each record's message to a
// channel, letting tests await the notifier's terminal log line deterministically
// instead of sleeping on the async delivery goroutine.
type recordHandler struct{ ch chan string }

func (h recordHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h recordHandler) Handle(_ context.Context, r slog.Record) error {
	select {
	case h.ch <- r.Message:
	default:
	}
	return nil
}
func (h recordHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h recordHandler) WithGroup(string) slog.Handler      { return h }

func recordingLogger() (*slog.Logger, chan string) {
	ch := make(chan string, 16)
	return slog.New(recordHandler{ch: ch}), ch
}

// waitForLog blocks until a log line containing want appears, or fails.
func waitForLog(t *testing.T, ch chan string, want string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case msg := <-ch:
			if msg == want {
				return
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
	results map[string]SendResult
}

func (f fakeSender) Send(_ context.Context, deviceToken, _, _ string, _ map[string]string) SendResult {
	if r, ok := f.results[deviceToken]; ok {
		return r
	}
	return SendResult{OK: true}
}

// capturingSender records the data map of the last Send so a test can assert the
// structured payload that reached FCM.
type capturingSender struct {
	mu   sync.Mutex
	data map[string]string
}

func (c *capturingSender) Send(_ context.Context, _, _, _ string, data map[string]string) SendResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = data
	return SendResult{OK: true}
}

func (c *capturingSender) lastData() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.data
}

func TestNotifier_NilSafe(t *testing.T) {
	var n *Notifier
	n.DeliverFallback("t", "b")         // must not panic
	n.DeliverPhoneAction("t", "b", nil) // must not panic
}

// DeliverPhoneAction must forward its caller-supplied data (kind=phone_action +
// the command) to Send, not the hardcoded proactive map DeliverFallback uses.
func TestNotifier_DeliverPhoneActionForwardsData(t *testing.T) {
	logger, ch := recordingLogger()
	cs := &capturingSender{}
	n := NewNotifier(NotifierDeps{
		Store:  &fakeStore{tokens: []DeviceToken{{Token: "a"}}},
		Sender: cs,
		Logger: logger,
	})
	n.DeliverPhoneAction("전화 걸기", "010-1234-5678", map[string]string{
		"kind": "phone_action", "action": "dial", "number": "010-1234-5678",
	})
	waitForLog(t, ch, "push fallback delivered")
	got := cs.lastData()
	if got["kind"] != "phone_action" || got["action"] != "dial" || got["number"] != "010-1234-5678" {
		t.Fatalf("phone-action data not forwarded to Send: %v", got)
	}
}

func TestNotifier_NoTokensIsNoop(t *testing.T) {
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

func TestNotifier_AllDelivered(t *testing.T) {
	logger, ch := recordingLogger()
	store := &fakeStore{tokens: []DeviceToken{{Token: "a"}, {Token: "b"}}}
	n := NewNotifier(NotifierDeps{
		Store:  store,
		Sender: fakeSender{results: map[string]SendResult{"a": {OK: true}, "b": {OK: true}}},
		Logger: logger,
		Broadcast: func(string, any) {
			t.Error("broadcast should not fire on full delivery")
		},
	})
	n.DeliverFallback("t", "b")
	waitForLog(t, ch, "push fallback delivered")
	if got := store.prunedTokens(); len(got) != 0 {
		t.Errorf("pruned = %v, want none", got)
	}
}

func TestNotifier_PrunesDeadTokensOnPartialDelivery(t *testing.T) {
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
		Broadcast: func(event string, payload any) {
			if event == "push.delivery_failed" {
				bc <- payload.(map[string]any)
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
	waitForLog(t, ch, "push fallback failed: proactive notification not delivered to any device")
}
