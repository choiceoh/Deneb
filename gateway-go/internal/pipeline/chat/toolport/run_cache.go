package toolport

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
)

// RunCache is a thread-safe, run-scoped cache for idempotent tool results.
type RunCache struct {
	mu      sync.RWMutex
	entries map[string]string
	scopes  map[string]string // cacheKey → path scope for selective invalidation
	// disabled is the async-writer latch: once an actor that can mutate the
	// workspace at an unpredictable future point exists in this run (a
	// background exec, a spawned sub-agent with write tools, a tracked
	// process), point-in-time invalidation can no longer bracket the writes,
	// so cached reads stay untrustworthy for the rest of the run. Sticky.
	disabled bool
}

// NewRunCache creates an empty run cache.
func NewRunCache() *RunCache {
	return &RunCache{
		entries: make(map[string]string),
		scopes:  make(map[string]string),
	}
}

// Get returns the cached output for the given key, if present.
func (rc *RunCache) Get(key string) (string, bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	if rc.disabled {
		return "", false
	}
	v, ok := rc.entries[key]
	return v, ok
}

// Set stores a tool output under the given key.
func (rc *RunCache) Set(key, output string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.disabled {
		return
	}
	rc.entries[key] = output
}

// SetWithScope stores a tool output and associates it with a path scope.
// When a mutation affects a specific file, only entries whose scope overlaps
// that file's directory are invalidated instead of the entire cache.
func (rc *RunCache) SetWithScope(key, output, scope string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.disabled {
		return
	}
	rc.entries[key] = output
	if scope != "" {
		rc.scopes[key] = filepath.Clean(scope)
	}
}

// Disable drops every entry and turns the cache off for the rest of the run:
// Get always misses, Set/SetWithScope become no-ops. Called when an async
// writer appears (see the disabled field doc) — unlike Invalidate, which
// brackets a synchronous mutation that has already fully landed.
func (rc *RunCache) Disable() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.disabled = true
	rc.entries = make(map[string]string)
	rc.scopes = make(map[string]string)
}

// Disabled reports whether the async-writer latch has fired.
func (rc *RunCache) Disabled() bool {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.disabled
}

// Invalidate clears all cached entries. Called when a mutation tool executes
// without a known file path (e.g., git operations).
func (rc *RunCache) Invalidate() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if len(rc.entries) == 0 && len(rc.scopes) == 0 {
		return // already empty — skip the reallocations
	}
	rc.entries = make(map[string]string)
	rc.scopes = make(map[string]string)
}

// InvalidateByPath removes cached entries whose scope overlaps with path.
// Entries without a recorded scope are conservatively removed.
func (rc *RunCache) InvalidateByPath(path string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	dir := filepath.Dir(filepath.Clean(path))
	for key := range rc.entries {
		scope, ok := rc.scopes[key]
		if !ok {
			// No scope recorded — conservatively invalidate.
			delete(rc.entries, key)
			continue
		}
		if scopeOverlaps(dir, scope) {
			delete(rc.entries, key)
			delete(rc.scopes, key)
		}
	}
}

// scopeOverlaps reports whether a file in dir could affect cached results
// scoped to scope. Returns true when the file is inside the scope's subtree.
func scopeOverlaps(dir, scope string) bool {
	if scope == "." || scope == "" {
		return true // workspace-wide search — always affected
	}
	// Mixed spellings — one side absolute, the other workspace-relative —
	// cannot be compared reliably: recorder (search tool input) and
	// invalidator (mutation tool input) pass the model's raw paths and never
	// normalize through a shared root. Fail toward invalidation so a stale
	// search result is never served on a spelling technicality.
	if filepath.IsAbs(dir) != filepath.IsAbs(scope) {
		return true
	}
	if dir == scope {
		return true
	}
	return strings.HasPrefix(dir+"/", scope+"/")
}

// Len returns the number of cached entries (used in tests).
func (rc *RunCache) Len() int {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return len(rc.entries)
}

// cacheableTools are tools whose identical repeat calls within one run may be
// served from the RunCache. Only pure, stateless workspace reads belong here:
// the cache Get short-circuits BEFORE the tool fn runs, so a tool with any
// internal repeat handling of its own is unsafe to cache — fetch_tools was
// briefly added (its repeats measured at 20% of calls) and reverted because
// its already-active branch returns a compact response on repeats, which a
// cache hit would replace with the first call's full schema payload.
var cacheableTools = map[string]struct{}{
	"grep": {},
}

var mutationTools = map[string]struct{}{
	"write": {},
	"edit":  {},
}

// IsCacheableTool returns true if the named tool's results can be cached.
// Only add tools whose fn has no repeat-call handling of its own (see
// cacheableTools).
func IsCacheableTool(name string) bool {
	_, ok := cacheableTools[name]
	return ok
}

// IsMutationTool returns true if the named tool can modify files.
func IsMutationTool(name string) bool {
	_, ok := mutationTools[name]
	return ok
}

// BuildCacheKey creates a canonical cache key from tool name and input JSON.
// Non-semantic fields (compress, $ref) are stripped before key generation.
func BuildCacheKey(name string, input rawJSON) string {
	if !bytes.Contains(input, []byte(`"compress"`)) && !bytes.Contains(input, []byte(`"$ref"`)) {
		return name + ":" + string(input)
	}
	var m map[string]any
	if json.Unmarshal(input, &m) != nil {
		return name + ":" + string(input)
	}
	delete(m, "compress")
	delete(m, "$ref")
	canonical, err := json.Marshal(m)
	if err != nil {
		return name + ":" + string(input)
	}
	return name + ":" + string(canonical)
}
