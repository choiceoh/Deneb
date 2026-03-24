package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/secret"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// SecretDeps holds the dependencies for secrets RPC methods.
type SecretDeps struct {
	Resolver *secret.Resolver
}

// RegisterSecretMethods registers secrets.reload and secrets.resolve RPC methods.
func RegisterSecretMethods(d *Dispatcher, deps SecretDeps) {
	if deps.Resolver == nil {
		return
	}

	d.Register("secrets.reload", secretsReload(deps))
	d.Register("secrets.resolve", secretsResolve(deps))
}

func secretsReload(deps SecretDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		result := deps.Resolver.Reload()
		resp, _ := protocol.NewResponseOK(req.ID, result)
		return resp
	}
}

func secretsResolve(deps SecretDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			CommandName string   `json:"commandName"`
			TargetIDs   []string `json:"targetIds"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.CommandName == "" || len(p.TargetIDs) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "commandName and targetIds are required"))
		}

		result := deps.Resolver.Resolve(p.CommandName, p.TargetIDs)
		resp, _ := protocol.NewResponseOK(req.ID, result)
		return resp
	}
}
