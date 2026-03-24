package rpc

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// RegisterConfigReloadMethod registers the config.reload RPC method.
// Note: config.get is registered separately in registerBuiltinMethods using runtimeCfg.
func RegisterConfigReloadMethod(d *Dispatcher) {
	d.Register("config.reload", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
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
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"valid":  true,
			"path":   snapshot.Path,
			"config": snapshot.Config,
		})
		return resp
	})
}
