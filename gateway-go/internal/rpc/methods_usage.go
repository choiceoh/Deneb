package rpc

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/usage"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// UsageDeps holds dependencies for usage RPC methods.
type UsageDeps struct {
	Tracker *usage.Tracker
}

// RegisterUsageMethods registers usage.status and usage.cost RPC methods.
func RegisterUsageMethods(d *Dispatcher, deps UsageDeps) {
	d.Register("usage.status", usageStatus(deps))
	d.Register("usage.cost", usageCost(deps))
}

func usageStatus(deps UsageDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Tracker == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"uptime":    "0s",
				"providers": map[string]any{},
			})
			return resp
		}
		resp, _ := protocol.NewResponseOK(req.ID, deps.Tracker.Status())
		return resp
	}
}

func usageCost(deps UsageDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Tracker == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"totalCalls": 0,
				"providers":  map[string]any{},
			})
			return resp
		}
		resp, _ := protocol.NewResponseOK(req.ID, deps.Tracker.Cost())
		return resp
	}
}
