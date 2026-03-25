package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/maintenance"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// MaintenanceDeps holds dependencies for maintenance RPC methods.
type MaintenanceDeps struct {
	Runner *maintenance.Runner
}

// RegisterMaintenanceMethods registers maintenance.run, maintenance.status,
// and maintenance.summary RPC methods.
func RegisterMaintenanceMethods(d *Dispatcher, deps MaintenanceDeps) {
	d.Register("maintenance.run", maintenanceRun(deps))
	d.Register("maintenance.status", maintenanceStatus(deps))
	d.Register("maintenance.summary", maintenanceSummary(deps))
}

func maintenanceRun(deps MaintenanceDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Runner == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "maintenance runner not available"))
		}

		var p struct {
			DryRun bool `json:"dryRun"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}

		report := deps.Runner.Run(p.DryRun)
		resp, _ := protocol.NewResponseOK(req.ID, report)
		return resp
	}
}

func maintenanceStatus(deps MaintenanceDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Runner == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"hasReport": false})
			return resp
		}

		report := deps.Runner.LastReport()
		if report == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"hasReport": false})
			return resp
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"hasReport": true,
			"report":    report,
			"summary":   maintenance.SummarizeReport(report),
		})
		return resp
	}
}

func maintenanceSummary(deps MaintenanceDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Runner == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "maintenance runner not available"))
		}

		report := deps.Runner.LastReport()
		if report == nil {
			// No previous report — trigger a dry-run.
			report = deps.Runner.Run(true)
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"summary": maintenance.SummarizeReport(report),
			"report":  report,
		})
		return resp
	}
}
