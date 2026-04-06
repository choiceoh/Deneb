// Package rl provides RPC handlers for the task-specific RL training pipeline.
//
// Methods manage the lifecycle of the external sglang + Tinker-Atropos
// process trio and expose trajectory collection status.
package rl

import (
	"context"

	rlpkg "github.com/choiceoh/deneb/gateway-go/internal/rl"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
)

// Deps holds dependencies for rl.* RPC methods.
type Deps struct {
	Service *rlpkg.Service
}

// Methods returns all rl.* RPC handler methods.
// Returns nil if the RL service is not configured, preventing registration.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	if deps.Service == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"rl.status":       rlStatus(deps),
		"rl.start":        rlStart(deps),
		"rl.stop":         rlStop(deps),
		"rl.trajectories": rlTrajectories(deps),
	}
}

func rlStatus(deps Deps) rpcutil.HandlerFunc {
	return rpcutil.BindHandler[struct{}](func(_ struct{}) (any, error) {
		return deps.Service.Status(), nil
	})
}

func rlStart(deps Deps) rpcutil.HandlerFunc {
	return rpcutil.BindHandler[struct{}](func(_ struct{}) (any, error) {
		// Start uses context.Background — processes must outlive the RPC call.
		// Service.Stop() handles cancellation.
		if err := deps.Service.Start(context.Background()); err != nil {
			return map[string]any{"ok": false, "error": err.Error()}, nil
		}
		return map[string]any{"ok": true, "status": deps.Service.Status()}, nil
	})
}

func rlStop(deps Deps) rpcutil.HandlerFunc {
	return rpcutil.BindHandler[struct{}](func(_ struct{}) (any, error) {
		if err := deps.Service.Stop(); err != nil {
			return map[string]any{"ok": false, "error": err.Error()}, nil
		}
		return map[string]any{"ok": true, "status": deps.Service.Status()}, nil
	})
}

func rlTrajectories(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		TaskType string `json:"taskType,omitempty"`
		Limit    int    `json:"limit,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		limit := p.Limit
		if limit <= 0 {
			limit = 50
		}
		items := deps.Service.Store().List(p.TaskType, limit)
		stats := deps.Service.Store().Stats()
		return map[string]any{
			"items": items,
			"stats": stats,
		}, nil
	})
}
