// Package rl provides RPC handlers for the RL self-learning pipeline.
//
// Methods manage the lifecycle of the external sglang + Tinker-Atropos
// process trio rather than implementing training directly.
package rl

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	rlpkg "github.com/choiceoh/deneb/gateway-go/internal/rl"
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
		if err := deps.Service.Start(context.Background()); err != nil {
			return map[string]any{"ok": false, "error": err.Error()}, nil
		}
		return map[string]any{"ok": true, "state": deps.Service.Status()}, nil
	})
}

func rlStop(deps Deps) rpcutil.HandlerFunc {
	return rpcutil.BindHandler[struct{}](func(_ struct{}) (any, error) {
		if err := deps.Service.Stop(); err != nil {
			return map[string]any{"ok": false, "error": err.Error()}, nil
		}
		return map[string]any{"ok": true, "state": deps.Service.Status()}, nil
	})
}

func rlTrajectories(deps Deps) rpcutil.HandlerFunc {
	return rpcutil.BindHandler[struct{}](func(_ struct{}) (any, error) {
		store := deps.Service.Store()
		pending := 0
		if store != nil {
			pending = store.PendingCount()
		}
		return map[string]any{"pending": pending}, nil
	})
}
