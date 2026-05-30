// merged.go — Unified web tool (search mode only).
package web

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
)

// MergedTool returns the web search/fetch tool handler. The previous multi-mode
// dispatcher (request/research) has been removed; only the default search mode
// remains. spill (optional) offloads full YouTube transcripts to disk.
func MergedTool(cache *FetchCache, localAI *LocalAIExtractor, spill *agent.SpilloverStore) func(context.Context, json.RawMessage) (string, error) {
	return Tool(cache, localAI, spill)
}
