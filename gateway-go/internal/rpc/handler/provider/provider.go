// Package provider contains RPC handlers for provider and model methods.
package provider

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/choiceoh/deneb/gateway-go/internal/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
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

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"providers": providers,
		})
		return resp
	}
}

func providersGet(deps Deps) rpcutil.HandlerFunc {
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

func providersCatalog(deps Deps) rpcutil.HandlerFunc {
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

func providersAuthPrepare(deps Deps) rpcutil.HandlerFunc {
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

func modelsList(deps ModelsDeps) rpcutil.HandlerFunc {
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
