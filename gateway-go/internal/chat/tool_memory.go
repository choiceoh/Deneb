package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const memoryFileCacheTTL = 2 * time.Second

// memFileCache caches the result of collectMemoryFiles to avoid repeated
// os.Stat + os.ReadDir calls within a short window. TTL-based invalidation
// follows the plugin discovery cache pattern.
var memFileCache = &memoryFileListCache{}

type memoryFileListCache struct {
	mu        sync.Mutex
	workspace string
	files     []string
	expiresAt time.Time
}

func (c *memoryFileListCache) get(workspace string) ([]string, bool) {
	if c.workspace != workspace {
		return nil, false
	}
	if time.Now().After(c.expiresAt) {
		return nil, false
	}
	return c.files, true
}

func (c *memoryFileListCache) set(workspace string, files []string) {
	c.workspace = workspace
	c.files = files
	c.expiresAt = time.Now().Add(memoryFileCacheTTL)
}

// memorySearchToolSchema returns the JSON Schema for the memory_search tool.
func memorySearchToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query for memory files",
			},
		},
		"required": []string{"query"},
	}
}

// memoryGetToolSchema returns the JSON Schema for the memory_get tool.
func memoryGetToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to memory file (relative to workspace)",
			},
			"startLine": map[string]any{
				"type":        "number",
				"description": "Start line (1-based)",
			},
			"endLine": map[string]any{
				"type":        "number",
				"description": "End line (1-based, inclusive)",
			},
		},
		"required": []string{"path"},
	}
}

// toolMemorySearch implements keyword-based search across MEMORY.md and memory/*.md.
func toolMemorySearch(workspaceDir string) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid memory_search params: %w", err)
		}
		if p.Query == "" {
			return "", fmt.Errorf("query is required")
		}

		// Collect memory files.
		memoryFiles := collectMemoryFiles(workspaceDir)
		if len(memoryFiles) == 0 {
			return fmt.Sprintf("No memory files found in workspace %q (looked for MEMORY.md, memory.md, memory/*.md).", workspaceDir), nil
		}

		keywords := strings.Fields(strings.ToLower(p.Query))
		var results []string

		for _, path := range memoryFiles {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			content := string(data)
			lines := strings.Split(content, "\n")
			rel, _ := filepath.Rel(workspaceDir, path)
			if rel == "" {
				rel = path
			}

			// Find lines matching any keyword, deduplicate by line number.
			matchedLines := make(map[int]bool)
			for i, line := range lines {
				if matchedLines[i] {
					continue
				}
				lower := strings.ToLower(line)
				for _, kw := range keywords {
					if strings.Contains(lower, kw) {
						// Include surrounding context (±2 lines).
						start := i - 2
						if start < 0 {
							start = 0
						}
						end := i + 3
						if end > len(lines) {
							end = len(lines)
						}
						// Mark all context lines as seen to avoid duplicates.
						for j := start; j < end; j++ {
							matchedLines[j] = true
						}
						snippet := strings.Join(lines[start:end], "\n")
						results = append(results, fmt.Sprintf("### %s (line %d)\n%s", rel, i+1, snippet))
						break
					}
				}
			}
		}

		if len(results) == 0 {
			return fmt.Sprintf("No matches found for %q in memory files.", p.Query), nil
		}

		// Cap results.
		if len(results) > 20 {
			results = results[:20]
			results = append(results, fmt.Sprintf("\n... and more results (showing 20 of total)"))
		}

		return strings.Join(results, "\n\n"), nil
	}
}

// toolMemoryGet implements reading specific lines from a memory file.
func toolMemoryGet(workspaceDir string) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Path      string `json:"path"`
			StartLine int    `json:"startLine"`
			EndLine   int    `json:"endLine"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid memory_get params: %w", err)
		}
		if p.Path == "" {
			return "", fmt.Errorf("path is required")
		}

		path := resolvePath(p.Path, workspaceDir)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("failed to read memory file: %w", err)
		}

		lines := strings.Split(string(data), "\n")

		// Apply line range if specified.
		start := 0
		end := len(lines)
		if p.StartLine > 0 {
			start = p.StartLine - 1
		}
		if p.EndLine > 0 && p.EndLine <= len(lines) {
			end = p.EndLine
		}
		if start > end {
			start = end
		}
		if start > len(lines) {
			start = len(lines)
		}

		var sb strings.Builder
		for i := start; i < end; i++ {
			fmt.Fprintf(&sb, "%d\t%s\n", i+1, lines[i])
		}
		return sb.String(), nil
	}
}

// collectMemoryFiles finds MEMORY.md and memory/*.md in the workspace.
// Results are cached with a short TTL to avoid repeated directory scans
// within the same agent turn.
func collectMemoryFiles(workspaceDir string) []string {
	memFileCache.mu.Lock()
	defer memFileCache.mu.Unlock()

	if cached, ok := memFileCache.get(workspaceDir); ok {
		return cached
	}

	files := scanMemoryFiles(workspaceDir)
	memFileCache.set(workspaceDir, files)
	return files
}

// scanMemoryFiles performs the actual filesystem scan for memory files.
func scanMemoryFiles(workspaceDir string) []string {
	var files []string

	// Check MEMORY.md at workspace root.
	memoryMd := filepath.Join(workspaceDir, "MEMORY.md")
	if _, err := os.Stat(memoryMd); err == nil {
		files = append(files, memoryMd)
	}

	// Check memory.md (lowercase variant).
	memoryMdLower := filepath.Join(workspaceDir, "memory.md")
	if _, err := os.Stat(memoryMdLower); err == nil {
		files = append(files, memoryMdLower)
	}

	// Check memory/*.md directory.
	memoryDir := filepath.Join(workspaceDir, "memory")
	entries, err := os.ReadDir(memoryDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
				files = append(files, filepath.Join(memoryDir, e.Name()))
			}
		}
	}

	return files
}
