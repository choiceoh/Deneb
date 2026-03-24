package rpc

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ProviderDeps holds the dependencies for provider RPC methods.
type ProviderDeps struct {
	Deps
	ProviderCatalog ProviderCatalog
}

// ProviderCatalog is the interface for querying discovered model providers.
// This decouples the RPC layer from the concrete provider manager.
type ProviderCatalog interface {
	ListProviders() []protocol.ProviderMeta
	ListCatalogEntries() []protocol.ProviderCatalogEntry
}

// RegisterProviderMethods registers provider-related RPC methods.
func RegisterProviderMethods(d *Dispatcher, deps ProviderDeps) {
	if deps.ProviderCatalog == nil {
		return
	}

	d.Register("providers.list", providersList(deps))
	d.Register("providers.catalog", providersCatalog(deps))
}

func providersList(deps ProviderDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		providers := deps.ProviderCatalog.ListProviders()
		resp, _ := protocol.NewResponseOK(req.ID, providers)
		return resp
	}
}

func providersCatalog(deps ProviderDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		snapshot := protocol.ProviderCatalogSnapshot{
			Providers:  deps.ProviderCatalog.ListProviders(),
			Entries:    deps.ProviderCatalog.ListCatalogEntries(),
			SnapshotAt: time.Now().UnixMilli(),
		}
		resp, _ := protocol.NewResponseOK(req.ID, snapshot)
		return resp
	}
}
