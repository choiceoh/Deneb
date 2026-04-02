// Package agent — FileCache provides a session-scoped LRU cache for file reads.
//
// When the same file is read multiple times within a session and its mtime/size
// are unchanged, the cache returns the cached content from memory, avoiding
// redundant disk I/O.
package agent

import (
	"fmt"
	"hash/fnv"
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
	Path        string
	MTime       time.Time
	Size        int64
	Content     string
	ContentHash uint64 // FNV-1a hash of raw file bytes (for staleness detection)
	ReadAt      time.Time
	ReadCount   int
	SpillID     string // set when the first read was spillovered
}

// ContentHashOf computes an FNV-1a 64-bit hash for staleness comparison.
func ContentHashOf(data []byte) uint64 {
	h := fnv.New64a()
	h.Write(data)
	return h.Sum64()
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

// FormatCachedRead returns the cached file content on cache hit.
// We always return the full content because the LLM's context window may have
// been compressed since the first read, making the original content invisible
// to the agent. Returning from memory cache still avoids disk I/O.
func FormatCachedRead(displayPath string, entry *FileCacheEntry) string {
	return entry.Content
}

// CheckStaleness verifies that the file has not been modified since the last
// cached read. It mirrors the Edit-tool staleness pattern:
//
//  1. Compare mtime — if unchanged, file is fresh (fast path).
//  2. If mtime differs, compare content hash (handles cloud-sync false positives
//     where mtime changes but content is identical).
//  3. If both differ, the file is stale.
//
// Returns nil when the file is fresh or was never cached (first write is always
// allowed). Returns a descriptive error when the file is stale.
func (c *FileCache) CheckStaleness(path string) error {
	c.mu.RLock()
	entry, ok := c.entries[path]
	c.mu.RUnlock()
	if !ok {
		return nil // never read → no staleness to detect
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil // can't stat → allow the write (will fail later if path is bad)
	}

	// Fast path: mtime unchanged.
	if info.ModTime().Equal(entry.MTime) && info.Size() == entry.Size {
		return nil
	}

	// Mtime changed — compare content hash to rule out cloud-sync false positive.
	if entry.ContentHash != 0 {
		data, err := os.ReadFile(path)
		if err == nil && ContentHashOf(data) == entry.ContentHash {
			return nil // content identical despite mtime change
		}
	}

	return fmt.Errorf(
		"file has been modified since last read (cached mtime %s, current mtime %s). "+
			"Re-read the file before editing",
		entry.MTime.Format(time.RFC3339), info.ModTime().Format(time.RFC3339),
	)
}

// UpdateAfterWrite refreshes the cache entry for path after a successful write,
// so subsequent staleness checks use the new state.
func (c *FileCache) UpdateAfterWrite(path string) {
	info, err := os.Stat(path)
	if err != nil {
		c.Invalidate(path)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		c.Invalidate(path)
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[path]; ok {
		entry.MTime = info.ModTime()
		entry.Size = info.Size()
		entry.ContentHash = ContentHashOf(data)
	}
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
