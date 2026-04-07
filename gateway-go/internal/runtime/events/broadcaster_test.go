package events

import (
	"sync"
	"testing"
)

type mockSubscriber struct {
	id             string
	authed         bool
	bufferedAmount int64
	mu             sync.Mutex
	received       [][]byte
	failSend       bool
}

func (m *mockSubscriber) ID() string            { return m.id }
func (m *mockSubscriber) IsAuthenticated() bool { return m.authed }
func (m *mockSubscriber) BufferedAmount() int64 { return m.bufferedAmount }
func (m *mockSubscriber) SendEvent(data []byte) error {
	if m.failSend {
		return &sendError{}
	}
	m.mu.Lock()
	m.received = append(m.received, append([]byte(nil), data...))
	m.mu.Unlock()
	return nil
}

type sendError struct{}

func (e *sendError) Error() string { return "send failed" }

func TestBroadcast_AllSubscribers(t *testing.T) {
	b := NewBroadcaster()
	s1 := &mockSubscriber{id: "s1", authed: true}
	s2 := &mockSubscriber{id: "s2", authed: true}

	b.Subscribe(s1, Filter{})
	b.Subscribe(s2, Filter{})

	sent, errs := b.Broadcast("test.event", map[string]string{"key": "value"})
	if sent != 2 {
		t.Errorf("expected 2 sent, got %d", sent)
	}
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
	if len(s1.received) != 1 {
		t.Errorf("s1 expected 1 message, got %d", len(s1.received))
	}
}

func TestBroadcast_SkipsUnauthenticated(t *testing.T) {
	b := NewBroadcaster()
	s1 := &mockSubscriber{id: "s1", authed: false}
	s2 := &mockSubscriber{id: "s2", authed: true}

	b.Subscribe(s1, Filter{})
	b.Subscribe(s2, Filter{})

	sent, _ := b.Broadcast("test", nil)
	if sent != 1 {
		t.Errorf("expected 1 sent, got %d", sent)
	}
	if len(s1.received) != 0 {
		t.Error("unauthed subscriber should not receive events")
	}
}

func TestBroadcast_Filter(t *testing.T) {
	b := NewBroadcaster()
	s1 := &mockSubscriber{id: "s1", authed: true}

	b.Subscribe(s1, Filter{Events: map[string]struct{}{"wanted": {}}})

	// Event that matches filter.
	sent, _ := b.Broadcast("wanted", nil)
	if sent != 1 {
		t.Errorf("expected 1 sent for matching event, got %d", sent)
	}

	// Event that doesn't match filter.
	sent, _ = b.Broadcast("unwanted", nil)
	if sent != 0 {
		t.Errorf("expected 0 sent for non-matching event, got %d", sent)
	}
}

func TestBroadcast_SendError(t *testing.T) {
	b := NewBroadcaster()
	s1 := &mockSubscriber{id: "s1", authed: true, failSend: true}

	b.Subscribe(s1, Filter{})

	sent, errs := b.Broadcast("test", nil)
	if sent != 0 {
		t.Errorf("expected 0 sent, got %d", sent)
	}
	if len(errs) != 1 {
		t.Errorf("expected 1 error, got %d", len(errs))
	}
}

func TestUnsubscribe(t *testing.T) {
	b := NewBroadcaster()
	s1 := &mockSubscriber{id: "s1", authed: true}

	b.Subscribe(s1, Filter{})
	if b.Count() != 1 {
		t.Errorf("expected count 1, got %d", b.Count())
	}

	b.Unsubscribe("s1")
	if b.Count() != 0 {
		t.Errorf("expected count 0, got %d", b.Count())
	}
}

func TestBroadcastRaw(t *testing.T) {
	b := NewBroadcaster()
	s1 := &mockSubscriber{id: "s1", authed: true}

	b.Subscribe(s1, Filter{})

	data := []byte(`{"type":"event","event":"raw","payload":{}}`)
	sent := b.BroadcastRaw("raw", data)
	if sent != 1 {
		t.Errorf("expected 1 sent, got %d", sent)
	}
	if len(s1.received) != 1 {
		t.Error("expected subscriber to receive raw data")
	}
}

func TestSequenceIncrement(t *testing.T) {
	b := NewBroadcaster()
	s := &mockSubscriber{id: "s1", authed: true}
	b.Subscribe(s, Filter{})

	b.Broadcast("e1", nil)
	b.Broadcast("e2", nil)
	b.Broadcast("e3", nil)

	if len(s.received) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(s.received))
	}
}

// --- Phase 2 tests: targeted broadcast, session subscriptions ---

func TestBroadcastToConnIDs(t *testing.T) {
	b := NewBroadcaster()
	s1 := &mockSubscriber{id: "s1", authed: true}
	s2 := &mockSubscriber{id: "s2", authed: true}
	b.Subscribe(s1, Filter{})
	b.Subscribe(s2, Filter{})

	targets := map[string]struct{}{"s1": {}}
	sent, _ := b.BroadcastToConnIDs("test", nil, targets)
	if sent != 1 {
		t.Errorf("expected 1 targeted send, got %d", sent)
	}
	if len(s1.received) != 1 {
		t.Error("s1 should receive targeted event")
	}
	if len(s2.received) != 0 {
		t.Error("s2 should not receive targeted event")
	}
}

func TestSessionEventSubscriptions(t *testing.T) {
	b := NewBroadcaster()

	b.SubscribeSessionEvents("conn-1")
	b.SubscribeSessionEvents("conn-2")

	subs := b.SessionEventSubscriberConnIDs()
	if len(subs) != 2 {
		t.Errorf("expected 2 session subs, got %d", len(subs))
	}

	b.UnsubscribeSessionEvents("conn-1")
	subs = b.SessionEventSubscriberConnIDs()
	if len(subs) != 1 {
		t.Errorf("expected 1 session sub after unsubscribe, got %d", len(subs))
	}
}

func TestSessionMessageSubscriptions(t *testing.T) {
	b := NewBroadcaster()

	b.SubscribeSessionMessageEvents("conn-1", "session-abc")
	b.SubscribeSessionMessageEvents("conn-2", "session-abc")
	b.SubscribeSessionMessageEvents("conn-1", "session-xyz")

	subs := b.SessionMessageSubscriberConnIDs("session-abc")
	if len(subs) != 2 {
		t.Errorf("expected 2 subs for session-abc, got %d", len(subs))
	}
	subs = b.SessionMessageSubscriberConnIDs("session-xyz")
	if len(subs) != 1 {
		t.Errorf("expected 1 sub for session-xyz, got %d", len(subs))
	}

	b.UnsubscribeSessionMessageEvents("conn-1", "session-abc")
	subs = b.SessionMessageSubscriberConnIDs("session-abc")
	if len(subs) != 1 {
		t.Errorf("expected 1 sub after unsubscribe, got %d", len(subs))
	}
}

func TestToolEventRecipients(t *testing.T) {
	b := NewBroadcaster()

	b.RegisterToolEventRecipient("run-1", "conn-1")
	if got := b.ToolEventRecipient("run-1"); got != "conn-1" {
		t.Errorf("expected conn-1, got %q", got)
	}

	b.UnregisterToolEventRecipient("run-1")
	if got := b.ToolEventRecipient("run-1"); got != "" {
		t.Errorf("expected empty after unregister, got %q", got)
	}
}

func TestUnsubscribe_CleansUpSessionSubs(t *testing.T) {
	b := NewBroadcaster()
	s := &mockSubscriber{id: "conn-1", authed: true}
	b.Subscribe(s, Filter{})

	b.SubscribeSessionEvents("conn-1")
	b.SubscribeSessionMessageEvents("conn-1", "session-abc")

	b.Unsubscribe("conn-1")

	if subs := b.SessionEventSubscriberConnIDs(); len(subs) != 0 {
		t.Error("session subs should be cleaned up on Unsubscribe")
	}
	if subs := b.SessionMessageSubscriberConnIDs("session-abc"); len(subs) != 0 {
		t.Error("session message subs should be cleaned up on Unsubscribe")
	}
}
