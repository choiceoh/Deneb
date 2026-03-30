package toolctx

import (
	"bytes"
	"encoding/json"
	"sync"
)

// RunCache is a thread-safe, run-scoped cache for idempotent tool results.
type RunCache struct {
	mu      sync.RWMutex
	entries map[string]string
}

// NewRunCache creates an empty run cache.
func NewRunCache() *RunCache {
	return &RunCache{entries: make(map[string]string)}
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

// Invalidate clears all cached entries. Called when a mutation tool executes.
func (rc *RunCache) Invalidate() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.entries = make(map[string]string)
}

// Len returns the number of cached entries (used in tests).
func (rc *RunCache) Len() int {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return len(rc.entries)
}

var cacheableTools = map[string]bool{
	"find": true,
	"tree": true,
	"grep": true,
}

var mutationTools = map[string]bool{
	"write":       true,
	"edit":        true,
	"multi_edit":  true,
	"apply_patch": true,
	"git":         true,
}

// IsCacheableTool returns true if the named tool's results can be cached.
func IsCacheableTool(name string) bool {
	return cacheableTools[name]
}

// IsMutationTool returns true if the named tool can modify files.
func IsMutationTool(name string) bool {
	return mutationTools[name]
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
