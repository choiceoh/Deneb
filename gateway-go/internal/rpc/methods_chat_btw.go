package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ChatBtwDeps holds the dependencies for the chat.btw RPC method.
type ChatBtwDeps struct {
	Forwarder   Forwarder
	Broadcaster BroadcastFunc
}

// RegisterChatBtwMethods registers the /btw side-question RPC method.
func RegisterChatBtwMethods(d *Dispatcher, deps ChatBtwDeps) {
	d.Register("chat.btw", handleChatBtw(deps))
}

// handleChatBtw processes a side question without affecting the main session
// context. The question is forwarded to the TypeScript runtime which handles
// LLM interaction, transcript branching, and response generation.
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

		// Forward to TypeScript runtime via bridge.
		// The TS side handles: session transcript branching, LLM call with
		// thinking=off, tool-less response generation.
		if deps.Forwarder == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "bridge not available"))
		}

		bridgeReq, err := protocol.NewRequestFrame(req.ID+"-btw", "chat.btw", map[string]any{
			"question":   p.Question,
			"sessionKey": p.SessionKey,
		})
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "failed to build bridge request: "+err.Error()))
		}

		bridgeResp, err := deps.Forwarder.Forward(ctx, bridgeReq)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "btw bridge call failed: "+err.Error()))
		}

		// Broadcast side_result event to connected clients.
		if bridgeResp != nil && bridgeResp.OK && deps.Broadcaster != nil {
			// Extract text from the bridge response payload for the event.
			var result map[string]any
			if len(bridgeResp.Payload) > 0 {
				_ = json.Unmarshal(bridgeResp.Payload, &result)
			}
			text, _ := result["text"].(string)
			ts := result["ts"]
			deps.Broadcaster("chat.side_result", map[string]any{
				"kind":       "btw",
				"sessionKey": p.SessionKey,
				"question":   p.Question,
				"text":       text,
				"ts":         ts,
			})
		}

		// Re-wrap response with the original request ID.
		if bridgeResp != nil {
			bridgeResp.ID = req.ID
		}
		return bridgeResp
	}
}
