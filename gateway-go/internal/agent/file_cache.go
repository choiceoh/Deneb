// Package agent — FileCache provides a session-scoped LRU cache for file reads.
//
// When the same file is read multiple times within a session and its mtime/size
// are unchanged, the cache returns a compact "already read" message instead of
// the full content, saving context tokens.
package agent

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Default limits for FileCache.
const (
	DefaultFileCacheMaxItems = 100
	DefaultFileCacheMaxSize  = 1 << 20 // 1 MB per entry
)

// FileCacheEntry tracks a cached file read result.
type FileCacheEntry struct {
	Path      string
	MTime     time.Time
	Size      int64
	Content   string
	ReadAt    time.Time
	ReadCount int
	SpillID   string // set when the first read was spillovered
}

// FileCache is a session-scoped LRU cache for file read results.
// Thread-safe for concurrent tool execution.
type FileCache struct {
	mu           sync.RWMutex
	entries      map[string]*FileCacheEntry // key: absolute path
	order        []string                   // LRU order: front=oldest, back=newest
	maxItems     int
	maxEntrySize int64
}

// NewFileCache creates a cache with the given max item count.
func NewFileCache(maxItems int) *FileCache {
	if maxItems <= 0 {
		maxItems = DefaultFileCacheMaxItems
	}
	return &FileCache{
		entries:      make(map[string]*FileCacheEntry),
		maxItems:     maxItems,
		maxEntrySize: DefaultFileCacheMaxSize,
	}
}

// MaxEntrySize returns the maximum content size that will be cached.
func (c *FileCache) MaxEntrySize() int64 {
	return c.maxEntrySize
}

// Get returns the cached entry for path, or nil if not cached.
// Moves the entry to the back of the LRU order on hit.
func (c *FileCache) Get(path string) *FileCacheEntry {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[path]
	if !ok {
		return nil
	}

	// Move to back (most recently used).
	c.moveToBack(path)
	return entry
}

// Set adds or replaces a cache entry. Evicts the oldest entry if at capacity.
func (c *FileCache) Set(path string, entry *FileCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.entries[path]; exists {
		c.entries[path] = entry
		c.moveToBack(path)
		return
	}

	// Evict oldest if at capacity.
	for len(c.entries) >= c.maxItems && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}

	c.entries[path] = entry
	c.order = append(c.order, path)
}

// Invalidate removes a single entry by path.
func (c *FileCache) Invalidate(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.entries[path]; !ok {
		return
	}
	delete(c.entries, path)
	c.removeFromOrder(path)
}

// InvalidateAll clears the entire cache.
func (c *FileCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*FileCacheEntry)
	c.order = c.order[:0]
}

// FileChanged checks if the file on disk differs from the cached entry.
// Returns true if the file has been modified (mtime or size changed),
// or if the file cannot be stat'd (treat as changed for safety).
func FileChanged(path string, cached *FileCacheEntry) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true // can't stat → treat as changed
	}
	return !info.ModTime().Equal(cached.MTime) || info.Size() != cached.Size
}

// FormatCachedRead builds the compact message returned on cache hit.
func FormatCachedRead(displayPath string, entry *FileCacheEntry) string {
	return fmt.Sprintf(
		"[FileCache: HIT | read | %s | mtime=%s | read %d times]\n"+
			"(Content unchanged since first read. Already in context.)\n"+
			"To re-read full content: read(\"%s\", force=true)",
		displayPath,
		entry.MTime.Format(time.RFC3339),
		entry.ReadCount,
		displayPath,
	)
}

// --- internal helpers ---

func (c *FileCache) moveToBack(path string) {
	c.removeFromOrder(path)
	c.order = append(c.order, path)
}

func (c *FileCache) removeFromOrder(path string) {
	for i, p := range c.order {
		if p == path {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}
