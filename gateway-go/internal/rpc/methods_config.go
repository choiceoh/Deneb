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
}

// RegisterConfigReloadMethod registers the config.reload RPC method.
// After reloading the Go gateway's config, it forwards a plugin-host.reload
// request to the Node.js Plugin Host so both runtimes stay in sync.
func RegisterConfigReloadMethod(d *Dispatcher, deps ...ConfigReloadDeps) {
	var forwarder Forwarder
	var logger *slog.Logger
	if len(deps) > 0 {
		forwarder = deps[0].Forwarder
		logger = deps[0].Logger
	}

	d.Register("config.reload", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		snapshot, err := config.LoadConfigFromDefaultPath()
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "config reload failed: "+err.Error()))
		}
		if !snapshot.Valid {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"valid":  false,
				"issues": snapshot.Issues,
			})
			return resp
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

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"valid":               true,
			"path":                snapshot.Path,
			"config":              snapshot.Config,
			"pluginHostReloaded":  pluginHostReloaded,
		})
		return resp
	})
}
