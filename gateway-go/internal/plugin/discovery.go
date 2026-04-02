// discovery.go — Plugin discovery system for the Go gateway.
// Mirrors src/plugins/discovery.ts (846 LOC).
//
// Discovers plugin candidates from filesystem roots with security checks
// (path escapes, world-writable, ownership), caching, bundle format support,
// and multi-root scanning (workspace, global, stock, extra paths).
//
// Simplified for the single-user DGX Spark deployment: no Windows paths,
// no Node.js-specific boundary file handling, no symlink hardlink rejection.
package plugin

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// PluginCandidate describes a discovered plugin candidate.
type PluginCandidate struct {
	IDHint             string                `json:"idHint"`
	Source             string                `json:"source"`
	SetupSource        string                `json:"setupSource,omitempty"`
	RootDir            string                `json:"rootDir"`
	Origin             PluginOrigin          `json:"origin"`
	Format             PluginFormat          `json:"format,omitempty"`
	BundleFormat       PluginBundleFormat    `json:"bundleFormat,omitempty"`
	WorkspaceDir       string                `json:"workspaceDir,omitempty"`
	PackageName        string                `json:"packageName,omitempty"`
	PackageVersion     string                `json:"packageVersion,omitempty"`
	PackageDescription string                `json:"packageDescription,omitempty"`
	PackageDir         string                `json:"packageDir,omitempty"`
	PackageManifest    *DenebPackageManifest `json:"packageManifest,omitempty"`
}

// DenebPackageManifest holds the deneb-specific section of a package.json.
type DenebPackageManifest struct {
	ID         string   `json:"id,omitempty"`
	Extensions []string `json:"extensions,omitempty"`
	SetupEntry string   `json:"setupEntry,omitempty"`
	Channels   []string `json:"channels,omitempty"`
	Providers  []string `json:"providers,omitempty"`
	Skills     []string `json:"skills,omitempty"`
	Hooks      []string `json:"hooks,omitempty"`
}

// PluginDiscoveryResult holds the result of plugin discovery.
type PluginDiscoveryResult struct {
	Candidates  []PluginCandidate  `json:"candidates"`
	Diagnostics []PluginDiagnostic `json:"diagnostics"`
}

// PluginSourceRoots holds the root directories for plugin discovery.
type PluginSourceRoots struct {
	Workspace string
	Global    string
	Stock     string
}

const defaultDiscoveryCacheMs = 1000

// PluginDiscoverer discovers plugin candidates from filesystem roots.
type PluginDiscoverer struct {
	mu     sync.Mutex
	cache  map[string]*discoveryEntry
	logger *slog.Logger
}

type discoveryEntry struct {
	expiresAt time.Time
	result    *PluginDiscoveryResult
}

// NewPluginDiscoverer creates a new plugin discoverer.
func NewPluginDiscoverer(logger *slog.Logger) *PluginDiscoverer {
	return &PluginDiscoverer{
		cache:  make(map[string]*discoveryEntry),
		logger: logger,
	}
}

// ClearCache clears the discovery cache.
func (d *PluginDiscoverer) ClearCache() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cache = make(map[string]*discoveryEntry)
}

// DiscoverPlugins discovers plugins from the given roots and paths.
// Mirrors discoverDenebPlugins from discovery.ts.
func (d *PluginDiscoverer) DiscoverPlugins(params DiscoverPluginsParams) *PluginDiscoveryResult {
	cacheTTL := d.resolveCacheTTL(params.Env)
	cacheKey := d.buildCacheKey(params)

	if cacheTTL > 0 && !params.NoCache {
		d.mu.Lock()
		entry, ok := d.cache[cacheKey]
		d.mu.Unlock()
		if ok && time.Now().Before(entry.expiresAt) {
			return entry.result
		}
	}

	candidates := make([]PluginCandidate, 0)
	diagnostics := make([]PluginDiagnostic, 0)
	seen := make(map[string]bool)
	ctx := &discoveryContext{
		candidates:   &candidates,
		diagnostics:  &diagnostics,
		seen:         seen,
		ownershipUID: params.OwnershipUID,
	}

	// Extra paths (config-specified, highest priority).
	for _, p := range params.ExtraPaths {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		d.discoverFromPath(ctx, trimmed, OriginConfig, params.WorkspaceDir)
	}

	// Workspace extensions.
	if params.Roots.Workspace != "" && params.WorkspaceDir != "" {
		d.discoverInDirectory(ctx, params.Roots.Workspace, OriginWorkspace, params.WorkspaceDir)
	}

	// Bundled/stock extensions.
	if params.Roots.Stock != "" {
		d.discoverInDirectory(ctx, params.Roots.Stock, OriginBundled, "")
	}

	// Global extensions (lowest auto-discovery priority).
	if params.Roots.Global != "" {
		d.discoverInDirectory(ctx, params.Roots.Global, OriginGlobal, "")
	}

	result := &PluginDiscoveryResult{
		Candidates:  candidates,
		Diagnostics: diagnostics,
	}

	if cacheTTL > 0 && !params.NoCache {
		d.mu.Lock()
		d.cache[cacheKey] = &discoveryEntry{
			expiresAt: time.Now().Add(cacheTTL),
			result:    result,
		}
		d.mu.Unlock()
	}

	return result
}

// DiscoverPluginsParams holds parameters for DiscoverPlugins.
type DiscoverPluginsParams struct {
	Roots        PluginSourceRoots
	WorkspaceDir string
	ExtraPaths   []string
	// OwnershipUID is the expected file owner UID for non-bundled plugins.
	// Set to -1 (default) to skip ownership checks. A value >= 0 enables
	// the check: files not owned by OwnershipUID or root are blocked.
	OwnershipUID int
	NoCache      bool
	Env          map[string]string
}

// NewDiscoverPluginsParams returns params with safe defaults (ownership check disabled).
func NewDiscoverPluginsParams() DiscoverPluginsParams {
	return DiscoverPluginsParams{OwnershipUID: -1}
}

type discoveryContext struct {
	candidates   *[]PluginCandidate
	diagnostics  *[]PluginDiagnostic
	seen         map[string]bool
	ownershipUID int
}

func (d *PluginDiscoverer) buildCacheKey(params DiscoverPluginsParams) string {
	return fmt.Sprintf("%s::%d::%s::%s::%d::%s",
		params.WorkspaceDir,
		params.OwnershipUID,
		params.Roots.Global,
		params.Roots.Stock,
		len(params.ExtraPaths),
		strings.Join(params.ExtraPaths, "\x00"),
	)
}
