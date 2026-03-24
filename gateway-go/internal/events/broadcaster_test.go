package events

import (
	"sync"
	"testing"
)

type mockSubscriber struct {
	id        string
	authed    bool
	mu        sync.Mutex
	received  [][]byte
	failSend  bool
}

func (m *mockSubscriber) ID() string              { return m.id }
func (m *mockSubscriber) IsAuthenticated() bool    { return m.authed }
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

	b.Subscribe(s1, Filter{Events: map[string]bool{"wanted": true}})

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
