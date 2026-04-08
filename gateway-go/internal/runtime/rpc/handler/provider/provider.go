// Package provider contains RPC handlers for provider and model methods.
package provider

import (
	"context"
	"sort"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps holds dependencies for provider RPC methods.
type Deps struct {
	Providers   *provider.Registry
	AuthManager *provider.AuthManager
}

// ModelsDeps holds dependencies for model-related RPC methods.
type ModelsDeps struct {
	Providers *provider.Registry
}

// Methods returns all provider-related RPC handlers.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	if deps.Providers == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"providers.list":         providersList(deps),
		"providers.get":          providersGet(deps),
		"providers.catalog":      providersCatalog(deps),
		"providers.auth.prepare": providersAuthPrepare(deps),
	}
}

// ModelsMethods returns all model-related RPC handlers.
func ModelsMethods(deps ModelsDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"models.list": modelsList(deps),
	}
}

// serializePlugin builds a map representation of a provider plugin.
func serializePlugin(p provider.Plugin) map[string]any {
	entry := map[string]any{
		"id":    p.ID(),
		"label": p.Label(),
		"auth":  p.AuthMethods(),
	}
	if ap, ok := p.(provider.AliasProvider); ok {
		entry["aliases"] = ap.Aliases()
	}
	if cp, ok := p.(provider.CapabilitiesProvider); ok {
		entry["capabilities"] = cp.Capabilities()
	}
	return entry
}

func providersList(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		snap := deps.Providers.Snapshot()

		// Build sorted list for deterministic output.
		ids := make([]string, 0, len(snap))
		for id := range snap {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		providers := make([]map[string]any, 0, len(snap))
		for _, id := range ids {
			providers = append(providers, serializePlugin(snap[id]))
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
			"providers": providers,
		})
	}
}

func providersGet(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.ID == "" {
			return nil, rpcerr.MissingParam("id")
		}
		plugin := deps.Providers.ByNormalizedID(p.ID)
		if plugin == nil {
			return nil, rpcerr.Newf(protocol.ErrNotFound, "provider not found: %s", p.ID)
		}
		return serializePlugin(plugin), nil
	})
}

func providersCatalog(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		Provider string `json:"provider"`
	}
	return rpcutil.BindHandlerCtx[params](func(ctx context.Context, p params) (any, error) {
		if p.Provider != "" {
			plugin := deps.Providers.ByNormalizedID(p.Provider)
			if cp, ok := plugin.(provider.CatalogProvider); ok {
				result, err := cp.Catalog(ctx, provider.CatalogContext{})
				if err == nil && result != nil {
					return result, nil
				}
			}
		}
		return provider.CatalogResult{Entries: []provider.CatalogEntry{}}, nil
	})
}

func providersAuthPrepare(deps Deps) rpcutil.HandlerFunc {
	return rpcutil.BindHandlerCtx[provider.RuntimeAuthContext](func(ctx context.Context, p provider.RuntimeAuthContext) (any, error) {
		if p.Provider == "" {
			return nil, rpcerr.MissingParam("provider")
		}
		if deps.AuthManager == nil {
			return provider.PreparedAuth{APIKey: p.APIKey}, nil
		}
		prepared, err := deps.AuthManager.Prepare(ctx, p)
		if err != nil {
			return nil, rpcerr.Newf(protocol.ErrDependencyFailed, "auth prepare failed: %v", err)
		}
		return prepared, nil
	})
}

func modelsList(deps ModelsDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Providers == nil {
			return rpcutil.RespondOK(req.ID, map[string]any{
				"models": []any{},
			})
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

		return rpcutil.RespondOK(req.ID, map[string]any{
			"models": models,
		})
	}
}
