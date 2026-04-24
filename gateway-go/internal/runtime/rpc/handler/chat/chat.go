// Package chat provides RPC method handlers for the chat domain.
//
// It exposes Methods and BtwMethods, which return handler maps that can be
// bulk-registered on the rpc.Dispatcher.
package chat

import (
	"context"

	chatpkg "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// BroadcastFunc is the canonical broadcast type defined in rpcutil.
type BroadcastFunc = rpcutil.BroadcastFunc

// Deps holds the dependencies for standard chat RPC methods (send, history, abort, steer).
type Deps struct {
	Chat        *chatpkg.Handler
	Broadcaster BroadcastFunc // optional; receives chat.steer_received events
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
		"chat.steer":   handleSteer(deps),
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

// handleSteer queues a /steer note for the main agent's next tool_result
// without interrupting the active run.
//
// Params:
//   - sessionKey (string, required): target session
//   - note       (string, required): user nudge text (trimmed)
//
// On accept, broadcasts "chat.steer_received" so the UI can surface the
// pending nudge. The note is drained and injected by the running agent
// goroutine right before its next LLM call.
func handleSteer(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			SessionKey string `json:"sessionKey"`
			Note       string `json:"note"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.SessionKey == "" {
			return rpcerr.MissingParam("sessionKey").Response(req.ID)
		}
		if p.Note == "" {
			return rpcerr.MissingParam("note").Response(req.ID)
		}
		if deps.Chat == nil {
			return rpcerr.Unavailable("chat handler not available").Response(req.ID)
		}
		accepted := deps.Chat.EnqueueSteer(p.SessionKey, p.Note)
		if !accepted {
			// Empty after trim, or queue unavailable. Surface as invalid
			// rather than silently swallowing so the caller notices.
			return rpcerr.InvalidRequest("steer note is empty").Response(req.ID)
		}
		if deps.Broadcaster != nil {
			_, _ = deps.Broadcaster("chat.steer_received", map[string]any{
				"sessionKey": p.SessionKey,
				"note":       p.Note,
			})
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":         true,
			"sessionKey": p.SessionKey,
		})
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
		p, errResp := rpcutil.DecodeParams[struct {
			Question   string `json:"question"`
			SessionKey string `json:"sessionKey"`
		}](req)
		if errResp != nil {
			return errResp
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
			return rpcerr.WrapDependencyFailed("btw failed", err).Response(req.ID)
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

		return rpcutil.RespondOK(req.ID, map[string]any{
			"text": text,
		})
	}
}
