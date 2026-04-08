// system_health.go — health.check and system.info RPC handlers.
package system

import (
	"context"
	"runtime"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// HealthDeps holds dependencies for health.check and system.info.
// Uses narrow types to avoid importing session/telegram packages.
type HealthDeps struct {
	SessionCount func() int  // session.Manager.Count
	HasTelegram  func() bool // whether Telegram plugin is active
	Version      string
}

// HealthMethods returns health.check and system.info handlers.
func HealthMethods(deps HealthDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"health.check": healthCheck(deps),
		"system.info":  systemInfo(deps),
	}
}

func healthCheck(deps HealthDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var channels []string
		if deps.HasTelegram != nil && deps.HasTelegram() {
			channels = []string{"telegram"}
		}
		sessionCount := 0
		if deps.SessionCount != nil {
			sessionCount = deps.SessionCount()
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"status":   "ok",
			"runtime":  "go",
			"sessions": sessionCount,
			"channels": channels,
		})
	}
}

func systemInfo(deps HealthDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		version := deps.Version
		if version == "" {
			version = "unknown"
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"runtime":   "go",
			"version":   version,
			"goVersion": runtime.Version(),
			"os":        "linux",
			"arch":      runtime.GOARCH,
			"numCPU":    runtime.NumCPU(),
		})
	}
}
