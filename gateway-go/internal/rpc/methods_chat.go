package rpc

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ChatDeps holds the dependencies for chat RPC methods.
type ChatDeps struct {
	Chat *chat.Handler
}

// RegisterChatMethods registers the chat.send, chat.history, chat.abort, and chat.inject RPC methods.
func RegisterChatMethods(d *Dispatcher, deps ChatDeps) {
	if deps.Chat == nil {
		return
	}
	d.Register("chat.send", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return deps.Chat.Send(ctx, req)
	})
	d.Register("chat.history", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return deps.Chat.History(ctx, req)
	})
	d.Register("chat.abort", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return deps.Chat.Abort(ctx, req)
	})
	d.Register("chat.inject", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return deps.Chat.Inject(ctx, req)
	})
}
