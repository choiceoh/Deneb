// tool_cache_stability.go implements cache-stable tool list ordering.
//
// When sending tools to the LLM API, the prompt cache key depends on the
// exact bytes of the request. If tool ordering changes between turns
// (e.g., because a plugin tool was added/removed), the entire prompt cache
// is invalidated, wasting significant API cost.
//
// This module ensures built-in tools form a stable sorted prefix, and
// dynamic tools (plugins, skills) are appended as a separate sorted group.
// Changes to dynamic tools only invalidate the cache from the dynamic
// boundary onward.
//
// Inspired by Claude Code's assembleToolPool cache stability trick.
package chat

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// ToolPartition separates tools into a stable built-in prefix and a
// dynamic suffix for cache-optimal ordering.
type ToolPartition struct {
	// Builtin tools form the cache-stable prefix (sorted by name).
	Builtin []llm.Tool
	// Dynamic tools are appended after builtins (sorted separately).
	Dynamic []llm.Tool
	// CacheKey is a hash of the builtin tool names for cache invalidation detection.
	CacheKey string
}

// PartitionTools separates tools into builtin (registered in the core
// registry) and dynamic (everything else, e.g., MCP, plugin tools).
// Both groups are sorted alphabetically by name within their partition.
func PartitionTools(allTools []llm.Tool, builtinNames map[string]bool) ToolPartition {
	var builtin, dynamic []llm.Tool
	for _, t := range allTools {
		if builtinNames[t.Name] {
			builtin = append(builtin, t)
		} else {
			dynamic = append(dynamic, t)
		}
	}

	sort.Slice(builtin, func(i, j int) bool { return builtin[i].Name < builtin[j].Name })
	sort.Slice(dynamic, func(i, j int) bool { return dynamic[i].Name < dynamic[j].Name })

	return ToolPartition{
		Builtin:  builtin,
		Dynamic:  dynamic,
		CacheKey: computeToolCacheKey(builtin),
	}
}

// MergedTools returns the combined tool list (builtin prefix + dynamic suffix).
func (tp ToolPartition) MergedTools() []llm.Tool {
	result := make([]llm.Tool, 0, len(tp.Builtin)+len(tp.Dynamic))
	result = append(result, tp.Builtin...)
	result = append(result, tp.Dynamic...)
	return result
}

// computeToolCacheKey produces a deterministic hash from tool names.
func computeToolCacheKey(tools []llm.Tool) string {
	h := sha256.New()
	for _, t := range tools {
		h.Write([]byte(t.Name))
		h.Write([]byte{0}) // separator
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// FilterDeniedTools removes tools whose names appear in the deny set.
// This prevents the model from seeing (and attempting to call) denied tools,
// avoiding wasted generation tokens.
func FilterDeniedTools(tools []llm.Tool, denySet map[string]bool) []llm.Tool {
	if len(denySet) == 0 {
		return tools
	}
	filtered := make([]llm.Tool, 0, len(tools))
	for _, t := range tools {
		if !denySet[t.Name] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}
