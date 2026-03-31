package streaming

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/channel"
)

// mockChannelPlugin is a minimal channel.Plugin + MessagingAdapter for testing.
type mockChannelPlugin struct {
	id       string
	sendErr  error
	mu       sync.Mutex
	sentMsgs []channel.OutboundMessage
}

func (m *mockChannelPlugin) ID() string                         { return m.id }
func (m *mockChannelPlugin) Meta() channel.Meta                 { return channel.Meta{} }
func (m *mockChannelPlugin) Capabilities() channel.Capabilities { return channel.Capabilities{} }
func (m *mockChannelPlugin) Start(_ context.Context) error      { return nil }
func (m *mockChannelPlugin) Stop(_ context.Context) error       { return nil }
func (m *mockChannelPlugin) Status() channel.Status             { return channel.Status{} }

func (m *mockChannelPlugin) SendMessage(_ context.Context, msg channel.OutboundMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentMsgs = append(m.sentMsgs, msg)
	return m.sendErr
}

// nonMessagingPlugin implements Plugin but NOT MessagingAdapter.
type nonMessagingPlugin struct {
	id string
}

func (p *nonMessagingPlugin) ID() string                         { return p.id }
func (p *nonMessagingPlugin) Meta() channel.Meta                 { return channel.Meta{} }
func (p *nonMessagingPlugin) Capabilities() channel.Capabilities { return channel.Capabilities{} }
func (p *nonMessagingPlugin) Start(_ context.Context) error      { return nil }
func (p *nonMessagingPlugin) Stop(_ context.Context) error       { return nil }
func (p *nonMessagingPlugin) Status() channel.Status             { return channel.Status{} }

func TestDispatch_SingleTarget(t *testing.T) {
	reg := channel.NewRegistry()
	plugin := &mockChannelPlugin{id: "telegram"}
	reg.Register(plugin)

	results := Dispatch(context.Background(), reg,
		[]DeliveryTarget{{Channel: "telegram", To: "user123", ReplyTo: "msg1"}},
		"Hello!", nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Delivered {
		t.Errorf("expected delivered, error: %v", results[0].Error)
	}
	if len(plugin.sentMsgs) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(plugin.sentMsgs))
	}
	if plugin.sentMsgs[0].To != "user123" {
		t.Errorf("To = %q", plugin.sentMsgs[0].To)
	}
	if plugin.sentMsgs[0].Text != "Hello!" {
		t.Errorf("Text = %q", plugin.sentMsgs[0].Text)
	}
}

func TestDispatch_MultipleTargets(t *testing.T) {
	reg := channel.NewRegistry()
	tg := &mockChannelPlugin{id: "telegram"}
	dc := &mockChannelPlugin{id: "telegram"}
	reg.Register(tg)
	reg.Register(dc)

	results := Dispatch(context.Background(), reg,
		[]DeliveryTarget{
			{Channel: "telegram", To: "user1"},
			{Channel: "telegram", To: "user2"},
		},
		"Multi!", nil)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for i, r := range results {
		if !r.Delivered {
			t.Errorf("result[%d] not delivered: %v", i, r.Error)
		}
	}
}

func TestDispatch_ChannelNotFound(t *testing.T) {
	reg := channel.NewRegistry()

	results := Dispatch(context.Background(), reg,
		[]DeliveryTarget{{Channel: "missing", To: "user1"}},
		"Hello!", nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Delivered {
		t.Error("expected not delivered for missing channel")
	}
	if results[0].Error == nil {
		t.Error("expected error for missing channel")
	}
}

func TestDispatch_PartialFailure(t *testing.T) {
	reg := channel.NewRegistry()
	ok := &mockChannelPlugin{id: "telegram"}
	fail := &mockChannelPlugin{id: "slack", sendErr: fmt.Errorf("send failed")}
	reg.Register(ok)
	reg.Register(fail)

	results := Dispatch(context.Background(), reg,
		[]DeliveryTarget{
			{Channel: "telegram", To: "user1"},
			{Channel: "slack", To: "user2"},
		},
		"Test", nil)

	// One should succeed, one should fail.
	var delivered, failed int
	for _, r := range results {
		if r.Delivered {
			delivered++
		} else {
			failed++
		}
	}
	if delivered != 1 || failed != 1 {
		t.Errorf("delivered=%d failed=%d, want 1/1", delivered, failed)
	}
}

func TestDispatch_NonMessagingChannel(t *testing.T) {
	reg := channel.NewRegistry()
	reg.Register(&nonMessagingPlugin{id: "readonly"})

	results := Dispatch(context.Background(), reg,
		[]DeliveryTarget{{Channel: "readonly", To: "user1"}},
		"Hello!", nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Delivered {
		t.Error("expected not delivered for non-messaging channel")
	}
}

func TestDispatch_EmptyTargets(t *testing.T) {
	reg := channel.NewRegistry()
	results := Dispatch(context.Background(), reg, nil, "Hello!", nil)
	if results != nil {
		t.Errorf("expected nil results, got %v", results)
	}
}
