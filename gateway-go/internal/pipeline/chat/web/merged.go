// merged.go — Unified web tool (search mode only).
package web

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// MergedTool returns the web search/fetch tool handler. The previous multi-mode
// dispatcher (request/research) has been removed; only the default search mode
// remains. spill (optional) offloads full YouTube transcripts to disk.
func MergedTool(cache *FetchCache, localAI *LocalAIExtractor, spill tooldeps.SpilloverStore) toolport.ToolFunc {
	return Tool(cache, localAI, spill)
}
