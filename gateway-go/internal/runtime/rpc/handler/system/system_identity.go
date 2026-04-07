// system_identity.go — gateway.identity.get RPC handler.
package system

import (
	"context"
	"os"
	"runtime"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// IdentityMethods returns the gateway.identity.get handler.
func IdentityMethods(version string) map[string]rpcutil.HandlerFunc {
	// Pre-compute static identity fields at registration time.
	hostname, _ := os.Hostname()
	stateDir := config.ResolveStateDir()

	return map[string]rpcutil.HandlerFunc{
		"gateway.identity.get": func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			resp := rpcutil.RespondOK(req.ID, map[string]any{
				"hostname": hostname,
				"version":  version,
				"runtime":  "go",
				"os":       "linux",
				"arch":     runtime.GOARCH,
				"stateDir": stateDir,
			})
			return resp
		},
	}
}
