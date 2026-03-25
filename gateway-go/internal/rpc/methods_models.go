package rpc

import (
	"context"
	"sort"

	"github.com/choiceoh/deneb/gateway-go/internal/provider"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ModelsDeps holds dependencies for model-related RPC methods.
type ModelsDeps struct {
	Providers *provider.Registry
}

// RegisterModelsMethods registers the models.list RPC method.
func RegisterModelsMethods(d *Dispatcher, deps ModelsDeps) {
	d.Register("models.list", modelsList(deps))
}

// modelsList aggregates model catalog entries from all registered providers.
func modelsList(deps ModelsDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Providers == nil {
			resp := protocol.MustResponseOK(req.ID, map[string]any{
				"models": []any{},
			})
			return resp
		}

		snap := deps.Providers.Snapshot()
		ids := make([]string, 0, len(snap))
		for id := range snap {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		var models []provider.CatalogEntry
		for _, id := range ids {
			p := snap[id]
			cp, ok := p.(provider.CatalogProvider)
			if !ok {
				continue
			}
			result, err := cp.Catalog(ctx, provider.CatalogContext{})
			if err != nil || result == nil {
				continue
			}
			models = append(models, result.Entries...)
		}

		if models == nil {
			models = []provider.CatalogEntry{}
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"models": models,
		})
		return resp
	}
}
