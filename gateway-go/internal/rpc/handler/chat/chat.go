// Package chat provides RPC method handlers for the chat domain.
//
// It exposes Methods and BtwMethods, which return handler maps that can be
// bulk-registered on the rpc.Dispatcher.
package chat

import (
	"context"

	chatpkg "github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// BroadcastFunc is the signature for broadcasting events to connected clients.
type BroadcastFunc func(event string, payload any) (int, []error)

// Deps holds the dependencies for standard chat RPC methods (send, history,
// abort, inject).
type Deps struct {
	Chat *chatpkg.Handler
}

// BtwDeps holds the dependencies for the chat.btw side-question RPC method.
type BtwDeps struct {
	// Chat is the native chat handler for processing side questions.
	Chat interface {
		// HandleBtw processes a side question and returns the answer text.
		// Returns empty string if the handler doesn't support /btw yet.
		HandleBtw(ctx context.Context, sessionKey, question string) (string, error)
	}
	Broadcaster BroadcastFunc
}

// Methods returns the standard chat RPC handlers keyed by method name.
// If deps.Chat is nil, an empty map is returned.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	if deps.Chat == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"chat.send":    handleSend(deps),
		"chat.history": handleHistory(deps),
		"chat.abort":   handleAbort(deps),
		"chat.inject":  handleInject(deps),
	}
}

// BtwMethods returns the chat.btw side-question RPC handler keyed by method name.
func BtwMethods(deps BtwDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"chat.btw": handleChatBtw(deps),
	}
}

// handleSend delegates to the chat handler's Send method.
func handleSend(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return deps.Chat.Send(ctx, req)
	}
}

// handleHistory delegates to the chat handler's History method.
func handleHistory(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return deps.Chat.History(ctx, req)
	}
}

// handleAbort delegates to the chat handler's Abort method.
func handleAbort(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return deps.Chat.Abort(ctx, req)
	}
}

// handleInject delegates to the chat handler's Inject method.
func handleInject(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return deps.Chat.Inject(ctx, req)
	}
}

// handleChatBtw processes a side question without affecting the main session
// context. In native Go mode, this routes through the chat handler directly.
//
// Params:
//   - question (string, required): The side question to answer.
//   - sessionKey (string, required): The active session key.
//
// Returns the side question answer text, and broadcasts a chat.side_result event.
func handleChatBtw(deps BtwDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Question   string `json:"question"`
			SessionKey string `json:"sessionKey"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return rpcerr.New(protocol.ErrInvalidRequest, "params required").Response(req.ID)
		}
		if p.Question == "" {
			return rpcerr.MissingParam("question").Response(req.ID)
		}
		if p.SessionKey == "" {
			return rpcerr.MissingParam("sessionKey").Response(req.ID)
		}

		if deps.Chat == nil {
			return rpcerr.Unavailable("chat handler not available").Response(req.ID)
		}

		// Process side question through native chat handler.
		text, err := deps.Chat.HandleBtw(ctx, p.SessionKey, p.Question)
		if err != nil {
			return rpcerr.New(protocol.ErrDependencyFailed, "btw failed: "+err.Error()).Response(req.ID)
		}

		// Broadcast side_result event to connected clients.
		if deps.Broadcaster != nil {
			_, _ = deps.Broadcaster("chat.side_result", map[string]any{
				"kind":       "btw",
				"sessionKey": p.SessionKey,
				"question":   p.Question,
				"text":       text,
			})
		}

		return protocol.MustResponseOK(req.ID, map[string]any{
			"text": text,
		})
	}
}
