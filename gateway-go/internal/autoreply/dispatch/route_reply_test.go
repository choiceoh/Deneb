package dispatch

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/chunk"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/channel"
)

type fakeMessagingPlugin struct {
	id   string
	sent []channel.OutboundMessage
}

func (f *fakeMessagingPlugin) ID() string                         { return f.id }
func (f *fakeMessagingPlugin) Meta() channel.Meta                 { return channel.Meta{ID: f.id, Label: f.id} }
func (f *fakeMessagingPlugin) Capabilities() channel.Capabilities { return channel.Capabilities{} }
func (f *fakeMessagingPlugin) Start(context.Context) error        { return nil }
func (f *fakeMessagingPlugin) Stop(context.Context) error         { return nil }
func (f *fakeMessagingPlugin) Status() channel.Status             { return channel.Status{Connected: true} }
func (f *fakeMessagingPlugin) SendMessage(_ context.Context, msg channel.OutboundMessage) error {
	f.sent = append(f.sent, msg)
	return nil
}

func TestRouteReplyChunksAndAppliesMediaOnLastChunk(t *testing.T) {
	reg := channel.NewRegistry()
	p := &fakeMessagingPlugin{id: "test"}
	if err := reg.Register(p); err != nil {
		t.Fatalf("register plugin: %v", err)
	}

	err := RouteReply(context.Background(), reg, "test", "user-1", types.ReplyPayload{
		Text:      "1234567890",
		ReplyToID: "origin",
		MediaURL:  "https://example.com/m.png",
	}, 4, chunk.ModeNewline)
	if err != nil {
		t.Fatalf("route reply: %v", err)
	}
	if len(p.sent) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(p.sent))
	}
	if p.sent[0].ReplyTo != "origin" {
		t.Fatalf("expected first chunk replyTo to be set")
	}
	if p.sent[1].ReplyTo != "" || p.sent[2].ReplyTo != "" {
		t.Fatalf("expected non-first chunks to clear replyTo")
	}
	if len(p.sent[0].Media) != 0 || len(p.sent[1].Media) != 0 {
		t.Fatalf("expected media only on last chunk")
	}
	if len(p.sent[2].Media) != 1 || p.sent[2].Media[0] != "https://example.com/m.png" {
		t.Fatalf("unexpected last chunk media: %#v", p.sent[2].Media)
	}
}
