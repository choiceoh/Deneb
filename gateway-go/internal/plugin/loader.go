package plugin

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// PluginManifest describes a plugin package from its manifest file.
type PluginManifest struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Version     string     `json:"version"`
	Description string     `json:"description,omitempty"`
	Kind        PluginKind `json:"kind"`
	Author      string     `json:"author,omitempty"`
	Enabled     bool       `json:"enabled"`
	Bundled     bool       `json:"bundled,omitempty"`
	EntryPoint  string     `json:"entryPoint,omitempty"`
}

// PluginLoader discovers and loads plugins from disk.
type PluginLoader struct {
	mu        sync.RWMutex
	logger    *slog.Logger
	roots     []string // directories to scan for plugins
	manifests map[string]*PluginManifest
}

// NewPluginLoader creates a new plugin loader.
func NewPluginLoader(roots []string, logger *slog.Logger) *PluginLoader {
	return &PluginLoader{
		logger:    logger,
		roots:     roots,
		manifests: make(map[string]*PluginManifest),
	}
}

// Discover scans plugin roots for plugin manifests.
func (l *PluginLoader) Discover() ([]PluginManifest, error) {
	var manifests []PluginManifest

	for _, root := range l.roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			l.logger.Warn("failed to scan plugin root", "root", root, "error", err)
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			manifestPath := filepath.Join(root, entry.Name(), "package.json")
			manifest, err := l.loadManifest(manifestPath)
			if err != nil {
				l.logger.Debug("skipping plugin dir", "dir", entry.Name(), "error", err)
				continue
			}
			manifests = append(manifests, *manifest)
			l.mu.Lock()
			l.manifests[manifest.ID] = manifest
			l.mu.Unlock()
		}
	}

	l.logger.Info("plugin discovery complete", "found", len(manifests))
	return manifests, nil
}

// GetManifest returns a cached manifest by ID.
func (l *PluginLoader) GetManifest(id string) *PluginManifest {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.manifests[id]
}

// ListManifests returns all discovered manifests.
func (l *PluginLoader) ListManifests() []PluginManifest {
	l.mu.RLock()
	defer l.mu.RUnlock()
	result := make([]PluginManifest, 0, len(l.manifests))
	for _, m := range l.manifests {
		result = append(result, *m)
	}
	return result
}

func (l *PluginLoader) loadManifest(path string) (*PluginManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var pkg struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Deneb   *struct {
			Plugin *PluginManifest `json:"plugin"`
		} `json:"deneb"`
	}

	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	if pkg.Deneb == nil || pkg.Deneb.Plugin == nil {
		return nil, fmt.Errorf("no deneb.plugin section in manifest")
	}

	manifest := pkg.Deneb.Plugin
	if manifest.ID == "" {
		// Use package name as fallback ID.
		manifest.ID = strings.TrimPrefix(pkg.Name, "@deneb/")
		manifest.ID = strings.TrimPrefix(manifest.ID, "deneb-plugin-")
	}
	if manifest.Name == "" {
		manifest.Name = pkg.Name
	}
	if manifest.Version == "" {
		manifest.Version = pkg.Version
	}

	return manifest, nil
}

// ManifestRegistry caches and provides lookup for plugin manifests.
type ManifestRegistry struct {
	mu        sync.RWMutex
	manifests map[string]*PluginManifest
}

// NewManifestRegistry creates a new manifest registry.
func NewManifestRegistry() *ManifestRegistry {
	return &ManifestRegistry{
		manifests: make(map[string]*PluginManifest),
	}
}

// Register adds a manifest to the registry.
func (r *ManifestRegistry) Register(m PluginManifest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.manifests[m.ID] = &m
}

// Get returns a manifest by ID.
func (r *ManifestRegistry) Get(id string) *PluginManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.manifests[id]
}

// ListByKind returns all manifests of a specific kind.
func (r *ManifestRegistry) ListByKind(kind PluginKind) []PluginManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []PluginManifest
	for _, m := range r.manifests {
		if m.Kind == kind {
			result = append(result, *m)
		}
	}
	return result
}

// Count returns the total number of registered manifests.
func (r *ManifestRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.manifests)
}
