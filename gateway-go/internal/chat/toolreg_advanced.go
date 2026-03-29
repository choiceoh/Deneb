package chat

import (
	chattools "github.com/choiceoh/deneb/gateway-go/internal/chat/tools"
)

// RegisterAdvancedTools registers composed tools that combine basic tool
// operations into higher-level, multi-step workflows.
// These tools reduce round-trips by executing multiple atomic operations
// (read, grep, analyze, git) in a single call.
func RegisterAdvancedTools(registry *ToolRegistry, workspaceDir string) {
	registry.RegisterTool(ToolDef{
		Name:        "batch_read",
		Description: "Read multiple files in one call (up to 20). Each file supports offset/limit/function extraction. Partial failures reported individually without aborting",
		InputSchema: batchReadToolSchema(),
		Fn:          adaptTool(chattools.ToolBatchRead(workspaceDir)),
	})
	registry.RegisterTool(ToolDef{
		Name:        "search_and_read",
		Description: "Grep for a pattern then auto-read matching files with surrounding context. Combines grep+read into one step. Returns file content around each match",
		InputSchema: searchAndReadToolSchema(),
		Fn:          adaptTool(chattools.ToolSearchAndRead(workspaceDir)),
	})
	registry.RegisterTool(ToolDef{
		Name:        "inspect",
		Description: "Deep code inspection: file outline + imports + git history in one call. depth=shallow (outline+imports), deep (+git log+stats), symbol (+definition+references+blame). Auto-promotes to symbol depth when symbol param is set",
		InputSchema: inspectToolSchema(),
		Fn:          adaptTool(chattools.ToolInspect(workspaceDir)),
	})
	registry.RegisterTool(ToolDef{
		Name:        "apply_patch",
		Description: "Apply a unified diff patch (git diff format). Handles multi-file, multi-hunk patches atomically via git apply. Use dry_run=true to verify before applying",
		InputSchema: applyPatchToolSchema(),
		Fn:          adaptTool(chattools.ToolApplyPatch(workspaceDir)),
	})
}
