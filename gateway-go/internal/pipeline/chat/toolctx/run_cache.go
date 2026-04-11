package toolctx

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
	v, ok := rc.entries[key]
	return v, ok
}

// Set stores a tool output under the given key.
func (rc *RunCache) Set(key, output string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.entries[key] = output
}

// SetWithScope stores a tool output and associates it with a path scope.
// When a mutation affects a specific file, only entries whose scope overlaps
// that file's directory are invalidated instead of the entire cache.
func (rc *RunCache) SetWithScope(key, output, scope string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.entries[key] = output
	if scope != "" {
		rc.scopes[key] = filepath.Clean(scope)
	}
}

// Invalidate clears all cached entries. Called when a mutation tool executes
// without a known file path (e.g., git operations).
func (rc *RunCache) Invalidate() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
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

var cacheableTools = map[string]struct{}{
	"tree":    {},
	"grep":    {},
	"analyze": {},
}

var mutationTools = map[string]struct{}{
	"write":      {},
	"edit":       {},
	"multi_edit": {},
	"git":        {},
}

// IsCacheableTool returns true if the named tool's results can be cached.
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
func BuildCacheKey(name string, input json.RawMessage) string {
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
