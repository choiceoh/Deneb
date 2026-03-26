package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ContextFile represents a workspace context file embedded in the system prompt.
type ContextFile struct {
	Path    string // relative path (e.g., "CLAUDE.md")
	Content string
}

// contextFileNames lists workspace context files in load order.
// Matches src/agents/workspace/workspace.ts DEFAULT_*_FILENAME constants.
var contextFileNames = []string{
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

// LoadContextFiles scans the workspace directory and its ancestors for known
// context files (CLAUDE.md, SOUL.md, TOOLS.md, etc.) and returns
// their contents. Files closer to the workspace root take precedence.
// This mirrors the Node.js behavior of walking up the directory tree.
func LoadContextFiles(workspaceDir string) []ContextFile {
	if workspaceDir == "" {
		return nil
	}

	// Collect search directories: workspace first, then parents up to root or ~/.
	searchDirs := collectSearchDirs(workspaceDir)

	var files []ContextFile
	totalChars := 0
	seen := make(map[string]bool) // track resolved paths for dedup

	for _, name := range contextFileNames {
		for _, dir := range searchDirs {
			path := filepath.Join(dir, name)

			// Follow symlinks in case context files are symlinked.
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				continue
			}

			// Skip if we already loaded this resolved path.
			if seen[resolved] {
				break
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

			// Content-based dedup (handles symlinks pointing to same file).
			isDup := false
			for _, existing := range files {
				if existing.Content == content {
					isDup = true
					break
				}
			}
			if isDup {
				seen[resolved] = true
				break
			}

			// Use relative label: if from workspace root, just the filename;
			// otherwise include relative path hint.
			label := name
			if dir != workspaceDir {
				rel, _ := filepath.Rel(workspaceDir, filepath.Join(dir, name))
				if rel != "" {
					label = rel
				}
			}

			files = append(files, ContextFile{
				Path:    label,
				Content: content,
			})
			totalChars += len(content)
			seen[resolved] = true
			break // Found for this filename, don't search further up
		}
	}

	return files
}

// collectSearchDirs returns the workspace dir plus its ancestors, stopping
// at the user's home directory or filesystem root (max 10 levels).
func collectSearchDirs(workspaceDir string) []string {
	dirs := []string{workspaceDir}

	home, _ := os.UserHomeDir()
	current := workspaceDir
	for i := 0; i < 10; i++ {
		parent := filepath.Dir(current)
		if parent == current {
			break // reached filesystem root
		}
		if home != "" && parent == home {
			// Include home but don't go above it.
			dirs = append(dirs, parent)
			break
		}
		dirs = append(dirs, parent)
		current = parent
	}

	return dirs
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
// in the system prompt. If SOUL.md is present, an explicit instruction to
// embody its persona/tone is injected (mirrors the TS system-prompt behavior).
func FormatContextFilesForPrompt(files []ContextFile) string {
	if len(files) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# Project Context\n\n")

	// Detect SOUL.md presence.
	hasSoulFile := false
	for _, f := range files {
		base := filepath.Base(f.Path)
		if strings.EqualFold(base, "SOUL.md") {
			hasSoulFile = true
			break
		}
	}

	sb.WriteString("The following project context files have been loaded:\n")
	if hasSoulFile {
		sb.WriteString("If SOUL.md is present, embody its persona and tone. Avoid stiff, generic replies; follow its guidance unless higher-priority instructions override it.\n")
	}
	sb.WriteString("\n")

	for _, f := range files {
		fmt.Fprintf(&sb, "## %s\n\n%s\n\n", f.Path, strings.TrimSpace(f.Content))
	}

	return sb.String()
}
