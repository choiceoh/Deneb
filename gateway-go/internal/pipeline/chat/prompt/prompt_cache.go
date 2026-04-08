package prompt

import (
	"os"
	"runtime"
	"sync"
	"time"
)

// PromptCache consolidates all prompt-related caches into a single manager.
// Replaces the previous package-level singletons (staticPromptMu/Key/Cached,
// ctxCache, sessionSnapshots, cachedTimezone, cachedHostname).
//
// Package-level var `Cache` is the singleton; all prompt functions use it.
type PromptCache struct {
	// --- Static prompt block (keyed on sorted tool name list) ---
	staticMu     sync.RWMutex
	staticKey    string
	staticCached string

	// --- Context file cache (mtime-based with TTL) ---
	ctxMu        sync.Mutex
	ctxWorkspace string
	ctxFiles     []ContextFile
	ctxResolved  map[string]time.Time // resolved path → mtime
	ctxCachedAt  time.Time

	// --- Session snapshots (frozen context files per session) ---
	sessMu    sync.Mutex
	sessStore map[string][]ContextFile

	// --- One-time values (resolved at startup) ---
	timezoneOnce sync.Once
	timezone     string
	timezoneLoc  *time.Location

	hostnameOnce sync.Once
	hostname     string
}

// Cache is the package-level singleton used by all prompt functions.
var Cache = &PromptCache{}

// --- Static prompt cache ---

// StaticPrompt returns the cached static prompt if the key matches.
func (c *PromptCache) StaticPrompt(key string) (string, bool) {
	c.staticMu.RLock()
	defer c.staticMu.RUnlock()
	if c.staticKey == key {
		return c.staticCached, true
	}
	return "", false
}

// SetStaticPrompt stores the assembled static prompt block.
func (c *PromptCache) SetStaticPrompt(key, text string) {
	c.staticMu.Lock()
	c.staticKey = key
	c.staticCached = text
	c.staticMu.Unlock()
}

// --- Context file cache ---

// ContextFiles returns cached context files if valid for the given workspace.
func (c *PromptCache) ContextFiles(workspace string) ([]ContextFile, bool) {
	// Caller must hold ctxMu — this is called inside LoadContextFiles which
	// already locks. No additional lock here; the public API is LoadContextFiles.
	if c.ctxWorkspace != workspace || len(c.ctxResolved) == 0 {
		return nil, false
	}
	if time.Since(c.ctxCachedAt) > ctxCacheRevalidateInterval {
		return nil, false
	}
	for path, cachedMtime := range c.ctxResolved {
		info, err := os.Stat(path)
		if err != nil {
			return nil, false
		}
		if !info.ModTime().Equal(cachedMtime) {
			return nil, false
		}
	}
	return c.ctxFiles, true
}

// SetContextFiles stores context files for the given workspace.
func (c *PromptCache) SetContextFiles(workspace string, files []ContextFile, resolved map[string]time.Time) {
	c.ctxWorkspace = workspace
	c.ctxFiles = files
	c.ctxResolved = resolved
	c.ctxCachedAt = time.Now()
}

// LockCtx acquires the context file mutex. Must be paired with UnlockCtx.
func (c *PromptCache) LockCtx() { c.ctxMu.Lock() }

// UnlockCtx releases the context file mutex.
func (c *PromptCache) UnlockCtx() { c.ctxMu.Unlock() }

// --- Session snapshots ---

// SessionSnapshot returns frozen context files for a session.
func (c *PromptCache) SessionSnapshot(key string) ([]ContextFile, bool) {
	c.sessMu.Lock()
	defer c.sessMu.Unlock()
	if c.sessStore == nil {
		return nil, false
	}
	files, ok := c.sessStore[key]
	return files, ok
}

// SetSessionSnapshot stores frozen context files for a session.
func (c *PromptCache) SetSessionSnapshot(key string, files []ContextFile) {
	c.sessMu.Lock()
	defer c.sessMu.Unlock()
	if c.sessStore == nil {
		c.sessStore = make(map[string][]ContextFile)
	}
	c.sessStore[key] = files
}

// ClearSession removes the frozen context files for a session.
func (c *PromptCache) ClearSession(key string) {
	c.sessMu.Lock()
	defer c.sessMu.Unlock()
	delete(c.sessStore, key)
}

// --- One-time values ---

// Timezone returns the resolved timezone string and location.
func (c *PromptCache) Timezone() (string, *time.Location) {
	c.timezoneOnce.Do(func() {
		c.timezone = resolveTimezone()
		loc, err := time.LoadLocation(c.timezone)
		if err == nil {
			c.timezoneLoc = loc
		}
	})
	return c.timezone, c.timezoneLoc
}

// Hostname returns the cached hostname.
func (c *PromptCache) Hostname() string {
	c.hostnameOnce.Do(func() {
		c.hostname, _ = os.Hostname()
	})
	return c.hostname
}

// BuildRuntimeInfo creates RuntimeInfo from the current environment.
func (c *PromptCache) BuildRuntimeInfo(model, defaultModel string) *RuntimeInfo {
	return &RuntimeInfo{
		Host:         c.Hostname(),
		OS:           "linux",
		Arch:         runtime.GOARCH,
		Model:        model,
		DefaultModel: defaultModel,
	}
}

// --- Reset (testing only) ---

// Reset clears all caches. Intended for tests to avoid cross-test state leakage.
func (c *PromptCache) Reset() {
	c.staticMu.Lock()
	c.staticKey = ""
	c.staticCached = ""
	c.staticMu.Unlock()

	c.ctxMu.Lock()
	c.ctxWorkspace = ""
	c.ctxFiles = nil
	c.ctxResolved = nil
	c.ctxCachedAt = time.Time{}
	c.ctxMu.Unlock()

	c.sessMu.Lock()
	c.sessStore = nil
	c.sessMu.Unlock()
}
