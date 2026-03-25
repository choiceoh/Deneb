package rpc

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// RegisterSessionRepairMethods registers session repair and overflow check methods.
// These are bridge-delegated: the Go layer validates params, then forwards to
// the TypeScript runtime for file I/O and repair logic.
func RegisterSessionRepairMethods(d *Dispatcher, deps SessionDeps) {
	d.Register("sessions.repair", sessionsRepair(deps))
	d.Register("sessions.overflow_check", sessionsOverflowCheck(deps))
}

// sessionsRepair triggers post-compaction transcript repair for a session.
// Delegates to TypeScript runtime via bridge for file I/O and repair logic.
func sessionsRepair(deps SessionDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			SessionKey string `json:"sessionKey"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "params required"))
		}

		if requireKey(p.SessionKey) == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "sessionKey required"))
		}

		// Forward to TypeScript runtime for repair logic.
		if resp := forwardToBridge(ctx, deps.Forwarder, req); resp != nil {
			return resp
		}

		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrUnavailable, "bridge not available"))
	}
}

// sessionsOverflowCheck checks if a session's context is in overflow
// state and returns the context usage percentage.
func sessionsOverflowCheck(deps SessionDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if resp := forwardToBridge(ctx, deps.Forwarder, req); resp != nil {
			return resp
		}

		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrUnavailable, "bridge not available"))
	}
}
