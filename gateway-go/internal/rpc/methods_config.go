package rpc

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ConfigReloadDeps holds the dependencies for the config.reload method.
type ConfigReloadDeps struct {
	// OnReloaded is called after a successful config reload with the new config snapshot.
	// Use this to propagate config changes to Go subsystems (hooks, broadcaster, etc.).
	OnReloaded func(snapshot *config.ConfigSnapshot)
}

// RegisterConfigReloadMethod registers the config.reload RPC method.
func RegisterConfigReloadMethod(d *Dispatcher, deps ...ConfigReloadDeps) {
	var onReloaded func(snapshot *config.ConfigSnapshot)
	if len(deps) > 0 {
		onReloaded = deps[0].OnReloaded
	}

	d.Register("config.reload", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		snapshot, err := config.LoadConfigFromDefaultPath()
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "config reload failed: "+err.Error()))
		}
		if !snapshot.Valid {
			resp := protocol.MustResponseOK(req.ID, map[string]any{
				"valid":  false,
				"issues": snapshot.Issues,
			})
			return resp
		}

		// Propagate to Go subsystems (hooks, broadcaster, etc.).
		if onReloaded != nil {
			onReloaded(snapshot)
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"valid":  true,
			"path":   snapshot.Path,
			"config": snapshot.Config,
		})
		return resp
	})
}
