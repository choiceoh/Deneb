package chat

import (
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
)

// Type aliases — canonical definitions are in toolctx/.

type RunCache = toolctx.RunCache

func NewRunCache() *RunCache { return toolctx.NewRunCache() }

func IsCacheableTool(name string) bool { return toolctx.IsCacheableTool(name) }

func IsMutationTool(name string) bool { return toolctx.IsMutationTool(name) }

func BuildCacheKey(name string, input json.RawMessage) string {
	return toolctx.BuildCacheKey(name, input)
}
