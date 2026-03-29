// tool_run_cache.go — Run-scoped cache for idempotent read tools (find, tree).
//
// Created once per agent run in executeAgentRun and injected into context via
// OnTurnInit. Survives across turns within the same run. Automatically
// invalidated when mutation tools (write, edit, exec, etc.) execute, ensuring
// cached results never become stale after file system changes.
package chat

import (
	"encoding/json"
	"sync"
)

// RunCache is a thread-safe, run-scoped cache for idempotent tool results.
// Tools executing in parallel within a turn share the same RunCache instance.
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

// cacheableTools are read-only tools whose results can be safely cached
// within an agent run. Results are invalidated when mutation tools execute.
var cacheableTools = map[string]bool{
	"find": true,
	"tree": true,
	"grep": true,
}

// mutationTools are tools that deterministically modify the file system,
// requiring cache invalidation to prevent stale results.
// exec is excluded: while it *can* mutate files, the majority of exec calls
// are read-only (cat, ls, curl, etc.) and invalidating the entire cache on
// every exec call destroys find/tree/grep cache hit rates. True file-mutating
// commands should use write/edit/apply_patch instead.
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
// Go's json.Marshal sorts map keys alphabetically, providing canonical ordering.
func BuildCacheKey(name string, input json.RawMessage) string {
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
