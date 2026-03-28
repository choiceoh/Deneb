package chat

import (
	"context"
	"encoding/json"

	chattools "github.com/choiceoh/deneb/gateway-go/internal/chat/tools"
)

func adaptTool(fn chattools.ToolFunc) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		return fn(ctx, input)
	}
}

func toolRead(defaultDir string) ToolFunc        { return adaptTool(chattools.ToolRead(defaultDir)) }
func toolWrite(defaultDir string) ToolFunc       { return adaptTool(chattools.ToolWrite(defaultDir)) }
func toolEdit(defaultDir string) ToolFunc        { return adaptTool(chattools.ToolEdit(defaultDir)) }
func toolGrep(defaultDir string) ToolFunc        { return adaptTool(chattools.ToolGrep(defaultDir)) }
func toolFind(defaultDir string) ToolFunc        { return adaptTool(chattools.ToolFind(defaultDir)) }
func toolMultiEdit(defaultDir string) ToolFunc   { return adaptTool(chattools.ToolMultiEdit(defaultDir)) }
func toolTree(defaultDir string) ToolFunc        { return adaptTool(chattools.ToolTree(defaultDir)) }
func toolDiff(defaultDir string) ToolFunc        { return adaptTool(chattools.ToolDiff(defaultDir)) }
func toolAnalyze(defaultDir string) ToolFunc     { return adaptTool(chattools.ToolAnalyze(defaultDir)) }
func toolHTTP() ToolFunc                         { return adaptTool(chattools.ToolHTTP()) }
func resolvePath(path, defaultDir string) string { return chattools.ResolvePath(path, defaultDir) }
