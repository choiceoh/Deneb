package chat

import chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"

func collectMemoryFiles(workspaceDir string) []string {
	return chattools.CollectMemoryFiles(workspaceDir)
}

func readMemoryFile(path string) (string, error) {
	return chattools.ReadMemoryFile(path)
}
