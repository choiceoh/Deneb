package provider

import (
	"log/slog"
	"os"
	"sync"
)

// ModelMeta holds cached metadata about a model.
type ModelMeta struct {
	ID            string `json:"id"`
	Provider      string `json:"provider"`
	ContextTokens int    `json:"contextTokens,omitempty"`
	MaxOutput     int    `json:"maxOutput,omitempty"`
}

// CatalogCache caches model catalog data, invalidating when the underlying
// config or auth files change. This avoids expensive provider discovery
// on every request.
type CatalogCache struct {
	mu          sync.RWMutex
	models      map[string]ModelMeta
	configPath  string
	authPath    string
	configMtime int64
	configSize  int64
	authMtime   int64
	authSize    int64
	forceStale  bool
	logger      *slog.Logger
}

// NewCatalogCache creates a new model catalog cache.
func NewCatalogCache(configPath, authPath string, logger *slog.Logger) *CatalogCache {
	return &CatalogCache{
		models:     make(map[string]ModelMeta),
		configPath: configPath,
		authPath:   authPath,
		logger:     logger,
	}
}

// IsStale returns true if the underlying files have changed since the last
// cache population.
func (cc *CatalogCache) IsStale() bool {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	if cc.forceStale || cc.models == nil || len(cc.models) == 0 {
		return true
	}
	return cc.fileChanged(cc.configPath, cc.configMtime, cc.configSize) ||
		cc.fileChanged(cc.authPath, cc.authMtime, cc.authSize)
}

func (cc *CatalogCache) fileChanged(path string, cachedMtime, cachedSize int64) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return info.ModTime().UnixNano() != cachedMtime || info.Size() != cachedSize
}

// Get returns the cached model metadata for the given model ID.
// Returns nil if not cached.
func (cc *CatalogCache) Get(modelID string) *ModelMeta {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	if m, ok := cc.models[modelID]; ok {
		return &m
	}
	return nil
}

// Update replaces the cached catalog with the given models and updates
// file state tracking.
func (cc *CatalogCache) Update(models map[string]ModelMeta) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.forceStale = false
	cc.models = make(map[string]ModelMeta, len(models))
	for k, v := range models {
		cc.models[k] = v
	}

	// Snapshot file states.
	cc.configMtime, cc.configSize = fileStat(cc.configPath)
	cc.authMtime, cc.authSize = fileStat(cc.authPath)

	if cc.logger != nil {
		cc.logger.Info("model catalog cache updated", "count", len(models))
	}
}

// Count returns the number of cached models.
func (cc *CatalogCache) Count() int {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return len(cc.models)
}

// ForceRefresh marks the cache as stale by zeroing mtime.
func (cc *CatalogCache) ForceRefresh() {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.forceStale = true
}

func fileStat(path string) (mtime, size int64) {
	if path == "" {
		return 0, 0
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0
	}
	return info.ModTime().UnixNano(), info.Size()
}
