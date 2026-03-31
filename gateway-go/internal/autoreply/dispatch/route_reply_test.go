package dispatch

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/chunk"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

func TestRouteReply_NilPlugin(t *testing.T) {
	err := RouteReply(context.Background(), nil, "telegram", "user-1", types.ReplyPayload{
		Text: "hello",
	}, 4096, chunk.ModeNewline)
	if err == nil {
		t.Fatal("expected error for nil plugin")
	}
}

func TestRouteReply_NonTelegramChannel(t *testing.T) {
	err := RouteReply(context.Background(), nil, "slack", "user-1", types.ReplyPayload{
		Text: "hello",
	}, 4096, chunk.ModeNewline)
	if err == nil {
		t.Fatal("expected error for non-telegram channel")
	}
}
