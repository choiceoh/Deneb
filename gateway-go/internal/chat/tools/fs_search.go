package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

func ToolGrep(defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Pattern      string `json:"pattern"`
			Path         string `json:"path"`
			Include      string `json:"include"`
			ContextLines int    `json:"contextLines"`
			Before       int    `json:"before"`
			After        int    `json:"after"`
			IgnoreCase   bool   `json:"ignoreCase"`
			MaxResults   int    `json:"maxResults"`
			FileType     string `json:"fileType"`
			Multiline    bool   `json:"multiline"`
			Mode         string `json:"mode"`
		}
		if err := jsonutil.UnmarshalInto("grep params", input, &p); err != nil {
			return "", err
		}
		if p.Pattern == "" {
			return "", fmt.Errorf("pattern is required")
		}

		searchPath := defaultDir
		if p.Path != "" {
			searchPath = ResolvePath(p.Path, defaultDir)
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
		if p.Multiline {
			args = append(args, "-U", "--multiline-dotall")
		}

		// Output mode.
		switch p.Mode {
		case "files_only":
			args = append(args, "-l")
		case "count":
			args = append(args, "-c")
		default:
			// content mode (default): use context lines.
			if p.Before > 0 || p.After > 0 {
				before := clampInt(p.Before, 0, 10)
				after := clampInt(p.After, 0, 10)
				if before > 0 {
					args = append(args, "-B", fmt.Sprintf("%d", before))
				}
				if after > 0 {
					args = append(args, "-A", fmt.Sprintf("%d", after))
				}
			} else if contextLines > 0 {
				args = append(args, "-C", fmt.Sprintf("%d", contextLines))
			}
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
			// Fall back to grep (fileType/multiline are rg-only, skip them here).
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
			switch p.Mode {
			case "files_only":
				grepArgs = append(grepArgs, "-l")
			case "count":
				grepArgs = append(grepArgs, "-c")
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

// clampInt clamps v to [min, max].
func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// --- Find tool ---

func ToolFind(defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Pattern    string `json:"pattern"`
			Path       string `json:"path"`
			ShowHidden bool   `json:"showHidden"`
		}
		if err := jsonutil.UnmarshalInto("find params", input, &p); err != nil {
			return "", err
		}
		if p.Pattern == "" {
			return "", fmt.Errorf("pattern is required")
		}

		searchDir := defaultDir
		if p.Path != "" {
			searchDir = ResolvePath(p.Path, defaultDir)
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

// --- Helpers ---

// resolvePath resolves a potentially relative path against the default directory.
// It validates that the resolved path does not escape the workspace boundary.
func ResolvePath(path, defaultDir string) string {
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
