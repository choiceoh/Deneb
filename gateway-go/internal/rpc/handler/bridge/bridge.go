// Package bridge provides the bridge.send RPC handler for inter-agent
// communication. It broadcasts lightweight messages to WebSocket clients
// (including the MCP server) without triggering LLM inference.
package bridge

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps holds dependencies for bridge RPC handlers.
type Deps struct {
	Broadcaster rpcutil.BroadcastFunc
}

// Methods returns the bridge RPC handlers.
// Returns nil if Broadcaster is not configured.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	if deps.Broadcaster == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"bridge.send": bridgeSend(deps),
	}
}

// bridgeSend broadcasts a bridge.message event to all WebSocket clients.
// This is a lightweight path — no session creation, no LLM inference.
func bridgeSend(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Message string `json:"message"`
			Source  string `json:"source,omitempty"` // sender identity (e.g., "main-agent", "claude-code")
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Message == "" {
			return rpcerr.MissingParam("message").Response(req.ID)
		}
		if p.Source == "" {
			p.Source = "gateway"
		}

		payload := map[string]any{
			"message": p.Message,
			"source":  p.Source,
			"ts":      time.Now().UnixMilli(),
		}

		sent, _ := deps.Broadcaster("bridge.message", payload)
		return rpcutil.RespondOK(req.ID, map[string]any{
			"sent": sent,
			"ts":   payload["ts"],
		})
	}
}
