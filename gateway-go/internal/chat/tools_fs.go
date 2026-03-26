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
				"minimum":     1,
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "The number of lines to read (default: 2000)",
				"default":     2000,
				"minimum":     1,
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
		totalLines := len(lines)
		fileSize := len(data)

		// Apply offset (1-based).
		start := 0
		if p.Offset > 0 {
			start = p.Offset - 1
		}
		if start > totalLines {
			start = totalLines
		}

		// Apply limit (default: 2000 lines).
		limit := 2000
		if p.Limit > 0 {
			limit = p.Limit
		}
		end := start + limit
		if end > totalLines {
			end = totalLines
		}

		// Format with line numbers (cat -n style) and file metadata.
		var sb strings.Builder
		fmt.Fprintf(&sb, "[File: %s | %d lines | %d bytes]\n", p.FilePath, totalLines, fileSize)
		for i := start; i < end; i++ {
			fmt.Fprintf(&sb, "%6d\t%s\n", i+1, lines[i])
		}
		if end < totalLines {
			fmt.Fprintf(&sb, "[... %d more lines. Use offset=%d to continue reading.]\n", totalLines-end, end+1)
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
				"description": "The text to replace (must be unique unless replace_all is true)",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "The text to replace it with",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "Replace all occurrences instead of requiring a unique match (default: false)",
				"default":     false,
			},
		},
		"required": []string{"file_path", "old_string", "new_string"},
	}
}

func toolEdit(defaultDir string) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			FilePath   string `json:"file_path"`
			OldString  string `json:"old_string"`
			NewString  string `json:"new_string"`
			ReplaceAll bool   `json:"replace_all"`
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
		if count > 1 && !p.ReplaceAll {
			return "", fmt.Errorf("old_string is not unique in file (%d occurrences). Use replace_all=true to replace all", count)
		}

		var newContent string
		if p.ReplaceAll {
			newContent = strings.ReplaceAll(content, p.OldString, p.NewString)
		} else {
			newContent = strings.Replace(content, p.OldString, p.NewString, 1)
		}
		if err := os.WriteFile(path, []byte(newContent), 0o644); err != nil {
			return "", fmt.Errorf("failed to write file: %w", err)
		}
		if count > 1 {
			return fmt.Sprintf("Successfully edited %s (%d replacements)", p.FilePath, count), nil
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
			"contextLines": map[string]any{
				"type":        "number",
				"description": "Lines of context around each match (0-10)",
				"default":     0,
				"minimum":     0,
				"maximum":     10,
			},
			"ignoreCase": map[string]any{
				"type":        "boolean",
				"description": "Case-insensitive search",
				"default":     false,
			},
			"maxResults": map[string]any{
				"type":        "number",
				"description": "Maximum matches to return",
				"default":     100,
				"minimum":     1,
				"maximum":     500,
			},
			"fileType": map[string]any{
				"type":        "string",
				"description": "File type filter for ripgrep --type (e.g. \"go\", \"py\", \"js\")",
			},
		},
		"required": []string{"pattern"},
	}
}

func toolGrep(defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Pattern      string `json:"pattern"`
			Path         string `json:"path"`
			Include      string `json:"include"`
			ContextLines int    `json:"contextLines"`
			IgnoreCase   bool   `json:"ignoreCase"`
			MaxResults   int    `json:"maxResults"`
			FileType     string `json:"fileType"`
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

		// Defaults and caps.
		maxResults := p.MaxResults
		if maxResults <= 0 {
			maxResults = 100
		}
		if maxResults > 500 {
			maxResults = 500
		}
		contextLines := p.ContextLines
		if contextLines < 0 {
			contextLines = 0
		}
		if contextLines > 10 {
			contextLines = 10
		}

		// Try ripgrep first, fall back to grep.
		args := []string{"-n", fmt.Sprintf("--max-count=%d", maxResults)}
		if p.IgnoreCase {
			args = append(args, "-i")
		}
		if contextLines > 0 {
			args = append(args, "-C", fmt.Sprintf("%d", contextLines))
		}
		if p.Include != "" {
			args = append(args, "--glob", p.Include)
		}
		if p.FileType != "" {
			args = append(args, "--type", p.FileType)
		}
		args = append(args, p.Pattern, searchPath)

		cmd := exec.CommandContext(ctx, "rg", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			// rg exit code 1 means no matches (not an error).
			if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
				return "No matches found.", nil
			}
			// Fall back to grep (fileType is rg-only, skip it here).
			grepArgs := []string{"-rn", fmt.Sprintf("--max-count=%d", maxResults)}
			if p.IgnoreCase {
				grepArgs = append(grepArgs, "-i")
			}
			if contextLines > 0 {
				grepArgs = append(grepArgs, fmt.Sprintf("-C%d", contextLines))
			}
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
				"description": "Glob pattern to match files (supports ** for recursive matching, e.g. \"**/*.go\")",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory to search in",
			},
			"showHidden": map[string]any{
				"type":        "boolean",
				"description": "Include hidden directories (starting with .) in search",
				"default":     false,
			},
		},
		"required": []string{"pattern"},
	}
}

func toolFind(defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Pattern    string `json:"pattern"`
			Path       string `json:"path"`
			ShowHidden bool   `json:"showHidden"`
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

		const maxResults = 200

		// Use ripgrep for ** glob patterns (filepath.Match doesn't support **).
		if strings.Contains(p.Pattern, "**") {
			matches, err := findWithRipgrep(ctx, searchDir, p.Pattern, p.ShowHidden, maxResults)
			if err == nil {
				if len(matches) == 0 {
					return "No files found matching pattern.", nil
				}
				return strings.Join(matches, "\n"), nil
			}
			// rg unavailable or failed — fall through to WalkDir.
		}

		var matches []string
		err := filepath.WalkDir(searchDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip errors
			}
			if len(matches) >= maxResults {
				return filepath.SkipAll
			}
			// Skip hidden directories unless showHidden is set.
			if d.IsDir() && strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				if !p.ShowHidden {
					return filepath.SkipDir
				}
			}
			// Match against both the filename and the relative path.
			rel, _ := filepath.Rel(searchDir, path)
			matched, _ := filepath.Match(p.Pattern, d.Name())
			if !matched && rel != "" {
				matched, _ = filepath.Match(p.Pattern, rel)
			}
			if matched {
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

// findWithRipgrep uses `rg --files --glob` to find files matching ** patterns.
func findWithRipgrep(ctx context.Context, dir, pattern string, showHidden bool, maxResults int) ([]string, error) {
	args := []string{"--files", "--glob", pattern}
	if showHidden {
		args = append(args, "--hidden")
	}
	args = append(args, dir)

	cmd := exec.CommandContext(ctx, "rg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// rg exit code 1 means no matches.
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var matches []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		// Convert absolute paths to relative.
		rel, relErr := filepath.Rel(dir, line)
		if relErr != nil {
			rel = line
		}
		matches = append(matches, rel)
		if len(matches) >= maxResults {
			break
		}
	}
	return matches, nil
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
// It validates that the resolved path does not escape the workspace boundary.
func resolvePath(path, defaultDir string) string {
	var resolved string
	if filepath.IsAbs(path) {
		resolved = filepath.Clean(path)
	} else {
		resolved = filepath.Clean(filepath.Join(defaultDir, path))
	}

	// Security: verify the resolved path is under the workspace root
	// to prevent path traversal attacks (e.g., "../../etc/passwd").
	absDefault, err := filepath.Abs(defaultDir)
	if err != nil {
		return resolved
	}
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return resolved
	}
	if !strings.HasPrefix(absResolved, absDefault+string(filepath.Separator)) && absResolved != absDefault {
		// Path escapes workspace — clamp to workspace root.
		return absDefault
	}

	return resolved
}
