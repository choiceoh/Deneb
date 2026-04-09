package events

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

func newTestGatewaySubscriptions() (*Broadcaster, *GatewayEventSubscriptions) {
	b := NewBroadcaster()
	b.SetLogger(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	gs := NewGatewayEventSubscriptions(GatewaySubscriptionParams{
		Broadcaster: b,
		Logger:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	return b, gs
}

func TestGatewaySubscriptions_EmitHeartbeat(t *testing.T) {
	b, gs := newTestGatewaySubscriptions()
	defer gs.Stop()

	sub := &mockSubscriber{id: "s1", authed: true}
	b.Subscribe(sub, Filter{})

	gs.EmitHeartbeat(HeartbeatEvent{Ts: time.Now().UnixMilli()})
	time.Sleep(50 * time.Millisecond)

	sub.mu.Lock()
	count := len(sub.received)
	sub.mu.Unlock()
	if count != 1 {
		t.Errorf("got %d, want 1 heartbeat event", count)
	}
}

func TestGatewaySubscriptions_EmitAgent(t *testing.T) {
	b, gs := newTestGatewaySubscriptions()
	defer gs.Stop()

	sub := &mockSubscriber{id: "s1", authed: true}
	b.Subscribe(sub, Filter{})

	gs.EmitAgent(AgentEvent{Kind: "tool.start", SessionKey: "s1"})
	time.Sleep(50 * time.Millisecond)

	sub.mu.Lock()
	count := len(sub.received)
	sub.mu.Unlock()
	if count != 1 {
		t.Errorf("got %d, want 1 agent event", count)
	}
}

func TestGatewaySubscriptions_EmitLifecycle(t *testing.T) {
	b, gs := newTestGatewaySubscriptions()
	defer gs.Stop()

	sub := &mockSubscriber{id: "s1", authed: true}
	b.Subscribe(sub, Filter{})
	b.SubscribeSessionEvents("s1")

	gs.EmitLifecycle(LifecycleChangeEvent{
		SessionKey: "session-1",
		Reason:     "completed",
	})
	time.Sleep(50 * time.Millisecond)

	sub.mu.Lock()
	count := len(sub.received)
	sub.mu.Unlock()
	if count != 1 {
		t.Errorf("got %d, want 1 lifecycle event", count)
	}
}

func TestGatewaySubscriptions_EmitTranscript(t *testing.T) {
	b, gs := newTestGatewaySubscriptions()
	defer gs.Stop()

	sub := &mockSubscriber{id: "s1", authed: true}
	b.Subscribe(sub, Filter{})
	b.SubscribeSessionMessageEvents("s1", "session-x")

	gs.EmitTranscript(TranscriptUpdate{
		SessionKey: "session-x",
		MessageID:  "msg-1",
		Message:    map[string]string{"role": "assistant", "content": "hello"},
	})
	time.Sleep(50 * time.Millisecond)

	sub.mu.Lock()
	count := len(sub.received)
	sub.mu.Unlock()
	if count != 1 {
		t.Errorf("got %d, want 1 transcript event", count)
	}
}

func TestGatewaySubscriptions_EmitTranscript_WithSessionEventSub(t *testing.T) {
	b, gs := newTestGatewaySubscriptions()
	defer gs.Stop()

	sub := &mockSubscriber{id: "s1", authed: true}
	b.Subscribe(sub, Filter{})
	b.SubscribeSessionEvents("s1")
	b.SubscribeSessionMessageEvents("s1", "session-x")

	gs.EmitTranscript(TranscriptUpdate{
		SessionKey: "session-x",
		MessageID:  "msg-1",
		Message:    map[string]string{"role": "assistant", "content": "hello"},
	})
	time.Sleep(50 * time.Millisecond)

	sub.mu.Lock()
	count := len(sub.received)
	sub.mu.Unlock()
	// Should receive both session.message and sessions.changed.
	if count != 2 {
		t.Errorf("got %d, want 2 events (session.message + sessions.changed)", count)
	}
}

func TestGatewaySubscriptions_Stop(t *testing.T) {
	_, gs := newTestGatewaySubscriptions()
	gs.Stop()
	gs.Stop() // Double stop should not panic.
}
