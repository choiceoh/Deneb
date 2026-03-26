// manifest_registry.go — Full plugin manifest registry with deduplication.
// Mirrors src/plugins/manifest-registry.ts (730 LOC).
//
// Extends the basic ManifestRegistry with full record types, origin-based
// deduplication, caching with TTL, and diagnostic collection.
package plugin

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultManifestCacheMs = 1000

// PluginManifestRecord is the complete metadata for a loaded plugin.
// Mirrors PluginManifestRecord from manifest-registry.ts.
type PluginManifestRecord struct {
	ID                string              `json:"id"`
	Name              string              `json:"name,omitempty"`
	Description       string              `json:"description,omitempty"`
	Version           string              `json:"version,omitempty"`
	Format            PluginFormat         `json:"format,omitempty"`
	Origin            PluginOrigin         `json:"origin"`
	Source            string              `json:"source"`
	SetupSource       string              `json:"setupSource,omitempty"`
	RootDir           string              `json:"rootDir"`
	PackageDir        string              `json:"packageDir,omitempty"`
	ManifestPath      string              `json:"manifestPath,omitempty"`
	WorkspaceDir      string              `json:"workspaceDir,omitempty"`
	Channels          []string            `json:"channels,omitempty"`
	Providers         []string            `json:"providers,omitempty"`
	Skills            []string            `json:"skills,omitempty"`
	Hooks             []string            `json:"hooks,omitempty"`
	Schema            map[string]any      `json:"schema,omitempty"`
	PackageManifest   *DenebPackageManifest `json:"packageManifest,omitempty"`
}

// PluginManifestRegistryResult holds the result of loading the manifest registry.
type PluginManifestRegistryResult struct {
	Plugins     []PluginManifestRecord `json:"plugins"`
	Diagnostics []PluginDiagnostic     `json:"diagnostics"`
}

// FullManifestRegistry extends ManifestRegistry with discovery, deduplication,
// and caching. It's the Go equivalent of loadPluginManifestRegistry.
type FullManifestRegistry struct {
	mu          sync.Mutex
	records     map[string]*PluginManifestRecord
	cache       map[string]*manifestCacheEntry
	discoverer  *PluginDiscoverer
	logger      *slog.Logger
}

type manifestCacheEntry struct {
	expiresAt time.Time
	result    *PluginManifestRegistryResult
}

// NewFullManifestRegistry creates a new full manifest registry.
func NewFullManifestRegistry(discoverer *PluginDiscoverer, logger *slog.Logger) *FullManifestRegistry {
	return &FullManifestRegistry{
		records:    make(map[string]*PluginManifestRecord),
		cache:      make(map[string]*manifestCacheEntry),
		discoverer: discoverer,
		logger:     logger,
	}
}

// ClearCache clears the manifest registry cache.
func (r *FullManifestRegistry) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = make(map[string]*manifestCacheEntry)
}

// Load discovers and loads the plugin manifest registry with deduplication.
// Mirrors loadPluginManifestRegistry from manifest-registry.ts.
func (r *FullManifestRegistry) Load(params LoadManifestRegistryParams) *PluginManifestRegistryResult {
	cacheTTL := r.resolveCacheTTL(params.Env)
	cacheKey := r.buildCacheKey(params)

	if cacheTTL > 0 && !params.NoCache {
		r.mu.Lock()
		entry, ok := r.cache[cacheKey]
		r.mu.Unlock()
		if ok && time.Now().Before(entry.expiresAt) {
			return entry.result
		}
	}

	// Run plugin discovery.
	discovery := r.discoverer.DiscoverPlugins(DiscoverPluginsParams{
		Roots:        params.Roots,
		WorkspaceDir: params.WorkspaceDir,
		ExtraPaths:   params.ExtraPaths,
		OwnershipUID: params.OwnershipUID,
		Env:          params.Env,
	})

	diagnostics := append([]PluginDiagnostic{}, discovery.Diagnostics...)
	seenIDs := make(map[string]*seenIDEntry)
	records := make([]PluginManifestRecord, 0, len(discovery.Candidates))

	for i := range discovery.Candidates {
		candidate := &discovery.Candidates[i]
		record := r.buildRecord(candidate)
		if record == nil {
			continue
		}

		existingSeen, hasSeen := seenIDs[record.ID]
		if hasSeen {
			// Resolve duplicate precedence.
			existingRank := resolveDuplicatePrecedenceRank(existingSeen.record)
			newRank := resolveDuplicatePrecedenceRank(record)
			if newRank >= existingRank {
				// Keep existing record.
				continue
			}
			// Replace existing with new.
			records[existingSeen.index] = *record
			seenIDs[record.ID] = &seenIDEntry{record: record, index: existingSeen.index}
		} else {
			idx := len(records)
			records = append(records, *record)
			seenIDs[record.ID] = &seenIDEntry{record: record, index: idx}
		}
	}

	// Store records in the registry map for fast lookup.
	r.mu.Lock()
	r.records = make(map[string]*PluginManifestRecord, len(records))
	for i := range records {
		r.records[records[i].ID] = &records[i]
	}
	r.mu.Unlock()

	result := &PluginManifestRegistryResult{
		Plugins:     records,
		Diagnostics: diagnostics,
	}

	if cacheTTL > 0 && !params.NoCache {
		r.mu.Lock()
		r.cache[cacheKey] = &manifestCacheEntry{
			expiresAt: time.Now().Add(cacheTTL),
			result:    result,
		}
		r.mu.Unlock()
	}

	r.logger.Info("manifest registry loaded", "plugins", len(records))
	return result
}

// GetRecord returns a manifest record by ID.
func (r *FullManifestRegistry) GetRecord(id string) *PluginManifestRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.records[id]
}

// ListRecords returns all loaded manifest records.
func (r *FullManifestRegistry) ListRecords() []PluginManifestRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]PluginManifestRecord, 0, len(r.records))
	for _, rec := range r.records {
		result = append(result, *rec)
	}
	return result
}

// RecordCount returns the number of loaded records.
func (r *FullManifestRegistry) RecordCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.records)
}

// LoadManifestRegistryParams holds parameters for Load.
type LoadManifestRegistryParams struct {
	Roots        PluginSourceRoots
	WorkspaceDir string
	ExtraPaths   []string
	OwnershipUID int
	NoCache      bool
	Env          map[string]string
}

type seenIDEntry struct {
	record *PluginManifestRecord
	index  int
}

func (r *FullManifestRegistry) buildRecord(candidate *PluginCandidate) *PluginManifestRecord {
	record := &PluginManifestRecord{
		ID:              candidate.IDHint,
		Source:          candidate.Source,
		SetupSource:     candidate.SetupSource,
		RootDir:         candidate.RootDir,
		Origin:          candidate.Origin,
		Format:          candidate.Format,
		PackageDir:      candidate.PackageDir,
		WorkspaceDir:    candidate.WorkspaceDir,
		Name:            candidate.PackageName,
		Description:     candidate.PackageDescription,
		Version:         candidate.PackageVersion,
		PackageManifest: candidate.PackageManifest,
	}

	// Enrich from package manifest.
	if candidate.PackageManifest != nil {
		pm := candidate.PackageManifest
		if pm.ID != "" {
			record.ID = pm.ID
		}
		record.Channels = pm.Channels
		record.Providers = pm.Providers
		record.Skills = pm.Skills
		record.Hooks = pm.Hooks
	}

	if record.ID == "" {
		return nil
	}
	return record
}

// resolveDuplicatePrecedenceRank returns a rank for origin-based deduplication.
// Lower rank = higher priority.
func resolveDuplicatePrecedenceRank(record *PluginManifestRecord) int {
	rank, ok := PluginOriginRank[record.Origin]
	if !ok {
		return 99
	}
	return rank
}

func (r *FullManifestRegistry) resolveCacheTTL(env map[string]string) time.Duration {
	if env != nil {
		if _, ok := env["DENEB_DISABLE_PLUGIN_MANIFEST_CACHE"]; ok {
			return 0
		}
		if raw, ok := env["DENEB_PLUGIN_MANIFEST_CACHE_MS"]; ok {
			raw = strings.TrimSpace(raw)
			if raw == "" || raw == "0" {
				return 0
			}
			if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
				return time.Duration(ms) * time.Millisecond
			}
		}
	}
	return defaultManifestCacheMs * time.Millisecond
}

func (r *FullManifestRegistry) buildCacheKey(params LoadManifestRegistryParams) string {
	return params.WorkspaceDir + "::" + params.Roots.Global + "::" +
		params.Roots.Stock + "::" + strings.Join(params.ExtraPaths, ",")
}

// safeStatMtimeMs returns the file modification time in milliseconds, or 0.
func safeStatMtimeMs(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.ModTime().UnixMilli()
}

// NormalizeManifestLabel trims and returns a label, or empty string.
func NormalizeManifestLabel(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	return trimmed
}

// IsCompatiblePluginIDHint checks if an ID hint matches a manifest ID.
func IsCompatiblePluginIDHint(idHint, manifestID string) bool {
	if idHint == manifestID {
		return true
	}
	suffixes := []string{"-provider", "-plugin", "-sandbox"}
	for _, suffix := range suffixes {
		if idHint+suffix == manifestID || manifestID+suffix == idHint {
			return true
		}
	}
	return false
}
