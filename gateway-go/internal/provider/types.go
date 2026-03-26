// Package provider defines the model provider plugin system.
//
// This mirrors the TypeScript ProviderPlugin interface from
// src/plugins/types-provider.ts. Providers supply LLM model access,
// authentication, and catalog discovery.
package provider

// AuthMethod describes one authentication method for a provider.
type AuthMethod struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"` // "oauth", "api_key", "token", "device_code", "custom"
	Hint  string `json:"hint,omitempty"`
}

// CatalogContext provides context for catalog discovery.
type CatalogContext struct {
	Config   map[string]any `json:"config,omitempty"`
	AgentDir string         `json:"agentDir,omitempty"`
	Env      map[string]string
}

// CatalogEntry represents a single model in the provider catalog.
type CatalogEntry struct {
	Provider      string `json:"provider"`
	ModelID       string `json:"modelId"`
	Label         string `json:"label,omitempty"`
	ContextWindow int64  `json:"contextWindow,omitempty"`
	Reasoning     bool   `json:"reasoning,omitempty"`
	APIType       string `json:"apiType,omitempty"`
}

// CatalogResult is the result of a catalog discovery call.
type CatalogResult struct {
	Entries []CatalogEntry `json:"entries"`
}

// RuntimeAuthContext provides context for runtime auth preparation.
type RuntimeAuthContext struct {
	Provider  string `json:"provider"`
	ModelID   string `json:"modelId"`
	APIKey    string `json:"apiKey"`
	AuthMode  string `json:"authMode"`
	ProfileID string `json:"profileId,omitempty"`
}

// PreparedAuth is the result of runtime auth preparation.
type PreparedAuth struct {
	APIKey    string `json:"apiKey"`
	BaseURL   string `json:"baseUrl,omitempty"`
	ExpiresAt int64  `json:"expiresAt,omitempty"`
}

// DynamicModelContext provides context for dynamic model resolution.
type DynamicModelContext struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

// RuntimeModel represents a resolved model ready for inference.
type RuntimeModel struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
	BaseURL  string `json:"baseUrl,omitempty"`
	APIType  string `json:"apiType,omitempty"`
}

// NormalizeContext provides context for model normalization.
type NormalizeContext struct {
	Provider string       `json:"provider"`
	ModelID  string       `json:"modelId"`
	Model    RuntimeModel `json:"model"`
}

// Capabilities describes provider-level feature flags.
type Capabilities struct {
	SupportsStreaming bool `json:"supportsStreaming,omitempty"`
	SupportsCaching   bool `json:"supportsCaching,omitempty"`
	SupportsTools     bool `json:"supportsTools,omitempty"`
}
