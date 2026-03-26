package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ContextFile represents a workspace context file embedded in the system prompt.
type ContextFile struct {
	Path    string // relative path (e.g., "AGENTS.md")
	Content string
}

// contextFileNames lists workspace context files in load order.
// Matches src/agents/workspace/workspace.ts DEFAULT_*_FILENAME constants.
var contextFileNames = []string{
	"AGENTS.md",
	"CLAUDE.md",
	"SOUL.md",
	"TOOLS.md",
	"IDENTITY.md",
	"USER.md",
	"MEMORY.md",
}

const (
	// maxContextFileChars is the maximum characters per context file.
	maxContextFileChars = 20_000
	// maxContextTotalChars is the maximum total characters for all context files.
	maxContextTotalChars = 150_000
)

// LoadContextFiles scans the workspace directory for known context files
// (AGENTS.md, CLAUDE.md, SOUL.md, TOOLS.md, etc.) and returns their contents.
// Files are truncated if they exceed size limits.
func LoadContextFiles(workspaceDir string) []ContextFile {
	if workspaceDir == "" {
		return nil
	}

	var files []ContextFile
	totalChars := 0

	for _, name := range contextFileNames {
		path := filepath.Join(workspaceDir, name)

		// Follow symlinks (CLAUDE.md is often a symlink to AGENTS.md).
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			continue
		}

		data, err := os.ReadFile(resolved)
		if err != nil {
			continue
		}

		content := string(data)
		if len(content) == 0 {
			continue
		}

		// Skip if this would exceed total budget.
		if totalChars+len(content) > maxContextTotalChars {
			// Truncate to fit remaining budget.
			remaining := maxContextTotalChars - totalChars
			if remaining <= 0 {
				break
			}
			content = truncateContent(content, remaining)
		}

		// Truncate individual file if too large.
		if len(content) > maxContextFileChars {
			content = truncateContent(content, maxContextFileChars)
		}

		// Check if we already loaded this content (symlink dedup).
		isDup := false
		for _, existing := range files {
			if existing.Content == content {
				isDup = true
				break
			}
		}
		if isDup {
			continue
		}

		files = append(files, ContextFile{
			Path:    name,
			Content: content,
		})
		totalChars += len(content)
	}

	return files
}

// truncateContent truncates content to maxChars using head (70%) + marker + tail (20%).
func truncateContent(content string, maxChars int) string {
	if len(content) <= maxChars {
		return content
	}
	headSize := maxChars * 70 / 100
	tailSize := maxChars * 20 / 100
	marker := "\n\n[...truncated...]\n\n"

	head := content[:headSize]
	tail := content[len(content)-tailSize:]
	return head + marker + tail
}

// FormatContextFilesForPrompt formats loaded context files for inclusion
// in the system prompt.
func FormatContextFilesForPrompt(files []ContextFile) string {
	if len(files) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# Project Context\n\n")

	for _, f := range files {
		fmt.Fprintf(&sb, "## %s\n\n%s\n\n", f.Path, strings.TrimSpace(f.Content))
	}

	return sb.String()
}
