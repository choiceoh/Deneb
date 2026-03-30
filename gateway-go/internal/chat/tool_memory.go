package chat

import chattools "github.com/choiceoh/deneb/gateway-go/internal/chat/tools"

// MemoryMatch is re-exported from the tools package.
type MemoryMatch = chattools.MemoryMatch

func toolMemorySearch(workspaceDir string) ToolFunc { return chattools.ToolMemorySearch(workspaceDir) }

func searchMemoryFiles(workspaceDir string, query string, limit int) []MemoryMatch {
	return chattools.SearchMemoryFiles(workspaceDir, query, limit)
}

func collectMemoryFiles(workspaceDir string) []string {
	return chattools.CollectMemoryFiles(workspaceDir)
}

func readMemoryFile(path string) (string, error) {
	return chattools.ReadMemoryFile(path)
}
