package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// Type aliases — canonical definitions are in toolport/.

// runCache is a thread-safe, run-scoped cache for idempotent tool results.
type runCache = toolport.RunCache

// newRunCache creates an empty run cache.
func newRunCache() *runCache { return toolport.NewRunCache() }

// isCacheableTool returns true if the named tool's results can be cached across calls.
func isCacheableTool(name string) bool { return toolport.IsCacheableTool(name) }

// isMutationTool returns true if the named tool can modify files, triggering cache invalidation.
func isMutationTool(name string) bool { return toolport.IsMutationTool(name) }

// buildCacheKey creates a canonical cache key from the tool name and its JSON input.
func buildCacheKey(name string, input rawJSON) string {
	return toolport.BuildCacheKey(name, input)
}
