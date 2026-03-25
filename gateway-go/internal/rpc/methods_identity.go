package rpc

import (
	"context"
	"os"
	"runtime"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// RegisterIdentityMethods registers the gateway.identity.get RPC method.
func RegisterIdentityMethods(d *Dispatcher, version string) {
	d.Register("gateway.identity.get", gatewayIdentityGet(version))
}

// gatewayIdentityGet returns gateway identification info: hostname, machine ID, version.
func gatewayIdentityGet(version string) HandlerFunc {
	// Pre-compute static identity fields at registration time.
	hostname, _ := os.Hostname()
	stateDir := config.ResolveStateDir()

	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"hostname": hostname,
			"version":  version,
			"runtime":  "go",
			"os":       runtime.GOOS,
			"arch":     runtime.GOARCH,
			"stateDir": stateDir,
		})
		return resp
	}
}
