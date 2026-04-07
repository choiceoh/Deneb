// Package platform provides RPC method handlers for the platform domain,
// covering the secret subsystem.
//
// It exposes SecretMethods, which returns a handler map that can be
// bulk-registered on the rpc.Dispatcher.
package platform

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/secret"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Secret
// ---------------------------------------------------------------------------

// SecretDeps holds the dependencies for secrets RPC methods.
type SecretDeps struct {
	Resolver *secret.Resolver
}

// SecretMethods returns the secrets RPC handlers keyed by method name.
// If deps.Resolver is nil, nil is returned.
func SecretMethods(deps SecretDeps) map[string]rpcutil.HandlerFunc {
	if deps.Resolver == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"secrets.reload":  secretsReload(deps),
		"secrets.resolve": secretsResolve(deps),
	}
}

func secretsReload(deps SecretDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		result := deps.Resolver.Reload()
		return rpcutil.RespondOK(req.ID, result)
	}
}

func secretsResolve(deps SecretDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			CommandName string   `json:"commandName"`
			TargetIDs   []string `json:"targetIds"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.CommandName == "" || len(p.TargetIDs) == 0 {
			return rpcerr.MissingParam("commandName and targetIds").Response(req.ID)
		}

		result := deps.Resolver.Resolve(p.CommandName, p.TargetIDs)
		return rpcutil.RespondOK(req.ID, result)
	}
}
