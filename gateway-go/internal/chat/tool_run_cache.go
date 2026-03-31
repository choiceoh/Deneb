package chat

import (
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
)

// Type aliases — canonical definitions are in toolctx/.

// RunCache is a thread-safe, run-scoped cache for idempotent tool results.
type RunCache = toolctx.RunCache

// NewRunCache creates an empty run cache.
func NewRunCache() *RunCache { return toolctx.NewRunCache() }

// IsCacheableTool returns true if the named tool's results can be cached across calls.
func IsCacheableTool(name string) bool { return toolctx.IsCacheableTool(name) }

// IsMutationTool returns true if the named tool can modify files, triggering cache invalidation.
func IsMutationTool(name string) bool { return toolctx.IsMutationTool(name) }

// BuildCacheKey creates a canonical cache key from the tool name and its JSON input.
func BuildCacheKey(name string, input json.RawMessage) string {
	return toolctx.BuildCacheKey(name, input)
}
