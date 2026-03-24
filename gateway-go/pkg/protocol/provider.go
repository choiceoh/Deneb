// Package protocol — provider catalog wire types.
//
// These types mirror the Protobuf definitions in proto/provider.proto
// and the TypeScript types in src/protocol/generated/provider.ts.
package protocol

// ProviderMeta describes a registered model provider.
// Mirrors proto/provider.proto ProviderMeta.
type ProviderMeta struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	DocsPath *string  `json:"docsPath,omitempty"`
	Aliases  []string `json:"aliases,omitempty"`
	EnvVars  []string `json:"envVars,omitempty"`
}

// ProviderAuthMethod describes one authentication method for a provider.
// Mirrors proto/provider.proto ProviderAuthMethod.
type ProviderAuthMethod struct {
	ID    string  `json:"id"`
	Label string  `json:"label"`
	Kind  string  `json:"kind"`
	Hint  *string `json:"hint,omitempty"`
}

// ProviderCatalogEntry represents a single model in the provider catalog.
// Mirrors proto/provider.proto ProviderCatalogEntry.
type ProviderCatalogEntry struct {
	Provider      string  `json:"provider"`
	ModelID       string  `json:"modelId"`
	Label         *string `json:"label,omitempty"`
	ContextWindow *int64  `json:"contextWindow,omitempty"`
	Reasoning     *bool   `json:"reasoning,omitempty"`
	APIType       *string `json:"apiType,omitempty"`
}

// ProviderCatalogSnapshot is a point-in-time view of all discovered models.
// Mirrors proto/provider.proto ProviderCatalogSnapshot.
type ProviderCatalogSnapshot struct {
	Providers  []ProviderMeta         `json:"providers"`
	Entries    []ProviderCatalogEntry `json:"entries"`
	SnapshotAt int64                  `json:"snapshotAt"`
}
