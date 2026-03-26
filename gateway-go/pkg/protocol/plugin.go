// Package protocol — plugin lifecycle wire types.
//
// These types mirror the Protobuf definitions in proto/plugin.proto
// and the TypeScript types in src/protocol/generated/plugin.ts.
package protocol

// PluginKind identifies the type of a plugin.
// Mirrors proto/plugin.proto PluginKind enum.
type PluginKind string

const (
	PluginKindUnspecified PluginKind = ""
	PluginKindChannel     PluginKind = "channel"
	PluginKindProvider    PluginKind = "provider"
	PluginKindFeature     PluginKind = "feature"
)

// PluginMeta describes a registered plugin.
// Mirrors proto/plugin.proto PluginMeta.
type PluginMeta struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Kind        PluginKind `json:"kind"`
	Version     string     `json:"version"`
	Enabled     bool       `json:"enabled"`
	Description *string    `json:"description,omitempty"`
	Source      *string    `json:"source,omitempty"`
}

// PluginHealthStatus represents the health of a single plugin.
// Mirrors proto/plugin.proto PluginHealthStatus.
type PluginHealthStatus struct {
	PluginID    string  `json:"pluginId"`
	Healthy     bool    `json:"healthy"`
	Error       *string `json:"error,omitempty"`
	LastCheckAt *int64  `json:"lastCheckAt,omitempty"`
	UptimeMs    *int64  `json:"uptimeMs,omitempty"`
}

// PluginRegistrySnapshot is a point-in-time view of all registered plugins.
// Mirrors proto/plugin.proto PluginRegistrySnapshot.
type PluginRegistrySnapshot struct {
	Plugins    []PluginMeta         `json:"plugins"`
	Health     []PluginHealthStatus `json:"health"`
	SnapshotAt int64                `json:"snapshotAt"`
}
