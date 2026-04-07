// catalog.go — Catalog building helpers for the Go gateway.
// Mirrors src/plugins/provider-catalog.ts (300 LOC).
//
// Provides helper functions for building provider catalog results:
// - FindCatalogTemplate: case-insensitive model search
// - BuildSingleProviderAPIKeyCatalog: single provider + API key
// - BuildPairedProviderAPIKeyCatalog: multiple providers sharing one API key
package provider

import (
	"strings"
)

// CatalogTemplateEntry describes a model entry for template matching.
type CatalogTemplateEntry struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
}

// FindCatalogTemplate searches entries for a matching provider/template ID pair.
// Uses case-insensitive comparison. Returns nil if not found.
func FindCatalogTemplate(entries []CatalogTemplateEntry, providerID string, templateIDs []string) *CatalogTemplateEntry {
	lowerProvider := strings.ToLower(providerID)
	for _, templateID := range templateIDs {
		lowerTemplate := strings.ToLower(templateID)
		for idx := range entries {
			if strings.ToLower(entries[idx].Provider) == lowerProvider &&
				strings.ToLower(entries[idx].ID) == lowerTemplate {
				return &entries[idx]
			}
		}
	}
	return nil
}

// ModelProviderCatalog holds a single provider configuration for catalog results.
type ModelProviderCatalog struct {
	ID      string         `json:"id,omitempty"`
	BaseURL string         `json:"baseUrl,omitempty"`
	APIKey  string         `json:"apiKey,omitempty"`
	API     string         `json:"api,omitempty"`
	Models  map[string]any `json:"models,omitempty"`
	Headers map[string]any `json:"headers,omitempty"`
}

// SingleProviderCatalogResult holds a single provider catalog result.
type SingleProviderCatalogResult struct {
	Provider *ModelProviderCatalog `json:"provider,omitempty"`
}

// PairedProviderCatalogResult holds multiple providers sharing one API key.
type PairedProviderCatalogResult struct {
	Providers map[string]*ModelProviderCatalog `json:"providers,omitempty"`
}

// CatalogBuilderContext provides context for building catalog results.
type CatalogBuilderContext struct {
	Config     map[string]any
	APIKey     string
	ProviderID string
}

// BuildSingleProviderAPIKeyCatalog creates a catalog result for a single provider
// with an API key. Returns nil if no API key is available.
//
// If allowExplicitBaseUrl is true and the config has an explicit baseUrl for
// the provider, it will be merged into the result.
func BuildSingleProviderAPIKeyCatalog(params SingleProviderCatalogParams) *SingleProviderCatalogResult {
	if params.APIKey == "" {
		return nil
	}

	provider := params.BuildProvider()
	if provider == nil {
		return nil
	}

	provider.APIKey = params.APIKey

	// Merge explicit baseUrl from config if allowed.
	if params.AllowExplicitBaseURL && params.Config != nil {
		if models, ok := params.Config["models"].(map[string]any); ok {
			if providers, ok := models["providers"].(map[string]any); ok {
				if provConfig, ok := providers[params.ProviderID].(map[string]any); ok {
					if baseURL, ok := provConfig["baseUrl"].(string); ok {
						trimmed := strings.TrimSpace(baseURL)
						if trimmed != "" {
							provider.BaseURL = trimmed
						}
					}
				}
			}
		}
	}

	return &SingleProviderCatalogResult{Provider: provider}
}

// SingleProviderCatalogParams holds parameters for BuildSingleProviderAPIKeyCatalog.
type SingleProviderCatalogParams struct {
	Config               map[string]any
	ProviderID           string
	APIKey               string
	AllowExplicitBaseURL bool
	BuildProvider        func() *ModelProviderCatalog
}

// BuildPairedProviderAPIKeyCatalog creates a catalog result for multiple
// providers sharing one API key. Returns nil if no API key is available.
func BuildPairedProviderAPIKeyCatalog(params PairedProviderCatalogParams) *PairedProviderCatalogResult {
	if params.APIKey == "" {
		return nil
	}

	providers := params.BuildProviders()
	if providers == nil || len(providers) == 0 {
		return nil
	}

	// Inject the API key into all providers.
	for _, p := range providers {
		p.APIKey = params.APIKey
	}

	return &PairedProviderCatalogResult{Providers: providers}
}

// PairedProviderCatalogParams holds parameters for BuildPairedProviderAPIKeyCatalog.
type PairedProviderCatalogParams struct {
	ProviderID     string
	APIKey         string
	BuildProviders func() map[string]*ModelProviderCatalog
}
