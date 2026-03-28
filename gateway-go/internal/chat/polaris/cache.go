// Package polaris implements the polaris agent tool: a searchable index of
// Deneb's documentation tree plus AI-curated system guides.
package polaris

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// polarisCacheTTL is the TTL for the doc tree index cache.
// Docs rarely change in a running gateway, so 60s is generous.
const polarisCacheTTL = 60 * time.Second

// polarisMaxReadChars caps the output of the read action to avoid context bloat.
const polarisMaxReadChars = 8000

// polarisMaxSearchResults caps keyword search results.
const polarisMaxSearchResults = 15

// --- Doc tree index cache ---

type docEntry struct {
	Path    string // relative to docs/, e.g. "concepts/session"
	Title   string // from frontmatter
	Summary string // from frontmatter
}

type docTreeCache struct {
	mu        sync.Mutex
	docsDir   string
	entries   []docEntry
	expiresAt time.Time
}

var polarisTreeCache = &docTreeCache{}

func (c *docTreeCache) get(docsDir string) ([]docEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.docsDir != docsDir {
		return nil, false
	}
	if time.Now().After(c.expiresAt) {
		return nil, false
	}
	return c.entries, true
}

func (c *docTreeCache) set(docsDir string, entries []docEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.docsDir = docsDir
	c.entries = entries
	c.expiresAt = time.Now().Add(polarisCacheTTL)
}

// --- Doc content cache (mtime-based, same pattern as tool_memory.go) ---

type polarisContentEntry struct {
	content string
	mtime   time.Time
}

var polarisContentCacheMu sync.Mutex
var polarisContentCacheMap = make(map[string]*polarisContentEntry)

func readDocFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	mtime := info.ModTime()

	polarisContentCacheMu.Lock()
	if entry, ok := polarisContentCacheMap[path]; ok && entry.mtime.Equal(mtime) {
		content := entry.content
		polarisContentCacheMu.Unlock()
		return content, nil
	}
	polarisContentCacheMu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)

	polarisContentCacheMu.Lock()
	polarisContentCacheMap[path] = &polarisContentEntry{content: content, mtime: mtime}
	polarisContentCacheMu.Unlock()

	return content, nil
}

// --- Frontmatter parsing ---

// parseFrontmatter extracts title and summary from YAML frontmatter.
// Returns (title, summary, bodyWithoutFrontmatter).
func parseFrontmatter(content string) (string, string, string) {
	if !strings.HasPrefix(content, "---\n") {
		return "", "", content
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return "", "", content
	}
	fm := content[4 : 4+end]
	body := content[4+end+4:] // skip past closing "---\n"

	var title, summary string
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "title:") {
			title = strings.Trim(strings.TrimPrefix(line, "title:"), " \"'")
		} else if strings.HasPrefix(line, "summary:") {
			summary = strings.Trim(strings.TrimPrefix(line, "summary:"), " \"'")
		}
	}
	return title, summary, body
}

// --- Doc tree scanning ---

func scanDocTree(docsDir string) []docEntry {
	var entries []docEntry

	_ = filepath.Walk(docsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}
		rel, _ := filepath.Rel(docsDir, path)
		if rel == "" {
			return nil
		}
		// Skip generated and asset files.
		if strings.HasPrefix(rel, ".generated/") || strings.HasPrefix(rel, "assets/") {
			return nil
		}

		content, readErr := readDocFile(path)
		if readErr != nil {
			return nil
		}

		title, summary, _ := parseFrontmatter(content)
		// Strip .md extension for topic path.
		topicPath := strings.TrimSuffix(rel, ".md")

		entries = append(entries, docEntry{
			Path:    topicPath,
			Title:   title,
			Summary: summary,
		})
		return nil
	})

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	return entries
}

func getDocTree(docsDir string) []docEntry {
	if cached, ok := polarisTreeCache.get(docsDir); ok {
		return cached
	}
	entries := scanDocTree(docsDir)
	polarisTreeCache.set(docsDir, entries)
	return entries
}

// --- Docs directory resolution ---

// resolveDocsDir finds the docs/ directory by checking multiple locations:
//  1. workspaceDir/docs (agent workspace)
//  2. Executable's parent directories (binary lives in repo under gateway-go/)
//  3. Current working directory ancestors (gateway often runs from repo root)
//
// Result is cached after first successful resolution.
var (
	resolvedDocsDir     string
	resolvedDocsDirOnce sync.Once
)

func resolveDocsDir(workspaceDir string) string {
	resolvedDocsDirOnce.Do(func() {
		// 1. Check workspace directory.
		candidate := filepath.Join(workspaceDir, "docs")
		if hasDocsContent(candidate) {
			resolvedDocsDir = candidate
			return
		}

		// 2. Walk up from executable path (e.g. /repo/gateway-go/deneb-gateway → /repo/docs).
		if exe, err := os.Executable(); err == nil {
			dir := filepath.Dir(exe)
			for i := 0; i < 5; i++ {
				candidate = filepath.Join(dir, "docs")
				if hasDocsContent(candidate) {
					resolvedDocsDir = candidate
					return
				}
				parent := filepath.Dir(dir)
				if parent == dir {
					break
				}
				dir = parent
			}
		}

		// 3. Walk up from cwd (gateway often started from repo root).
		if cwd, err := os.Getwd(); err == nil {
			dir := cwd
			for i := 0; i < 5; i++ {
				candidate = filepath.Join(dir, "docs")
				if hasDocsContent(candidate) {
					resolvedDocsDir = candidate
					return
				}
				parent := filepath.Dir(dir)
				if parent == dir {
					break
				}
				dir = parent
			}
		}

		// Fallback: use workspace/docs even if empty (preserves old behavior).
		resolvedDocsDir = filepath.Join(workspaceDir, "docs")
	})
	return resolvedDocsDir
}

// hasDocsContent checks that a directory exists and contains at least one .md file.
func hasDocsContent(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	// Quick check: look for any .md file in the top two levels.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			return true
		}
		if e.IsDir() {
			subPath := filepath.Join(dir, e.Name())
			subEntries, err := os.ReadDir(subPath)
			if err != nil {
				continue
			}
			for _, se := range subEntries {
				if !se.IsDir() && strings.HasSuffix(se.Name(), ".md") {
					return true
				}
			}
		}
	}
	return false
}

// --- Tool implementation ---

// NewHandler returns the polaris tool handler function for use with ToolRegistry.
func NewHandler(workspaceDir string) func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		docsDir := resolveDocsDir(workspaceDir)
		var p struct {
			Action string `json:"action"`
			Query  string `json:"query"`
			Topic  string `json:"topic"`
		}
		if err := jsonutil.UnmarshalInto("polaris params", input, &p); err != nil {
			return "", err
		}

		switch p.Action {
		case "topics":
			return polarisTopics(docsDir, p.Topic)
		case "search":
			return polarisSearch(docsDir, p.Query)
		case "read":
			return polarisRead(docsDir, p.Topic)
		case "guides":
			return polarisGuides(p.Topic)
		default:
			return "", fmt.Errorf("unknown action %q (valid: topics, search, read, guides)", p.Action)
		}
	}
}
