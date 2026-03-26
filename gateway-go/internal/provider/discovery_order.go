// discovery_order.go — Provider discovery ordering system for the Go gateway.
// Mirrors src/plugins/provider-discovery.ts (460 LOC).
//
// Provides:
// - Discovery ordering (simple, profile, paired, late)
// - Provider grouping by order
// - Catalog result normalization
// - Catalog execution dispatch
package provider

import (
	"context"
	"sort"
	"strings"
)

// DiscoveryOrder determines the order in which provider catalogs are resolved.
type DiscoveryOrder string

const (
	DiscoveryOrderSimple  DiscoveryOrder = "simple"
	DiscoveryOrderProfile DiscoveryOrder = "profile"
	DiscoveryOrderPaired  DiscoveryOrder = "paired"
	DiscoveryOrderLate    DiscoveryOrder = "late"
)

// AllDiscoveryOrders is the canonical order of discovery phases.
var AllDiscoveryOrders = []DiscoveryOrder{
	DiscoveryOrderSimple,
	DiscoveryOrderProfile,
	DiscoveryOrderPaired,
	DiscoveryOrderLate,
}

// DiscoveryProvider represents a provider plugin with catalog/discovery hooks.
type DiscoveryProvider struct {
	Plugin        Plugin
	CatalogOrder  DiscoveryOrder
	Label         string
	HasCatalog    bool
	HasDiscovery  bool
}

// DiscoveryProviderConfig holds resolved provider config for catalog results.
type DiscoveryProviderConfig struct {
	ID      string         `json:"id,omitempty"`
	BaseURL string         `json:"baseUrl,omitempty"`
	ApiKey  string         `json:"apiKey,omitempty"`
	API     string         `json:"api,omitempty"`
	Models  map[string]any `json:"models,omitempty"`
	Headers map[string]any `json:"headers,omitempty"`
}

// DiscoveryCatalogResult is the result of running a provider's catalog hook.
type DiscoveryCatalogResult struct {
	// Provider is set for single-provider results.
	Provider *DiscoveryProviderConfig
	// Providers is set for multi-provider results.
	Providers map[string]*DiscoveryProviderConfig
}

// ResolveDiscoveryProviders returns all registered providers that have
// catalog or discovery hooks. Mirrors resolvePluginDiscoveryProviders.
func ResolveDiscoveryProviders(registry *Registry) []DiscoveryProvider {
	snapshot := registry.Snapshot()
	var result []DiscoveryProvider
	for _, plugin := range snapshot {
		dp := toDiscoveryProvider(plugin)
		if dp != nil {
			result = append(result, *dp)
		}
	}
	return result
}

func toDiscoveryProvider(plugin Plugin) *DiscoveryProvider {
	hasCatalog := false
	hasDiscovery := false
	order := DiscoveryOrderLate

	if cp, ok := plugin.(CatalogProvider); ok {
		hasCatalog = true
		_ = cp
	}
	if dp, ok := plugin.(discoveryProvider); ok {
		hasDiscovery = true
		_ = dp
	}

	if !hasCatalog && !hasDiscovery {
		return nil
	}

	if op, ok := plugin.(catalogOrderProvider); ok {
		order = op.CatalogOrder()
	}

	return &DiscoveryProvider{
		Plugin:       plugin,
		CatalogOrder: order,
		Label:        plugin.Label(),
		HasCatalog:   hasCatalog,
		HasDiscovery: hasDiscovery,
	}
}

// catalogOrderProvider is an optional interface for providers that specify
// their catalog discovery order.
type catalogOrderProvider interface {
	CatalogOrder() DiscoveryOrder
}

// discoveryProvider is an optional interface for providers that implement
// the legacy discovery hook.
type discoveryProvider interface {
	Discovery(ctx context.Context, cctx CatalogContext) (*CatalogResult, error)
}

// GroupDiscoveryProvidersByOrder groups providers by their catalog order.
// Each group is sorted alphabetically by label.
func GroupDiscoveryProvidersByOrder(providers []DiscoveryProvider) map[DiscoveryOrder][]DiscoveryProvider {
	grouped := map[DiscoveryOrder][]DiscoveryProvider{
		DiscoveryOrderSimple:  {},
		DiscoveryOrderProfile: {},
		DiscoveryOrderPaired:  {},
		DiscoveryOrderLate:    {},
	}

	for _, p := range providers {
		order := p.CatalogOrder
		if _, ok := grouped[order]; !ok {
			order = DiscoveryOrderLate
		}
		grouped[order] = append(grouped[order], p)
	}

	for _, order := range AllDiscoveryOrders {
		sort.SliceStable(grouped[order], func(i, j int) bool {
			return strings.Compare(grouped[order][i].Label, grouped[order][j].Label) < 0
		})
	}

	return grouped
}

// NormalizeDiscoveryResult normalizes a catalog result into a map of
// provider ID -> config. Mirrors normalizePluginDiscoveryResult.
func NormalizeDiscoveryResult(providerID string, result *DiscoveryCatalogResult) map[string]*DiscoveryProviderConfig {
	if result == nil {
		return map[string]*DiscoveryProviderConfig{}
	}

	if result.Provider != nil {
		normalizedID := NormalizeProviderID(providerID)
		return map[string]*DiscoveryProviderConfig{
			normalizedID: result.Provider,
		}
	}

	if result.Providers != nil {
		normalized := make(map[string]*DiscoveryProviderConfig, len(result.Providers))
		for key, value := range result.Providers {
			normalizedKey := NormalizeProviderID(key)
			if normalizedKey == "" || value == nil {
				continue
			}
			normalized[normalizedKey] = value
		}
		return normalized
	}

	return map[string]*DiscoveryProviderConfig{}
}

// RunProviderCatalog executes a provider's catalog or discovery hook.
// Mirrors runProviderCatalog from provider-discovery.ts.
func RunProviderCatalog(ctx context.Context, dp DiscoveryProvider, cctx CatalogContext) (*CatalogResult, error) {
	// Prefer catalog over discovery.
	if dp.HasCatalog {
		if cp, ok := dp.Plugin.(CatalogProvider); ok {
			return cp.Catalog(ctx, cctx)
		}
	}
	if dp.HasDiscovery {
		if dp, ok := dp.Plugin.(discoveryProvider); ok {
			return dp.Discovery(ctx, cctx)
		}
	}
	return nil, nil
}
