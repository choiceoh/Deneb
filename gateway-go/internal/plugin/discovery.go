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
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PluginCandidate describes a discovered plugin candidate.
type PluginCandidate struct {
	IDHint             string              `json:"idHint"`
	Source             string              `json:"source"`
	SetupSource        string              `json:"setupSource,omitempty"`
	RootDir            string              `json:"rootDir"`
	Origin             PluginOrigin        `json:"origin"`
	Format             PluginFormat        `json:"format,omitempty"`
	BundleFormat       PluginBundleFormat  `json:"bundleFormat,omitempty"`
	WorkspaceDir       string              `json:"workspaceDir,omitempty"`
	PackageName        string              `json:"packageName,omitempty"`
	PackageVersion     string              `json:"packageVersion,omitempty"`
	PackageDescription string              `json:"packageDescription,omitempty"`
	PackageDir         string              `json:"packageDir,omitempty"`
	PackageManifest    *DenebPackageManifest `json:"packageManifest,omitempty"`
}

// DenebPackageManifest holds the deneb-specific section of a package.json.
type DenebPackageManifest struct {
	ID          string   `json:"id,omitempty"`
	Extensions  []string `json:"extensions,omitempty"`
	SetupEntry  string   `json:"setupEntry,omitempty"`
	Channels    []string `json:"channels,omitempty"`
	Providers   []string `json:"providers,omitempty"`
	Skills      []string `json:"skills,omitempty"`
	Hooks       []string `json:"hooks,omitempty"`
}

// PluginDiscoveryResult holds the result of plugin discovery.
type PluginDiscoveryResult struct {
	Candidates  []PluginCandidate  `json:"candidates"`
	Diagnostics []PluginDiagnostic `json:"diagnostics"`
}

// CandidateBlockReason identifies why a candidate was blocked.
type CandidateBlockReason string

const (
	BlockSourceEscapesRoot    CandidateBlockReason = "source_escapes_root"
	BlockPathStatFailed       CandidateBlockReason = "path_stat_failed"
	BlockPathWorldWritable    CandidateBlockReason = "path_world_writable"
	BlockSuspiciousOwnership  CandidateBlockReason = "path_suspicious_ownership"
)

type candidateBlockIssue struct {
	reason        CandidateBlockReason
	sourcePath    string
	rootPath      string
	targetPath    string
	sourceRealPath string
	rootRealPath  string
	modeBits      uint32
	foundUID      uint32
	expectedUID   uint32
}

// PluginSourceRoots holds the root directories for plugin discovery.
type PluginSourceRoots struct {
	Workspace string
	Global    string
	Stock     string
}

// extensionExts are the allowed plugin file extensions.
var extensionExts = map[string]bool{
	".ts": true, ".js": true, ".mts": true, ".cts": true, ".mjs": true, ".cjs": true,
}

// defaultEntryCandidates are the default index file names to look for.
var defaultEntryCandidates = []string{
	"index.ts", "index.js", "index.mts", "index.mjs",
}

const defaultDiscoveryCacheMs = 1000

// PluginDiscoverer discovers plugin candidates from filesystem roots.
type PluginDiscoverer struct {
	mu          sync.Mutex
	cache       map[string]*discoveryEntry
	logger      *slog.Logger
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
	OwnershipUID int
	NoCache      bool
	Env          map[string]string
}

type discoveryContext struct {
	candidates   *[]PluginCandidate
	diagnostics  *[]PluginDiagnostic
	seen         map[string]bool
	ownershipUID int
}

func (d *PluginDiscoverer) resolveCacheTTL(env map[string]string) time.Duration {
	if env != nil {
		if _, ok := env["DENEB_DISABLE_PLUGIN_DISCOVERY_CACHE"]; ok {
			return 0
		}
		if raw, ok := env["DENEB_PLUGIN_DISCOVERY_CACHE_MS"]; ok {
			raw = strings.TrimSpace(raw)
			if raw == "" || raw == "0" {
				return 0
			}
			if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
				return time.Duration(ms) * time.Millisecond
			}
		}
	}
	return defaultDiscoveryCacheMs * time.Millisecond
}

func (d *PluginDiscoverer) buildCacheKey(params DiscoverPluginsParams) string {
	return fmt.Sprintf("%s::%d::%s::%s::%s",
		params.WorkspaceDir,
		params.OwnershipUID,
		params.Roots.Global,
		params.Roots.Stock,
		strings.Join(params.ExtraPaths, ","),
	)
}

// --- Security checks ---

func checkSourceEscapesRoot(source, rootDir string) *candidateBlockIssue {
	sourceReal, err := filepath.EvalSymlinks(source)
	if err != nil {
		return nil
	}
	rootReal, err := filepath.EvalSymlinks(rootDir)
	if err != nil {
		return nil
	}
	if isPathInside(rootReal, sourceReal) {
		return nil
	}
	return &candidateBlockIssue{
		reason:         BlockSourceEscapesRoot,
		sourcePath:     source,
		rootPath:       rootDir,
		targetPath:     source,
		sourceRealPath: sourceReal,
		rootRealPath:   rootReal,
	}
}

func isPathInside(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != ".."
}

func checkPathStatAndPermissions(source, rootDir string, origin PluginOrigin, uid int) *candidateBlockIssue {
	paths := []string{rootDir, source}
	seen := make(map[string]bool)
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true

		info, err := os.Stat(abs)
		if err != nil {
			return &candidateBlockIssue{
				reason:     BlockPathStatFailed,
				sourcePath: source,
				rootPath:   rootDir,
				targetPath: abs,
			}
		}

		// Check world-writable on Unix.
		mode := info.Mode().Perm()
		if mode&0o002 != 0 {
			// For bundled origins, attempt repair.
			if origin == OriginBundled {
				repaired := mode &^ 0o022
				if err := os.Chmod(abs, repaired); err == nil {
					info, err = os.Stat(abs)
					if err != nil {
						return &candidateBlockIssue{
							reason:     BlockPathStatFailed,
							sourcePath: source,
							rootPath:   rootDir,
							targetPath: abs,
						}
					}
					mode = info.Mode().Perm()
				}
			}
			if mode&0o002 != 0 {
				return &candidateBlockIssue{
					reason:     BlockPathWorldWritable,
					sourcePath: source,
					rootPath:   rootDir,
					targetPath: abs,
					modeBits:   uint32(mode),
				}
			}
		}

		// Check ownership for non-bundled origins.
		if origin != OriginBundled && uid >= 0 {
			if sysUID := fileUID(info); sysUID >= 0 && sysUID != uid && sysUID != 0 {
				return &candidateBlockIssue{
					reason:      BlockSuspiciousOwnership,
					sourcePath:  source,
					rootPath:    rootDir,
					targetPath:  abs,
					foundUID:    uint32(sysUID),
					expectedUID: uint32(uid),
				}
			}
		}
	}
	return nil
}

func findCandidateBlockIssue(source, rootDir string, origin PluginOrigin, uid int) *candidateBlockIssue {
	if issue := checkSourceEscapesRoot(source, rootDir); issue != nil {
		return issue
	}
	return checkPathStatAndPermissions(source, rootDir, origin, uid)
}

func formatCandidateBlockMessage(issue *candidateBlockIssue) string {
	switch issue.reason {
	case BlockSourceEscapesRoot:
		return fmt.Sprintf("blocked plugin candidate: source escapes plugin root (%s -> %s; root=%s)",
			issue.sourcePath, issue.sourceRealPath, issue.rootRealPath)
	case BlockPathStatFailed:
		return fmt.Sprintf("blocked plugin candidate: cannot stat path (%s)", issue.targetPath)
	case BlockPathWorldWritable:
		return fmt.Sprintf("blocked plugin candidate: world-writable path (%s, mode=%04o)",
			issue.targetPath, issue.modeBits)
	case BlockSuspiciousOwnership:
		return fmt.Sprintf("blocked plugin candidate: suspicious ownership (%s, uid=%d, expected uid=%d or root)",
			issue.targetPath, issue.foundUID, issue.expectedUID)
	default:
		return fmt.Sprintf("blocked plugin candidate: %s (%s)", issue.reason, issue.targetPath)
	}
}

func (d *PluginDiscoverer) isUnsafeCandidate(ctx *discoveryContext, source, rootDir string, origin PluginOrigin) bool {
	issue := findCandidateBlockIssue(source, rootDir, origin, ctx.ownershipUID)
	if issue == nil {
		return false
	}
	*ctx.diagnostics = append(*ctx.diagnostics, PluginDiagnostic{
		Level:   "warn",
		Source:  issue.targetPath,
		Message: formatCandidateBlockMessage(issue),
	})
	return true
}

// --- Discovery helpers ---

func isExtensionFile(filePath string) bool {
	ext := filepath.Ext(filePath)
	if !extensionExts[ext] {
		return false
	}
	return !strings.HasSuffix(filePath, ".d.ts")
}

func shouldIgnoreScannedDirectory(name string) bool {
	normalized := strings.TrimSpace(strings.ToLower(name))
	if normalized == "" {
		return true
	}
	if strings.HasSuffix(normalized, ".bak") {
		return true
	}
	if strings.Contains(normalized, ".backup-") {
		return true
	}
	if strings.Contains(normalized, ".disabled") {
		return true
	}
	return false
}

func readPackageManifest(dir string) (*packageJSON, error) {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil, err
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}

type packageJSON struct {
	Name        string                `json:"name"`
	Version     string                `json:"version"`
	Description string                `json:"description"`
	Deneb       *packageDenebSection  `json:"deneb"`
}

type packageDenebSection struct {
	Plugin     *DenebPackageManifest `json:"plugin,omitempty"`
	Extensions []string              `json:"extensions,omitempty"`
}

func deriveIDHint(filePath, packageName string, hasMultipleExts bool) string {
	base := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	raw := strings.TrimSpace(packageName)
	if raw == "" {
		return base
	}
	// Prefer unscoped name.
	unscoped := raw
	if idx := strings.LastIndex(raw, "/"); idx >= 0 {
		unscoped = raw[idx+1:]
	}
	normalized := unscoped
	if strings.HasSuffix(normalized, "-provider") && len(normalized) > len("-provider") {
		normalized = normalized[:len(normalized)-len("-provider")]
	}
	if !hasMultipleExts {
		return normalized
	}
	return normalized + "/" + base
}

func (d *PluginDiscoverer) addCandidate(ctx *discoveryContext, params addCandidateParams) {
	resolved, err := filepath.Abs(params.source)
	if err != nil {
		return
	}
	if ctx.seen[resolved] {
		return
	}
	rootReal, err := filepath.EvalSymlinks(params.rootDir)
	if err != nil {
		rootReal, _ = filepath.Abs(params.rootDir)
	}
	if d.isUnsafeCandidate(ctx, resolved, rootReal, params.origin) {
		return
	}
	ctx.seen[resolved] = true

	candidate := PluginCandidate{
		IDHint:       params.idHint,
		Source:       resolved,
		SetupSource:  params.setupSource,
		RootDir:      rootReal,
		Origin:       params.origin,
		Format:       params.format,
		BundleFormat: params.bundleFormat,
		WorkspaceDir: params.workspaceDir,
		PackageDir:   params.packageDir,
	}
	if candidate.Format == "" {
		candidate.Format = FormatDeneb
	}
	if params.manifest != nil {
		candidate.PackageName = strings.TrimSpace(params.manifest.Name)
		candidate.PackageVersion = strings.TrimSpace(params.manifest.Version)
		candidate.PackageDescription = strings.TrimSpace(params.manifest.Description)
		if params.manifest.Deneb != nil && params.manifest.Deneb.Plugin != nil {
			candidate.PackageManifest = params.manifest.Deneb.Plugin
		}
	}
	*ctx.candidates = append(*ctx.candidates, candidate)
}

type addCandidateParams struct {
	idHint       string
	source       string
	setupSource  string
	rootDir      string
	origin       PluginOrigin
	format       PluginFormat
	bundleFormat PluginBundleFormat
	workspaceDir string
	manifest     *packageJSON
	packageDir   string
}

func (d *PluginDiscoverer) discoverInDirectory(ctx *discoveryContext, dir string, origin PluginOrigin, workspaceDir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			*ctx.diagnostics = append(*ctx.diagnostics, PluginDiagnostic{
				Level:   "warn",
				Source:  dir,
				Message: fmt.Sprintf("failed to read extensions dir: %s (%v)", dir, err),
			})
		}
		return
	}

	for _, entry := range entries {
		fullPath := filepath.Join(dir, entry.Name())

		if entry.Type().IsRegular() {
			if !isExtensionFile(fullPath) {
				continue
			}
			d.addCandidate(ctx, addCandidateParams{
				idHint:       strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
				source:       fullPath,
				rootDir:      filepath.Dir(fullPath),
				origin:       origin,
				workspaceDir: workspaceDir,
			})
			continue
		}

		if !entry.IsDir() {
			continue
		}
		if shouldIgnoreScannedDirectory(entry.Name()) {
			continue
		}

		manifest, _ := readPackageManifest(fullPath)
		extensions := resolvePackageExtensions(manifest)

		if len(extensions) > 0 {
			for _, extPath := range extensions {
				source := filepath.Join(fullPath, extPath)
				if !fileExists(source) {
					*ctx.diagnostics = append(*ctx.diagnostics, PluginDiagnostic{
						Level:   "error",
						Source:  fullPath,
						Message: fmt.Sprintf("extension entry not found: %s", extPath),
					})
					continue
				}
				d.addCandidate(ctx, addCandidateParams{
					idHint:       deriveIDHint(source, nameFromPkg(manifest), len(extensions) > 1),
					source:       source,
					rootDir:      fullPath,
					origin:       origin,
					workspaceDir: workspaceDir,
					manifest:     manifest,
					packageDir:   fullPath,
				})
			}
			continue
		}

		// Check for bundle format.
		if d.discoverBundleInRoot(ctx, fullPath, origin, workspaceDir) {
			continue
		}

		// Fall back to index file discovery.
		for _, candidate := range defaultEntryCandidates {
			indexPath := filepath.Join(fullPath, candidate)
			if fileExists(indexPath) && isExtensionFile(indexPath) {
				d.addCandidate(ctx, addCandidateParams{
					idHint:       entry.Name(),
					source:       indexPath,
					rootDir:      fullPath,
					origin:       origin,
					workspaceDir: workspaceDir,
					manifest:     manifest,
					packageDir:   fullPath,
				})
				break
			}
		}
	}
}

func (d *PluginDiscoverer) discoverBundleInRoot(ctx *discoveryContext, rootDir string, origin PluginOrigin, workspaceDir string) bool {
	// Check for bundle manifest (deneb-plugin.json or similar).
	bundlePath := filepath.Join(rootDir, "deneb-plugin.json")
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		return false
	}
	var bundle struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &bundle); err != nil || bundle.ID == "" {
		*ctx.diagnostics = append(*ctx.diagnostics, PluginDiagnostic{
			Level:   "error",
			Source:  bundlePath,
			Message: fmt.Sprintf("invalid bundle manifest: %v", err),
		})
		return false
	}
	d.addCandidate(ctx, addCandidateParams{
		idHint:       bundle.ID,
		source:       rootDir,
		rootDir:      rootDir,
		origin:       origin,
		format:       FormatBundle,
		bundleFormat: BundleFormatJSON,
		workspaceDir: workspaceDir,
	})
	return true
}

func (d *PluginDiscoverer) discoverFromPath(ctx *discoveryContext, rawPath string, origin PluginOrigin, workspaceDir string) {
	resolved, err := filepath.Abs(rawPath)
	if err != nil {
		*ctx.diagnostics = append(*ctx.diagnostics, PluginDiagnostic{
			Level:   "error",
			Source:  rawPath,
			Message: fmt.Sprintf("plugin path not found: %s", rawPath),
		})
		return
	}

	info, err := os.Stat(resolved)
	if err != nil {
		*ctx.diagnostics = append(*ctx.diagnostics, PluginDiagnostic{
			Level:   "error",
			Source:  resolved,
			Message: fmt.Sprintf("plugin path not found: %s", resolved),
		})
		return
	}

	if info.Mode().IsRegular() {
		if !isExtensionFile(resolved) {
			*ctx.diagnostics = append(*ctx.diagnostics, PluginDiagnostic{
				Level:   "error",
				Source:  resolved,
				Message: fmt.Sprintf("plugin path is not a supported file: %s", resolved),
			})
			return
		}
		d.addCandidate(ctx, addCandidateParams{
			idHint:       strings.TrimSuffix(filepath.Base(resolved), filepath.Ext(resolved)),
			source:       resolved,
			rootDir:      filepath.Dir(resolved),
			origin:       origin,
			workspaceDir: workspaceDir,
		})
		return
	}

	if info.IsDir() {
		manifest, _ := readPackageManifest(resolved)
		extensions := resolvePackageExtensions(manifest)

		if len(extensions) > 0 {
			for _, extPath := range extensions {
				source := filepath.Join(resolved, extPath)
				if !fileExists(source) {
					continue
				}
				d.addCandidate(ctx, addCandidateParams{
					idHint:       deriveIDHint(source, nameFromPkg(manifest), len(extensions) > 1),
					source:       source,
					rootDir:      resolved,
					origin:       origin,
					workspaceDir: workspaceDir,
					manifest:     manifest,
					packageDir:   resolved,
				})
			}
			return
		}

		if d.discoverBundleInRoot(ctx, resolved, origin, workspaceDir) {
			return
		}

		// Fall back to index file, then directory scan.
		for _, candidate := range defaultEntryCandidates {
			indexPath := filepath.Join(resolved, candidate)
			if fileExists(indexPath) && isExtensionFile(indexPath) {
				d.addCandidate(ctx, addCandidateParams{
					idHint:       filepath.Base(resolved),
					source:       indexPath,
					rootDir:      resolved,
					origin:       origin,
					workspaceDir: workspaceDir,
					manifest:     manifest,
					packageDir:   resolved,
				})
				return
			}
		}

		d.discoverInDirectory(ctx, resolved, origin, workspaceDir)
	}
}

// --- Utilities ---

func resolvePackageExtensions(pkg *packageJSON) []string {
	if pkg == nil || pkg.Deneb == nil {
		return nil
	}
	if pkg.Deneb.Plugin != nil && len(pkg.Deneb.Plugin.Extensions) > 0 {
		return pkg.Deneb.Plugin.Extensions
	}
	if len(pkg.Deneb.Extensions) > 0 {
		return pkg.Deneb.Extensions
	}
	return nil
}

func nameFromPkg(pkg *packageJSON) string {
	if pkg == nil {
		return ""
	}
	return pkg.Name
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// fileUID extracts the file UID on Unix. Returns -1 if unavailable.
func fileUID(info fs.FileInfo) int {
	return fileUIDFromInfo(info)
}
