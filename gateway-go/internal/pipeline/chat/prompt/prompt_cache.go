package prompt

import (
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

// PromptCache consolidates all prompt-related caches into a single manager.
// Replaces the previous package-level singletons (staticPromptMu/Key/Cached,
// ctxCache, sessionSnapshots, cachedTimezone, cachedHostname).
//
// Package-level var `Cache` is the singleton; all prompt functions use it.
//
// Lock hierarchy (acquire in this order; never reverse):
//
//	ctxMu  →  sessMu
//	staticMu  (independent — never held together with ctxMu or sessMu)
//
// ctxMu → sessMu: LoadContextFiles (context_files.go) holds ctxMu via
// LockCtx/UnlockCtx and calls SetSessionSnapshot (sessMu) to freeze the
// loaded files for the session. sessMu sections are leaf-level map accesses
// that never acquire ctxMu. Reset takes each lock strictly sequentially.
// ContextFiles/SetContextFiles assume the caller already holds ctxMu (see
// LockCtx/UnlockCtx); they must not be called with sessMu held.
type PromptCache struct {
	// --- Static prompt block (keyed on tool set + profile/topic/persona hashes) ---
	// A bounded map, not a single slot: the 업무/코딩 profiles (and distinct
	// topics/personas) build different static blocks under different keys, and a
	// single slot made alternating sessions evict each other's entry every turn,
	// rebuilding kilobytes of prompt strings. Key cardinality is small (a handful
	// of profiles × rare content-hash changes); staticCacheMaxEntries is a leak
	// backstop, not a working-set tuning knob.
	staticMu    sync.RWMutex
	staticCache map[string]string

	// --- Context file cache (mtime-based with TTL) ---
	ctxMu        sync.Mutex
	ctxWorkspace string
	ctxFiles     []ContextFile
	ctxResolved  map[string]time.Time // resolved path → mtime
	ctxCachedAt  time.Time

	// --- Session snapshots (frozen context files per session) ---
	sessMu         sync.Mutex
	sessStore      map[string][]ContextFile
	sessTopicStore map[string]TopicKnowledge // frozen per-topic knowledge per session

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

// staticCacheMaxEntries bounds the static prompt cache. Keys are profile
// fingerprints (tool set + topic/persona content hashes), so steady-state
// cardinality is single-digit; the cap only guards
// against pathological churn (e.g. a bug minting a fresh hash per turn).
// Eviction is drop-all: entries are cheap to rebuild and LRU bookkeeping is
// not worth it at this size.
const staticCacheMaxEntries = 32

// StaticPrompt returns the cached static prompt for the key.
func (c *PromptCache) StaticPrompt(key string) (string, bool) {
	c.staticMu.RLock()
	defer c.staticMu.RUnlock()
	text, ok := c.staticCache[key]
	return text, ok
}

// SetStaticPrompt stores the assembled static prompt block under its key.
func (c *PromptCache) SetStaticPrompt(key, text string) {
	c.staticMu.Lock()
	if c.staticCache == nil || len(c.staticCache) >= staticCacheMaxEntries {
		c.staticCache = make(map[string]string, 4)
	}
	c.staticCache[key] = text
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

// TopicSnapshot returns frozen per-topic knowledge for a session.
func (c *PromptCache) TopicSnapshot(key string) (TopicKnowledge, bool) {
	c.sessMu.Lock()
	defer c.sessMu.Unlock()
	if c.sessTopicStore == nil {
		return TopicKnowledge{}, false
	}
	tk, ok := c.sessTopicStore[key]
	return tk, ok
}

// SetTopicSnapshot stores frozen per-topic knowledge for a session.
func (c *PromptCache) SetTopicSnapshot(key string, tk TopicKnowledge) {
	c.sessMu.Lock()
	defer c.sessMu.Unlock()
	if c.sessTopicStore == nil {
		c.sessTopicStore = make(map[string]TopicKnowledge)
	}
	c.sessTopicStore[key] = tk
}

// ClearSession removes the frozen context files and topic knowledge for a
// session.
func (c *PromptCache) ClearSession(key string) {
	c.sessMu.Lock()
	defer c.sessMu.Unlock()
	delete(c.sessStore, key)
	delete(c.sessTopicStore, key)
}

// ClearAllContextSnapshots invalidates the shared context-file cache and every
// session's frozen AGENTS/SOUL/USER/MEMORY view. Fact-plane projections are
// shared across sessions, so clearing only the session that performed a
// correction would leave stale system-prompt facts frozen elsewhere.
// Topic snapshots are independent files and intentionally remain intact.
func (c *PromptCache) ClearAllContextSnapshots() {
	// Hold both locks in the documented ctxMu -> sessMu order so a context load
	// cannot observe the cleared shared cache while still returning an old
	// per-session snapshot from the gap between two independent lock sections.
	c.ctxMu.Lock()
	c.sessMu.Lock()
	c.ctxWorkspace = ""
	c.ctxFiles = nil
	c.ctxResolved = nil
	c.ctxCachedAt = time.Time{}
	c.sessStore = nil
	c.sessMu.Unlock()
	c.ctxMu.Unlock()
}

// ClearAllTopicSnapshots drops every session's frozen per-topic knowledge so the
// next turn re-reads <key>.md from disk. It backs miniapp.topicdocs.write_current
// with applyNow=true: topic background is shared across all session keys
// (client:main, its sub-conversations, cron, …), so clearing one session would
// not fully apply an edit — the whole topic store must be swept. Frozen context
// files are left intact (a topic edit does not touch CLAUDE.md/SOUL.md/etc.).
// The cost is a one-time Static-cache miss per active session on the next turn.
func (c *PromptCache) ClearAllTopicSnapshots() {
	c.sessMu.Lock()
	defer c.sessMu.Unlock()
	c.sessTopicStore = nil
}

// --- One-time values ---

// Timezone returns the resolved timezone string and location.
//
// Both come from pkg/dentime — the canonical Deneb clock that honors
// DENEB_TIMEZONE and the config "timezone" key — so the system prompt's
// date line agrees with logs, cron, and the calendar briefing. The display
// name and the location are read from the same source to keep them
// consistent (a zone abbreviation like "KST" is not a valid LoadLocation
// argument, which previously left the location nil and silently fell back
// to the server-local zone).
func (c *PromptCache) Timezone() (string, *time.Location) {
	c.timezoneOnce.Do(func() {
		c.timezone = resolveTimezone()
		c.timezoneLoc = dentime.Location() // never nil
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
	c.staticCache = nil
	c.staticMu.Unlock()

	c.ctxMu.Lock()
	c.ctxWorkspace = ""
	c.ctxFiles = nil
	c.ctxResolved = nil
	c.ctxCachedAt = time.Time{}
	c.ctxMu.Unlock()

	c.sessMu.Lock()
	c.sessStore = nil
	c.sessTopicStore = nil
	c.sessMu.Unlock()
}
