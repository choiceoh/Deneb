package toolreg

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/filesystem"
)

// RegisterFileTools registers the workspace file read/write/edit/grep surface.
func RegisterFileTools(registry toolport.ToolRegistrar, workspaceDir string, skillsCatalogDirs ...string) {
	registry.RegisterTool(toolport.ToolDef{
		Name:        "read",
		Description: "Read file contents with line numbers for code review (default: 2000 lines). Use offset/limit for large files; equivalent to a clean bat/cat -n view",
		InputSchema: readToolSchema(),
		Fn:          filesystem.ToolRead(workspaceDir, skillsCatalogDirs...),
	})
	registry.RegisterTool(toolport.ToolDef{
		Name:        "write",
		Description: "Create or overwrite a file. Auto-creates parent directories. Use edit for partial changes",
		InputSchema: writeToolSchema(),
		Fn:          filesystem.ToolWrite(workspaceDir),
	})
	// Deferred (prompt audit 2026-06-12): ~370 wire tokens for 2 uses in 14
	// days — Deneb is a chief-of-staff, not a coding agent, so partial file
	// edits are rare. read/write/grep stay eager; an editing turn fetches this.
	registry.RegisterTool(toolport.ToolDef{
		Name:        "edit",
		Description: "Search-and-replace in a file. old_string must be unique unless replace_all=true. Read first to find the exact string",
		InputSchema: editToolSchema(),
		Fn:          filesystem.ToolEdit(workspaceDir),
		Deferred:    true,
	})
	registry.RegisterTool(toolport.ToolDef{
		Name:        "grep",
		Description: "Regex search across files (rg / ripgrep). Use include/fileType to narrow scope. Returns file:line:match format",
		InputSchema: grepToolSchema(),
		Fn:          filesystem.ToolGrep(workspaceDir),
	})
}
