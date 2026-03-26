// types_core.go — Core plugin type definitions for the Go gateway.
// Mirrors src/plugins/types.ts (barrel re-export) and types-api.ts.
//
// Consolidates core plugin types that are shared across the plugin system.
// In Go these are co-located rather than split across domain files.
package plugin

// PluginOrigin describes where a plugin was discovered.
type PluginOrigin string

const (
	OriginConfig    PluginOrigin = "config"
	OriginWorkspace PluginOrigin = "workspace"
	OriginGlobal    PluginOrigin = "global"
	OriginBundled   PluginOrigin = "bundled"
)

// PluginOriginRank maps origins to their precedence (lower = higher priority).
var PluginOriginRank = map[PluginOrigin]int{
	OriginConfig:    0,
	OriginWorkspace: 1,
	OriginGlobal:    2,
	OriginBundled:   3,
}

// PluginFormat identifies the plugin packaging format.
type PluginFormat string

const (
	FormatDeneb  PluginFormat = "deneb"
	FormatBundle PluginFormat = "bundle"
)

// PluginBundleFormat identifies the bundle manifest format.
type PluginBundleFormat string

const (
	BundleFormatJSON PluginBundleFormat = "json"
	BundleFormatYAML PluginBundleFormat = "yaml"
)

// PluginDiagnostic holds a diagnostic message from plugin discovery or loading.
type PluginDiagnostic struct {
	Level    string `json:"level"` // "error", "warn", "info"
	PluginID string `json:"pluginId,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

// PluginRuntime describes a loaded plugin's runtime state.
type PluginRuntime struct {
	ID          string       `json:"id"`
	PluginID    string       `json:"pluginId"`
	Kind        PluginKind   `json:"kind"`
	Label       string       `json:"label,omitempty"`
	Version     string       `json:"version,omitempty"`
	Origin      PluginOrigin `json:"origin"`
	Enabled     bool         `json:"enabled"`
	Source      string       `json:"source,omitempty"`
	PackageDir  string       `json:"packageDir,omitempty"`
}
