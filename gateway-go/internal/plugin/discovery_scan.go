package plugin

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// extensionExts are the allowed plugin file extensions.
var extensionExts = map[string]bool{
	".ts": true, ".js": true, ".mts": true, ".cts": true, ".mjs": true, ".cjs": true,
}

// defaultEntryCandidates are the default index file names to look for.
var defaultEntryCandidates = []string{
	"index.ts", "index.js", "index.mts", "index.mjs",
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
	Name        string               `json:"name"`
	Version     string               `json:"version"`
	Description string               `json:"description"`
	Deneb       *packageDenebSection `json:"deneb"`
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
