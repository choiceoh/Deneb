// system_monitoring.go — monitoring.* RPC handlers.
package system

import (
	"context"
	"sort"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/monitoring"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/metrics"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// MethodLister returns all registered RPC method names.
type MethodLister interface {
	Methods() []string
}

// MonitoringDeps holds the dependencies for monitoring RPC methods.
type MonitoringDeps struct {
	ChannelHealth *monitoring.ChannelHealthMonitor
	Dispatcher    MethodLister // for rpc_zero_calls
}

// MonitoringMethods returns the monitoring.channel_health and
// monitoring.rpc_zero_calls handlers.
func MonitoringMethods(deps MonitoringDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"monitoring.channel_health": func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			if deps.ChannelHealth == nil {
				resp := rpcutil.RespondOK(req.ID, map[string]any{"channels": []any{}})
				return resp
			}
			snapshot := deps.ChannelHealth.HealthSnapshot()
			resp := rpcutil.RespondOK(req.ID, map[string]any{"channels": snapshot})
			return resp
		},

		"monitoring.rpc_zero_calls": func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			if deps.Dispatcher == nil {
				return rpcutil.RespondOK(req.ID, map[string]any{
					"zeroCalls": []any{},
					"total":     0,
				})
			}

			// Get all registered methods and their call counts.
			methods := deps.Dispatcher.Methods()
			sort.Strings(methods)

			counts := metrics.RPCRequestsTotal.Snapshot()

			// Find methods with zero calls.
			var zeroCalls []string
			called := make([]map[string]any, 0)
			for _, m := range methods {
				okKey := m + "\x00" + "ok"
				errKey := m + "\x00" + "error"
				ok := counts[okKey]
				errs := counts[errKey]
				total := ok + errs
				if total == 0 {
					zeroCalls = append(zeroCalls, m)
				} else {
					called = append(called, map[string]any{
						"method": m,
						"ok":     ok,
						"error":  errs,
					})
				}
			}

			resp := rpcutil.RespondOK(req.ID, map[string]any{
				"zeroCalls":    zeroCalls,
				"zeroCount":    len(zeroCalls),
				"calledCount":  len(called),
				"called":       called,
				"totalMethods": len(methods),
			})
			return resp
		},
	}
}
