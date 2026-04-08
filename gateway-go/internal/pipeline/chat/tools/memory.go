package tools

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Extended to 5m for single-user DGX Spark: memory files rarely change,
// matching ctxCacheRevalidateInterval to reduce redundant os.ReadDir calls.
const memoryFileCacheTTL = 5 * time.Minute

// memFileCache caches the result of collectMemoryFiles to avoid repeated
// os.Stat + os.ReadDir calls within a short window. TTL-based invalidation
// follows the plugin discovery cache pattern.
var memFileCache = &memoryFileListCache{}

// memContentCache caches file contents keyed by absolute path with mtime-based
// invalidation. Avoids repeated os.ReadFile calls for the same memory files
// within the same agent turn.
var memContentCache = &memoryContentCache{
	entries: make(map[string]*memContentEntry),
}

type memContentEntry struct {
	content string
	mtime   time.Time
}

type memoryContentCache struct {
	mu      sync.Mutex
	entries map[string]*memContentEntry
}

// ReadMemoryFile returns file content, using a mtime-based cache to skip
// redundant reads. Falls back to os.ReadFile on cache miss or mtime change.
func ReadMemoryFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	mtime := info.ModTime()

	memContentCache.mu.Lock()
	if entry, ok := memContentCache.entries[path]; ok && entry.mtime.Equal(mtime) {
		content := entry.content
		memContentCache.mu.Unlock()
		return content, nil
	}
	memContentCache.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)

	memContentCache.mu.Lock()
	memContentCache.entries[path] = &memContentEntry{content: content, mtime: mtime}
	memContentCache.mu.Unlock()

	return content, nil
}

type memoryFileListCache struct {
	mu        sync.Mutex
	workspace string
	files     []string
	expiresAt time.Time
}

func (c *memoryFileListCache) get(workspace string) ([]string, bool) {
	if c.workspace != workspace {
		return nil, false
	}
	if time.Now().After(c.expiresAt) {
		return nil, false
	}
	return c.files, true
}

func (c *memoryFileListCache) set(workspace string, files []string) {
	c.workspace = workspace
	c.files = files
	c.expiresAt = time.Now().Add(memoryFileCacheTTL)
}

// CollectMemoryFiles finds MEMORY.md and memory/*.md in the workspace.
// Results are cached with a short TTL to avoid repeated directory scans
// within the same agent turn.
func CollectMemoryFiles(workspaceDir string) []string {
	memFileCache.mu.Lock()
	defer memFileCache.mu.Unlock()

	if cached, ok := memFileCache.get(workspaceDir); ok {
		return cached
	}

	files := scanMemoryFiles(workspaceDir)
	memFileCache.set(workspaceDir, files)
	return files
}

// scanMemoryFiles performs the actual filesystem scan for memory files.
func scanMemoryFiles(workspaceDir string) []string {
	var files []string

	// Check MEMORY.md at workspace root.
	memoryMd := filepath.Join(workspaceDir, "MEMORY.md")
	if _, err := os.Stat(memoryMd); err == nil {
		files = append(files, memoryMd)
	}

	// Check memory.md (lowercase variant).
	memoryMdLower := filepath.Join(workspaceDir, "memory.md")
	if _, err := os.Stat(memoryMdLower); err == nil {
		files = append(files, memoryMdLower)
	}

	// Check memory/*.md directory.
	memoryDir := filepath.Join(workspaceDir, "memory")
	entries, err := os.ReadDir(memoryDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
				files = append(files, filepath.Join(memoryDir, e.Name()))
			}
		}
	}

	return files
}
