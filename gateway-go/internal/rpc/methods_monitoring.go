package rpc

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/monitoring"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// MonitoringDeps holds the dependencies for monitoring RPC methods.
type MonitoringDeps struct {
	ChannelHealth *monitoring.ChannelHealthMonitor
	Activity      *monitoring.ActivityTracker
}

// RegisterMonitoringMethods registers monitoring-related RPC methods.
func RegisterMonitoringMethods(d *Dispatcher, deps MonitoringDeps) {
	d.Register("monitoring.channel_health", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.ChannelHealth == nil {
			resp := protocol.MustResponseOK(req.ID, map[string]any{"channels": []any{}})
			return resp
		}
		snapshot := deps.ChannelHealth.HealthSnapshot()
		resp := protocol.MustResponseOK(req.ID, map[string]any{"channels": snapshot})
		return resp
	})

	d.Register("monitoring.activity", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Activity == nil {
			resp := protocol.MustResponseOK(req.ID, map[string]any{"lastActivityMs": 0})
			return resp
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"lastActivityMs": deps.Activity.LastActivityAt(),
		})
		return resp
	})
}
