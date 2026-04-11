// merged.go — Unified web tool (search mode only).
package web

import (
	"context"
	"encoding/json"
)

// MergedTool returns the web search/fetch tool handler. The previous multi-mode
// dispatcher (request/research) has been removed; only the default search mode
// remains.
func MergedTool(cache *FetchCache, localAI *LocalAIExtractor) func(context.Context, json.RawMessage) (string, error) {
	return Tool(cache, localAI)
}
