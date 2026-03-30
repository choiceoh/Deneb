package chat

import (
	chattools "github.com/choiceoh/deneb/gateway-go/internal/chat/tools"
)

// Bridge functions: thin wrappers that delegate to tools/ implementations.
// Kept for backward compatibility with existing tests (toolreg_fs_test.go,
// toolreg_coding_test.go). No type adaptation needed — both ToolFunc types
// are aliased to toolctx.ToolFunc.

func toolRead(defaultDir string) ToolFunc      { return chattools.ToolRead(defaultDir) }
func toolWrite(defaultDir string) ToolFunc     { return chattools.ToolWrite(defaultDir) }
func toolEdit(defaultDir string) ToolFunc      { return chattools.ToolEdit(defaultDir) }
func toolGrep(defaultDir string) ToolFunc      { return chattools.ToolGrep(defaultDir) }
func toolFind(defaultDir string) ToolFunc      { return chattools.ToolFind(defaultDir) }
func toolMultiEdit(defaultDir string) ToolFunc { return chattools.ToolMultiEdit(defaultDir) }
func toolTree(defaultDir string) ToolFunc      { return chattools.ToolTree(defaultDir) }
func toolDiff(defaultDir string) ToolFunc      { return chattools.ToolDiff(defaultDir) }
func toolAnalyze(defaultDir string) ToolFunc   { return chattools.ToolAnalyze(defaultDir) }
func toolHTTP() ToolFunc                       { return chattools.ToolHTTP() }
func resolvePath(path, defaultDir string) string { return chattools.ResolvePath(path, defaultDir) }
