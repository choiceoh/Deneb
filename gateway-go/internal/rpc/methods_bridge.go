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
func RegisterBridgeMethods(d *Dispatcher, deps BridgeDeps) {
	methods := []string{
		// Config
		"config.set",
		"config.patch",
		"config.apply",
		"config.schema",
		"config.schema.lookup",

		// Channels
		"channels.logout",

		// Sessions (messaging/lifecycle)
		"sessions.send",
		"sessions.steer",
		"sessions.abort",
		"sessions.patch",
		"sessions.reset",
		"sessions.compact",
		"sessions.preview",
		"sessions.resolve",

		// Messaging
		"send",
		"poll",
		"talk.config",
		"talk.mode",

		// Agent
		"agent",
		"agent.identity.get",
		"agent.wait",

		// Agents CRUD
		"agents.list",
		"agents.create",
		"agents.update",
		"agents.delete",
		"agents.files.list",
		"agents.files.get",
		"agents.files.set",

		// Skills
		"skills.status",
		"skills.bins",
		"skills.install",
		"skills.update",
		"tools.catalog",

		// Wizard
		"wizard.start",
		"wizard.next",
		"wizard.cancel",
		"wizard.status",

		// TTS / Media
		"tts.status",
		"tts.enable",
		"tts.disable",
		"tts.convert",
		"tts.setProvider",
		"tts.providers",
		"voicewake.get",
		"voicewake.set",
		"browser.request",

		// Web Login
		"web.login.start",
		"web.login.wait",

		// Exec Approvals
		"exec.approvals.get",
		"exec.approvals.set",
		"exec.approvals.node.get",
		"exec.approvals.node.set",
		"exec.approval.request",
		"exec.approval.waitDecision",
		"exec.approval.resolve",

		// Nodes
		"node.pair.request",
		"node.pair.list",
		"node.pair.approve",
		"node.pair.reject",
		"node.pair.verify",
		"node.list",
		"node.describe",
		"node.rename",
		"node.invoke",
		"node.invoke.result",
		"node.canvas.capability.refresh",
		"node.pending.pull",
		"node.pending.ack",
		"node.pending.drain",
		"node.pending.enqueue",

		// Device
		"device.pair.list",
		"device.pair.approve",
		"device.pair.reject",
		"device.pair.remove",
		"device.token.rotate",
		"device.token.revoke",

		// Secrets
		"secrets.reload",
		"secrets.resolve",

		// Usage
		"usage.status",
		"usage.cost",

		// Cron (RPC-driven task management — persisted in TS)
		"wake",
		"cron.status",
		"cron.add",
		"cron.update",
		"cron.remove",
		"cron.run",
		"cron.runs",

		// System (heavy — delegated to TS)
		"update.run",
		"doctor.memory.status",
		"logs.tail",
		"maintenance.run",
		"maintenance.status",
		"maintenance.summary",
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
