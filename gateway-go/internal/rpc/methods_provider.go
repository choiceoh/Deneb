package rpc

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/choiceoh/deneb/gateway-go/internal/provider"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ProviderDeps holds dependencies for provider RPC methods.
type ProviderDeps struct {
	Deps
	Providers   *provider.Registry
	AuthManager *provider.AuthManager
}

// RegisterProviderMethods registers provider-related RPC methods.
func RegisterProviderMethods(d *Dispatcher, deps ProviderDeps) {
	if deps.Providers == nil {
		return
	}

	d.Register("providers.list", providersList(deps))
	d.Register("providers.get", providersGet(deps))
	d.Register("providers.catalog", providersCatalog(deps))
	d.Register("providers.auth.prepare", providersAuthPrepare(deps))
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

func providersList(deps ProviderDeps) HandlerFunc {
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

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"providers": providers,
		})
		return resp
	}
}

func providersGet(deps ProviderDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id is required"))
		}

		plugin := deps.Providers.GetByNormalizedID(p.ID)
		if plugin == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "provider not found: "+p.ID))
		}

		resp := protocol.MustResponseOK(req.ID, serializePlugin(plugin))
		return resp
	}
}

func providersCatalog(deps ProviderDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Provider string `json:"provider"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}

		// If provider specified, check if it supports catalog locally.
		if p.Provider != "" {
			plugin := deps.Providers.GetByNormalizedID(p.Provider)
			if cp, ok := plugin.(provider.CatalogProvider); ok {
				result, err := cp.Catalog(ctx, provider.CatalogContext{})
				if err == nil && result != nil {
					resp := protocol.MustResponseOK(req.ID, result)
					return resp
				}
			}
		}

		// Empty catalog fallback.
		resp := protocol.MustResponseOK(req.ID, provider.CatalogResult{
			Entries: []provider.CatalogEntry{},
		})
		return resp
	}
}

func providersAuthPrepare(deps ProviderDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p provider.RuntimeAuthContext
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid auth params: "+err.Error()))
		}
		if p.Provider == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "provider is required"))
		}

		if deps.AuthManager == nil {
			resp := protocol.MustResponseOK(req.ID, provider.PreparedAuth{
				APIKey: p.APIKey,
			})
			return resp
		}

		prepared, err := deps.AuthManager.Prepare(ctx, p)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "auth prepare failed: "+err.Error()))
		}

		resp := protocol.MustResponseOK(req.ID, prepared)
		return resp
	}
}
