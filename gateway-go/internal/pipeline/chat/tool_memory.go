package chat

import chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"

// MemoryMatch is re-exported from the tools package.
type MemoryMatch = chattools.MemoryMatch

func collectMemoryFiles(workspaceDir string) []string {
	return chattools.CollectMemoryFiles(workspaceDir)
}

func readMemoryFile(path string) (string, error) {
	return chattools.ReadMemoryFile(path)
}
