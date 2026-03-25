package rpc

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ChatBtwDeps holds the dependencies for the chat.btw RPC method.
type ChatBtwDeps struct {
	// Chat is the native chat handler for processing side questions.
	Chat interface {
		// HandleBtw processes a side question and returns the answer text.
		// Returns empty string if the handler doesn't support /btw yet.
		HandleBtw(ctx context.Context, sessionKey, question string) (string, error)
	}
	Broadcaster BroadcastFunc
}

// RegisterChatBtwMethods registers the /btw side-question RPC method.
func RegisterChatBtwMethods(d *Dispatcher, deps ChatBtwDeps) {
	d.Register("chat.btw", handleChatBtw(deps))
}

// handleChatBtw processes a side question without affecting the main session
// context. In native Go mode, this routes through the chat handler directly.
//
// Params:
//   - question (string, required): The side question to answer.
//   - sessionKey (string, required): The active session key.
//
// Returns the side question answer text, and broadcasts a chat.side_result event.
func handleChatBtw(deps ChatBtwDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Question   string `json:"question"`
			SessionKey string `json:"sessionKey"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "params required"))
		}
		if p.Question == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "question is required"))
		}
		if p.SessionKey == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "sessionKey is required"))
		}

		if deps.Chat == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "chat handler not available"))
		}

		// Process side question through native chat handler.
		text, err := deps.Chat.HandleBtw(ctx, p.SessionKey, p.Question)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "btw failed: "+err.Error()))
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
