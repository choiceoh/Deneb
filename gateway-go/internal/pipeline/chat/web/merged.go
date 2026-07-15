// merged.go — Unified web tool registration helper (search + fetch + search+fetch).
package web

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// MergedTool returns the web search/fetch tool handler (url, query, queries,
// search+fetch, typed search). spill (optional) offloads full YouTube
// transcripts to disk.
func MergedTool(cache *FetchCache, localAI *LocalAIExtractor, spill tooldeps.SpilloverStore) toolport.ToolFunc {
	return Tool(cache, localAI, spill)
}
