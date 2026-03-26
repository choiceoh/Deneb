package agent

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/copilot"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// CopilotDeps holds the dependencies for copilot RPC methods.
type CopilotDeps struct {
	Copilot *copilot.Service
}

// CopilotMethods returns the copilot.* handlers.
func CopilotMethods(deps CopilotDeps) map[string]rpcutil.HandlerFunc {
	if deps.Copilot == nil {
		return nil
	}

	return map[string]rpcutil.HandlerFunc{
		"copilot.status":  copilotStatus(deps),
		"copilot.check":   copilotCheck(deps),
		"copilot.enable":  copilotEnable(deps),
		"copilot.disable": copilotDisable(deps),
	}
}

func copilotStatus(deps CopilotDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		status := deps.Copilot.Status()
		return protocol.MustResponseOK(req.ID, status)
	}
}

func copilotCheck(deps CopilotDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		results := deps.Copilot.RunCheck(ctx)
		return protocol.MustResponseOK(req.ID, map[string]any{
			"results": results,
			"count":   len(results),
		})
	}
}

func copilotEnable(deps CopilotDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		deps.Copilot.Enable()
		return protocol.MustResponseOK(req.ID, map[string]any{"enabled": true})
	}
}

func copilotDisable(deps CopilotDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		deps.Copilot.Disable()
		return protocol.MustResponseOK(req.ID, map[string]any{"enabled": false})
	}
}
