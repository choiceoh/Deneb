package rpc

import (
	"context"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ConfigReloadDeps holds the dependencies for the config.reload method.
type ConfigReloadDeps struct {
	// Forwarder sends requests to the Plugin Host bridge (optional).
	// When set, config.reload propagates to the Plugin Host so it can
	// reinitialize its gateway context with updated config and plugins.
	Forwarder Forwarder
	Logger    *slog.Logger
	// OnReloaded is called after a successful config reload with the new config snapshot.
	// Use this to propagate config changes to Go subsystems (hooks, broadcaster, etc.).
	OnReloaded func(snapshot *config.ConfigSnapshot)
}

// RegisterConfigReloadMethod registers the config.reload RPC method.
// After reloading the Go gateway's config, it forwards a plugin-host.reload
// request to the Node.js Plugin Host so both runtimes stay in sync.
func RegisterConfigReloadMethod(d *Dispatcher, deps ...ConfigReloadDeps) {
	var forwarder Forwarder
	var logger *slog.Logger
	var onReloaded func(snapshot *config.ConfigSnapshot)
	if len(deps) > 0 {
		forwarder = deps[0].Forwarder
		logger = deps[0].Logger
		onReloaded = deps[0].OnReloaded
	}

	d.Register("config.reload", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
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

		// Propagate reload to Plugin Host so it reinitializes its gateway
		// context with the updated config and plugin state.
		pluginHostReloaded := false
		if forwarder != nil {
			reloadReq := &protocol.RequestFrame{
				Type:   protocol.FrameTypeRequest,
				ID:     req.ID + ":plugin-host-reload",
				Method: "plugin-host.reload",
			}
			reloadResp, reloadErr := forwarder.Forward(ctx, reloadReq)
			if reloadErr != nil {
				if logger != nil {
					logger.Warn("plugin host reload failed", "error", reloadErr)
				}
			} else {
				pluginHostReloaded = reloadResp.OK
			}
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"valid":               true,
			"path":                snapshot.Path,
			"config":              snapshot.Config,
			"pluginHostReloaded":  pluginHostReloaded,
		})
		return resp
	})
}
