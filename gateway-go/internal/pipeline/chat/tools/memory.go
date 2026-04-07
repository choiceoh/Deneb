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
// across searches within the same agent turn.
var memContentCache = &memoryContentCache{
	entries: make(map[string]*memContentEntry),
}

type memContentEntry struct {
	content    string
	lines      []string // pre-split by "\n"
	lowerLines []string // pre-lowercased variant of lines
	mtime      time.Time
}

type memoryContentCache struct {
	mu      sync.Mutex
	entries map[string]*memContentEntry
}

// ReadMemoryFile returns file content, using a mtime-based cache to skip
// redundant reads. Falls back to os.ReadFile on cache miss or mtime change.
func ReadMemoryFile(path string) (string, error) {
	entry, err := readMemoryFileParsed(path)
	if err != nil {
		return "", err
	}
	return entry.content, nil
}

// readMemoryFileParsed returns the full cached entry including pre-split lines
// and their lowercase variants. Avoids repeated strings.Split + strings.ToLower
// in SearchMemoryFiles when the same file is searched multiple times per turn.
func readMemoryFileParsed(path string) (*memContentEntry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	mtime := info.ModTime()

	memContentCache.mu.Lock()
	if entry, ok := memContentCache.entries[path]; ok && entry.mtime.Equal(mtime) {
		e := entry
		memContentCache.mu.Unlock()
		return e, nil
	}
	memContentCache.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	lowerLines := make([]string, len(lines))
	for i, l := range lines {
		lowerLines[i] = strings.ToLower(l)
	}

	entry := &memContentEntry{
		content:    string(data),
		lines:      lines,
		lowerLines: lowerLines,
		mtime:      mtime,
	}

	memContentCache.mu.Lock()
	memContentCache.entries[path] = entry
	memContentCache.mu.Unlock()

	return entry, nil
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

// MemoryMatch represents a single keyword match in a memory file.
type MemoryMatch struct {
	File    string // relative path from workspace
	Line    int    // 1-based line number
	Snippet string // matched line with ±2 lines context
}

// SearchMemoryFiles searches memory files for keyword matches and returns results.
// Shared by ToolMemorySearch (LLM tool) and PrefetchKnowledge (context assembly).
func SearchMemoryFiles(workspaceDir, query string, limit int) []MemoryMatch {
	memoryFiles := CollectMemoryFiles(workspaceDir)
	if len(memoryFiles) == 0 {
		return nil // nil signals "no memory files exist"
	}

	keywords := strings.Fields(strings.ToLower(query))
	if len(keywords) == 0 {
		return []MemoryMatch{} // empty slice signals "files exist, no matches"
	}

	var matches []MemoryMatch
	for _, path := range memoryFiles {
		entry, err := readMemoryFileParsed(path)
		if err != nil {
			continue
		}
		lines := entry.lines
		lowerLines := entry.lowerLines
		rel, _ := filepath.Rel(workspaceDir, path)
		if rel == "" {
			rel = path
		}

		matchedLines := make(map[int]bool)
		for i, lower := range lowerLines {
			if matchedLines[i] {
				continue
			}
			for _, kw := range keywords {
				if !strings.Contains(lower, kw) {
					continue
				}
				start := i - 2
				if start < 0 {
					start = 0
				}
				end := i + 3
				if end > len(lines) {
					end = len(lines)
				}
				for j := start; j < end; j++ {
					matchedLines[j] = true
				}
				snippet := strings.Join(lines[start:end], "\n")
				matches = append(matches, MemoryMatch{
					File:    rel,
					Line:    i + 1,
					Snippet: snippet,
				})
				break
			}
		}
	}

	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	if matches == nil {
		return []MemoryMatch{} // files exist but no keyword matches
	}
	return matches
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
