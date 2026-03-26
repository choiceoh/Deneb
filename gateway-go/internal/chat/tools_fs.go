package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// --- Read tool ---

func readToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The absolute path to the file to read",
			},
			"offset": map[string]any{
				"type":        "number",
				"description": "The line number to start reading from (1-based)",
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "The number of lines to read",
			},
		},
		"required": []string{"file_path"},
	}
}

func toolRead(defaultDir string) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			FilePath string `json:"file_path"`
			Offset   int    `json:"offset"`
			Limit    int    `json:"limit"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid read params: %w", err)
		}
		if p.FilePath == "" {
			return "", fmt.Errorf("file_path is required")
		}

		path := resolvePath(p.FilePath, defaultDir)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("failed to read file: %w", err)
		}

		lines := strings.Split(string(data), "\n")

		// Apply offset (1-based).
		start := 0
		if p.Offset > 0 {
			start = p.Offset - 1
		}
		if start > len(lines) {
			start = len(lines)
		}

		// Apply limit (default: 2000 lines).
		limit := 2000
		if p.Limit > 0 {
			limit = p.Limit
		}
		end := start + limit
		if end > len(lines) {
			end = len(lines)
		}

		// Format with line numbers (cat -n style).
		var sb strings.Builder
		for i := start; i < end; i++ {
			fmt.Fprintf(&sb, "%6d\t%s\n", i+1, lines[i])
		}
		return sb.String(), nil
	}
}

// --- Write tool ---

func writeToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The absolute path to the file to write",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write to the file",
			},
		},
		"required": []string{"file_path", "content"},
	}
}

func toolWrite(defaultDir string) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			FilePath string `json:"file_path"`
			Content  string `json:"content"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid write params: %w", err)
		}
		if p.FilePath == "" {
			return "", fmt.Errorf("file_path is required")
		}

		path := resolvePath(p.FilePath, defaultDir)

		// Ensure parent directory exists.
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("failed to create directory: %w", err)
		}

		if err := os.WriteFile(path, []byte(p.Content), 0o644); err != nil {
			return "", fmt.Errorf("failed to write file: %w", err)
		}
		return fmt.Sprintf("Successfully wrote %d bytes to %s", len(p.Content), p.FilePath), nil
	}
}

// --- Edit tool ---

func editToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The absolute path to the file to modify",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "The text to replace (must be unique in the file)",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "The text to replace it with",
			},
		},
		"required": []string{"file_path", "old_string", "new_string"},
	}
}

func toolEdit(defaultDir string) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			FilePath  string `json:"file_path"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid edit params: %w", err)
		}
		if p.FilePath == "" {
			return "", fmt.Errorf("file_path is required")
		}
		if p.OldString == "" {
			return "", fmt.Errorf("old_string is required")
		}

		path := resolvePath(p.FilePath, defaultDir)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("failed to read file: %w", err)
		}

		content := string(data)
		count := strings.Count(content, p.OldString)
		if count == 0 {
			return "", fmt.Errorf("old_string not found in file")
		}
		if count > 1 {
			return "", fmt.Errorf("old_string is not unique in file (%d occurrences)", count)
		}

		newContent := strings.Replace(content, p.OldString, p.NewString, 1)
		if err := os.WriteFile(path, []byte(newContent), 0o644); err != nil {
			return "", fmt.Errorf("failed to write file: %w", err)
		}
		return fmt.Sprintf("Successfully edited %s", p.FilePath), nil
	}
}

// --- Grep tool ---

func grepToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regular expression pattern to search for",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "File or directory to search in",
			},
			"include": map[string]any{
				"type":        "string",
				"description": "Glob pattern to filter files (e.g. \"*.ts\")",
			},
		},
		"required": []string{"pattern"},
	}
}

func toolGrep(defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
			Include string `json:"include"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid grep params: %w", err)
		}
		if p.Pattern == "" {
			return "", fmt.Errorf("pattern is required")
		}

		searchPath := defaultDir
		if p.Path != "" {
			searchPath = resolvePath(p.Path, defaultDir)
		}

		// Try ripgrep first, fall back to grep.
		args := []string{"-n", "--max-count=100"}
		if p.Include != "" {
			args = append(args, "--glob", p.Include)
		}
		args = append(args, p.Pattern, searchPath)

		cmd := exec.CommandContext(ctx, "rg", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			// rg exit code 1 means no matches (not an error).
			if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
				return "No matches found.", nil
			}
			// Fall back to grep.
			grepArgs := []string{"-rn", "--max-count=100"}
			if p.Include != "" {
				grepArgs = append(grepArgs, "--include="+p.Include)
			}
			grepArgs = append(grepArgs, p.Pattern, searchPath)
			cmd2 := exec.CommandContext(ctx, "grep", grepArgs...)
			out2, err2 := cmd2.CombinedOutput()
			if err2 != nil {
				if cmd2.ProcessState != nil && cmd2.ProcessState.ExitCode() == 1 {
					return "No matches found.", nil
				}
				return string(out2), nil
			}
			return string(out2), nil
		}
		return string(out), nil
	}
}

// --- Find tool ---

func findToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Glob pattern to match files against",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory to search in",
			},
		},
		"required": []string{"pattern"},
	}
}

func toolFind(defaultDir string) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid find params: %w", err)
		}
		if p.Pattern == "" {
			return "", fmt.Errorf("pattern is required")
		}

		searchDir := defaultDir
		if p.Path != "" {
			searchDir = resolvePath(p.Path, defaultDir)
		}

		var matches []string
		const maxResults = 200
		err := filepath.WalkDir(searchDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip errors
			}
			if len(matches) >= maxResults {
				return filepath.SkipAll
			}
			// Skip hidden directories.
			if d.IsDir() && strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return filepath.SkipDir
			}
			matched, _ := filepath.Match(p.Pattern, d.Name())
			if matched {
				rel, _ := filepath.Rel(searchDir, path)
				if rel == "" {
					rel = path
				}
				matches = append(matches, rel)
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("find failed: %w", err)
		}
		if len(matches) == 0 {
			return "No files found matching pattern.", nil
		}
		return strings.Join(matches, "\n"), nil
	}
}

// --- Ls tool ---

func lsToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Directory to list (defaults to workspace root)",
			},
		},
	}
}

func toolLs(defaultDir string) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid ls params: %w", err)
		}

		dir := defaultDir
		if p.Path != "" {
			dir = resolvePath(p.Path, defaultDir)
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return "", fmt.Errorf("failed to read directory: %w", err)
		}

		var sb strings.Builder
		for _, entry := range entries {
			if entry.IsDir() {
				fmt.Fprintf(&sb, "%s/\n", entry.Name())
			} else {
				info, _ := entry.Info()
				if info != nil {
					fmt.Fprintf(&sb, "%s (%d bytes)\n", entry.Name(), info.Size())
				} else {
					fmt.Fprintf(&sb, "%s\n", entry.Name())
				}
			}
		}
		if sb.Len() == 0 {
			return "(empty directory)", nil
		}
		return sb.String(), nil
	}
}

// --- Helpers ---

// resolvePath resolves a potentially relative path against the default directory.
func resolvePath(path, defaultDir string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(defaultDir, path)
}
