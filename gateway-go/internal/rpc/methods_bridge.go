package rpc

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// BridgeDeps holds the dependencies for bridge-forwarded RPC methods.
type BridgeDeps struct {
	// ForwarderFunc returns the current forwarder (bridge).
	// Using a func allows late binding: the bridge may not be connected at
	// registration time but will be available when handlers execute.
	ForwarderFunc func() Forwarder
}

// RegisterBridgeMethods registers RPC methods that are forwarded to the Node.js
// Plugin Host via the bridge. These methods have tightly-coupled TypeScript
// implementations and are registered here for proper scope-based authorization
// rather than falling through to the dispatcher's default forwarder (which
// would require ScopeAdmin for unknown methods).
//
// Note: Methods that have been ported to native Go are no longer bridge-forwarded:
// exec.approvals.*, node.*, device.*, cron advanced, agents.*, config advanced,
// skills.*, wizard.*, secrets.*, talk.*, sessions.send/steer/abort,
// agent, agent.identity.get, agent.wait.
func RegisterBridgeMethods(d *Dispatcher, deps BridgeDeps) {
	methods := []string{
		// Channels
		"channels.logout",

		// Sessions (remaining bridge-forwarded methods)
		"sessions.patch",
		"sessions.reset",
		"sessions.compact",
		"sessions.preview",
		"sessions.resolve",

		// Note: send and poll have been migrated to native Go handlers
		// (RegisterMessagingMethods) with Telegram support + bridge fallback.

		// Tools (catalog only — invoke/list/status are native)
		"tools.catalog",

		// Media
		"browser.request",

		// Web Login
		"web.login.start",
		"web.login.wait",

		// Note: usage.status, usage.cost, update.run, doctor.memory.status,
		// logs.tail, maintenance.* have been migrated to native Go handlers.
	}

	for _, m := range methods {
		d.Register(m, bridgeForward(deps, m))
	}
}

// bridgeForward returns a handler that forwards the request to the Plugin Host bridge.
func bridgeForward(deps BridgeDeps, method string) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		fwd := deps.ForwarderFunc()
		if fwd == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed,
				"plugin host bridge not available for method: "+method,
			))
		}
		resp, err := fwd.Forward(ctx, req)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed,
				"bridge forward failed for "+method+": "+err.Error(),
			))
		}
		return resp
	}
}
