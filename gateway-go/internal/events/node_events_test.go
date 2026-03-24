package events

import (
	"log/slog"
	"os"
	"testing"
)

func newTestBroadcasterForNode() *Broadcaster {
	b := NewBroadcaster()
	b.SetLogger(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	return b
}

func newTestNodeCtx() *NodeEventContext {
	return &NodeEventContext{
		Broadcaster: newTestBroadcasterForNode(),
		Logger:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}
}

func TestHandleNodeEvent_VoiceTranscript(t *testing.T) {
	ctx := newTestNodeCtx()
	sub := &mockSubscriber{id: "s1", authed: true, role: "operator", scopes: []string{"read", "write", "admin"}}
	ctx.Broadcaster.Subscribe(sub, Filter{})

	HandleNodeEvent(ctx, "node-1", NodeEvent{
		Event:       "voice.transcript",
		PayloadJSON: `{"text": "hello world", "sessionKey": "test-session"}`,
	})

	sub.mu.Lock()
	count := len(sub.received)
	sub.mu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 broadcast, got %d", count)
	}
}

func TestHandleNodeEvent_VoiceTranscript_EmptyText(t *testing.T) {
	ctx := newTestNodeCtx()
	sub := &mockSubscriber{id: "s1", authed: true, role: "operator", scopes: []string{"read"}}
	ctx.Broadcaster.Subscribe(sub, Filter{})

	HandleNodeEvent(ctx, "node-1", NodeEvent{
		Event:       "voice.transcript",
		PayloadJSON: `{"text": "  "}`,
	})

	sub.mu.Lock()
	count := len(sub.received)
	sub.mu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 broadcasts for empty text, got %d", count)
	}
}

func TestHandleNodeEvent_VoiceTranscript_Dedup(t *testing.T) {
	ctx := newTestNodeCtx()
	sub := &mockSubscriber{id: "s1", authed: true, role: "operator", scopes: []string{"read"}}
	ctx.Broadcaster.Subscribe(sub, Filter{})

	evt := NodeEvent{
		Event:       "voice.transcript",
		PayloadJSON: `{"text": "hello", "eventId": "evt-dedup-test"}`,
	}
	HandleNodeEvent(ctx, "node-1", evt)
	HandleNodeEvent(ctx, "node-1", evt) // duplicate

	sub.mu.Lock()
	count := len(sub.received)
	sub.mu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 broadcast (deduped), got %d", count)
	}
}

func TestHandleNodeEvent_AgentRequest(t *testing.T) {
	ctx := newTestNodeCtx()
	sub := &mockSubscriber{id: "s1", authed: true, role: "operator", scopes: []string{"read"}}
	ctx.Broadcaster.Subscribe(sub, Filter{})

	HandleNodeEvent(ctx, "node-1", NodeEvent{
		Event:       "agent.request",
		PayloadJSON: `{"message": "do something", "sessionKey": "s1"}`,
	})

	sub.mu.Lock()
	count := len(sub.received)
	sub.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 broadcast, got %d", count)
	}
}

func TestHandleNodeEvent_AgentRequest_EmptyMessage(t *testing.T) {
	ctx := newTestNodeCtx()
	sub := &mockSubscriber{id: "s1", authed: true, role: "operator", scopes: []string{"read"}}
	ctx.Broadcaster.Subscribe(sub, Filter{})

	HandleNodeEvent(ctx, "node-1", NodeEvent{
		Event:       "agent.request",
		PayloadJSON: `{"message": ""}`,
	})

	sub.mu.Lock()
	count := len(sub.received)
	sub.mu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 broadcasts for empty message, got %d", count)
	}
}

func TestHandleNodeEvent_NotificationsChanged(t *testing.T) {
	ctx := newTestNodeCtx()
	sub := &mockSubscriber{id: "s1", authed: true, role: "operator", scopes: []string{"read"}}
	ctx.Broadcaster.Subscribe(sub, Filter{})

	HandleNodeEvent(ctx, "node-1", NodeEvent{
		Event:       "notifications.changed",
		PayloadJSON: `{"change": "posted", "key": "k1", "title": "New msg", "text": "Hello"}`,
	})

	sub.mu.Lock()
	count := len(sub.received)
	sub.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 broadcast, got %d", count)
	}
}

func TestHandleNodeEvent_NotificationsChanged_InvalidChange(t *testing.T) {
	ctx := newTestNodeCtx()
	sub := &mockSubscriber{id: "s1", authed: true, role: "operator", scopes: []string{"read"}}
	ctx.Broadcaster.Subscribe(sub, Filter{})

	HandleNodeEvent(ctx, "node-1", NodeEvent{
		Event:       "notifications.changed",
		PayloadJSON: `{"change": "unknown", "key": "k1"}`,
	})

	sub.mu.Lock()
	count := len(sub.received)
	sub.mu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 broadcasts for invalid change, got %d", count)
	}
}

func TestHandleNodeEvent_ExecStarted(t *testing.T) {
	ctx := newTestNodeCtx()
	sub := &mockSubscriber{id: "s1", authed: true, role: "operator", scopes: []string{"read"}}
	ctx.Broadcaster.Subscribe(sub, Filter{})

	HandleNodeEvent(ctx, "node-1", NodeEvent{
		Event:       "exec.started",
		PayloadJSON: `{"runId": "r1", "command": "ls -la"}`,
	})

	sub.mu.Lock()
	count := len(sub.received)
	sub.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 broadcast, got %d", count)
	}
}

func TestHandleNodeEvent_ExecFinished_SuccessQuiet(t *testing.T) {
	ctx := newTestNodeCtx()
	sub := &mockSubscriber{id: "s1", authed: true, role: "operator", scopes: []string{"read"}}
	ctx.Broadcaster.Subscribe(sub, Filter{})

	HandleNodeEvent(ctx, "node-1", NodeEvent{
		Event:       "exec.finished",
		PayloadJSON: `{"exitCode": 0, "output": ""}`,
	})

	sub.mu.Lock()
	count := len(sub.received)
	sub.mu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 broadcasts for quiet success, got %d", count)
	}
}

func TestHandleNodeEvent_ExecFinished_NonZeroNotifies(t *testing.T) {
	ctx := newTestNodeCtx()
	sub := &mockSubscriber{id: "s1", authed: true, role: "operator", scopes: []string{"read"}}
	ctx.Broadcaster.Subscribe(sub, Filter{})

	HandleNodeEvent(ctx, "node-1", NodeEvent{
		Event:       "exec.finished",
		PayloadJSON: `{"exitCode": 1, "output": "error occurred"}`,
	})

	sub.mu.Lock()
	count := len(sub.received)
	sub.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 broadcast, got %d", count)
	}
}

func TestHandleNodeEvent_ChatSubscribe(t *testing.T) {
	ctx := newTestNodeCtx()

	HandleNodeEvent(ctx, "node-1", NodeEvent{
		Event:       "chat.subscribe",
		PayloadJSON: `{"sessionKey": "s1"}`,
	})

	keys := ctx.Broadcaster.NodeSessionKeys("node-1")
	if len(keys) != 1 || keys[0] != "s1" {
		t.Errorf("expected node session key 's1', got %v", keys)
	}
}

func TestHandleNodeEvent_ChatUnsubscribe(t *testing.T) {
	ctx := newTestNodeCtx()

	HandleNodeEvent(ctx, "node-1", NodeEvent{
		Event:       "chat.subscribe",
		PayloadJSON: `{"sessionKey": "s1"}`,
	})
	HandleNodeEvent(ctx, "node-1", NodeEvent{
		Event:       "chat.unsubscribe",
		PayloadJSON: `{"sessionKey": "s1"}`,
	})

	keys := ctx.Broadcaster.NodeSessionKeys("node-1")
	if len(keys) != 0 {
		t.Fatalf("expected 0 session keys after unsub, got %d", len(keys))
	}
}

func TestHandleNodeEvent_UnknownEventIgnored(t *testing.T) {
	ctx := newTestNodeCtx()
	sub := &mockSubscriber{id: "s1", authed: true, role: "operator", scopes: []string{"read"}}
	ctx.Broadcaster.Subscribe(sub, Filter{})

	HandleNodeEvent(ctx, "node-1", NodeEvent{
		Event:       "unknown.event",
		PayloadJSON: `{}`,
	})

	sub.mu.Lock()
	count := len(sub.received)
	sub.mu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 broadcasts for unknown event, got %d", count)
	}
}

func TestCompactText(t *testing.T) {
	tests := []struct {
		input  string
		max    int
		expect string
	}{
		{"", 100, ""},
		{"hello", 100, "hello"},
		{"hello world", 5, "hell…"},
		{"  spaces   everywhere  ", 100, "spaces everywhere"},
	}
	for _, tt := range tests {
		got := compactText(tt.input, tt.max)
		if got != tt.expect {
			t.Errorf("compactText(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.expect)
		}
	}
}

func TestResolveVoiceTranscriptFingerprint(t *testing.T) {
	tests := []struct {
		name   string
		obj    map[string]any
		text   string
		expect string
	}{
		{"eventId", map[string]any{"eventId": "e1"}, "hello", "event:e1"},
		{"callId+seq", map[string]any{"callId": "c1", "sequence": float64(3)}, "hello", "call-seq:c1:3"},
		{"callId+timestamp", map[string]any{"callId": "c1", "timestamp": float64(1234)}, "hello", "call-ts:c1:1234"},
		{"timestamp+text", map[string]any{"timestamp": float64(1234)}, "hello", "timestamp:1234|text:hello"},
		{"text only", map[string]any{}, "hello", "text:hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveVoiceTranscriptFingerprint(tt.obj, tt.text)
			if got != tt.expect {
				t.Errorf("got %q, want %q", got, tt.expect)
			}
		})
	}
}
