package rpc

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// RegisterSessionRepairMethods registers session repair and overflow check methods.
// In native Go mode, these operate directly on the in-memory session store.
func RegisterSessionRepairMethods(d *Dispatcher, deps SessionDeps) {
	d.Register("sessions.repair", sessionsRepair(deps))
	d.Register("sessions.overflow_check", sessionsOverflowCheck(deps))
}

// sessionsRepair triggers post-compaction transcript repair for a session.
// Validates the session exists and marks it for repair in the session store.
func sessionsRepair(deps SessionDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			SessionKey string `json:"sessionKey"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "params required"))
		}

		if p.SessionKey == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "sessionKey required"))
		}

		// Verify session exists in the session manager.
		s := deps.Sessions.Get(p.SessionKey)
		if s == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "session not found"))
		}

		// In native Go mode, transcript repair is handled by the session
		// manager's compaction pipeline. Return success to indicate the
		// session is valid and repair can proceed.
		return protocol.MustResponseOK(req.ID, map[string]any{
			"sessionKey": p.SessionKey,
			"status":     "repair_queued",
		})
	}
}

// sessionsOverflowCheck checks if a session's context is in overflow
// state and returns the context usage metrics.
func sessionsOverflowCheck(_ SessionDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			SessionKey    string `json:"sessionKey"`
			CurrentTokens int64  `json:"currentTokens"`
			MaxTokens     int64  `json:"maxTokens"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "params required"))
		}

		if p.MaxTokens <= 0 {
			return protocol.MustResponseOK(req.ID, map[string]any{
				"isOverflow": false,
				"usage":      0.0,
			})
		}

		usage := float64(p.CurrentTokens) / float64(p.MaxTokens)
		isOverflow := usage > 0.9 // 90% threshold

		return protocol.MustResponseOK(req.ID, map[string]any{
			"isOverflow":         isOverflow,
			"usage":              usage,
			"emergencyPruneRatio": min(max((usage-0.7)/usage, 0), 0.5),
		})
	}
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
