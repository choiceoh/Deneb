package chat

import (
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// Type aliases — canonical definitions are in toolport/.

// RunCache is a thread-safe, run-scoped cache for idempotent tool results.
type RunCache = toolport.RunCache

// NewRunCache creates an empty run cache.
func NewRunCache() *RunCache { return toolport.NewRunCache() }

// IsCacheableTool returns true if the named tool's results can be cached across calls.
func IsCacheableTool(name string) bool { return toolport.IsCacheableTool(name) }

// IsMutationTool returns true if the named tool can modify files, triggering cache invalidation.
func IsMutationTool(name string) bool { return toolport.IsMutationTool(name) }

// BuildCacheKey creates a canonical cache key from the tool name and its JSON input.
func BuildCacheKey(name string, input json.RawMessage) string {
	return toolport.BuildCacheKey(name, input)
}
