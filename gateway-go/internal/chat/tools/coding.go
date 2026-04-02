package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// --- Multi-edit tool ---
// Applies multiple search-and-replace edits across one or more files in a single call.
// Essential for refactoring: rename a symbol, update imports, or make coordinated
// changes across files without multiple round-trips.

func ToolMultiEdit(defaultDir string) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Edits []struct {
				FilePath   string `json:"file_path"`
				OldString  string `json:"old_string"`
				NewString  string `json:"new_string"`
				ReplaceAll bool   `json:"replace_all"`
			} `json:"edits"`
		}
		if err := jsonutil.UnmarshalInto("multi_edit params", input, &p); err != nil {
			return "", err
		}
		if len(p.Edits) == 0 {
			return "", fmt.Errorf("edits array is required and must not be empty")
		}
		if len(p.Edits) > 50 {
			return "", fmt.Errorf("too many edits (%d), max is 50", len(p.Edits))
		}

		// Cache file contents to avoid re-reading the same file for multiple edits.
		fileCache := make(map[string]string)

		var results []string
		successCount := 0
		failCount := 0

		for i, edit := range p.Edits {
			if edit.FilePath == "" || edit.OldString == "" {
				results = append(results, fmt.Sprintf("[%d] SKIP: file_path and old_string required", i+1))
				failCount++
				continue
			}

			path := ResolvePath(edit.FilePath, defaultDir)

			// Read file (from cache or disk).
			content, cached := fileCache[path]
			if !cached {
				data, err := os.ReadFile(path)
				if err != nil {
					results = append(results, fmt.Sprintf("[%d] FAIL %s: %v", i+1, edit.FilePath, err))
					failCount++
					continue
				}
				content = string(data)
			}

			count := strings.Count(content, edit.OldString)
			if count == 0 {
				results = append(results, fmt.Sprintf("[%d] FAIL %s: old_string not found", i+1, edit.FilePath))
				failCount++
				continue
			}
			if count > 1 && !edit.ReplaceAll {
				results = append(results, fmt.Sprintf("[%d] FAIL %s: old_string not unique (%d occurrences, use replace_all)", i+1, edit.FilePath, count))
				failCount++
				continue
			}

			var newContent string
			if edit.ReplaceAll {
				newContent = strings.ReplaceAll(content, edit.OldString, edit.NewString)
			} else {
				newContent = strings.Replace(content, edit.OldString, edit.NewString, 1)
			}

			fileCache[path] = newContent

			if count > 1 {
				results = append(results, fmt.Sprintf("[%d] OK %s (%d replacements)", i+1, edit.FilePath, count))
			} else {
				results = append(results, fmt.Sprintf("[%d] OK %s", i+1, edit.FilePath))
			}
			successCount++
		}

		// Write all modified files back to disk atomically (flock + tmp + rename).
		writtenFiles := make(map[string]bool)
		for path, content := range fileCache {
			if err := atomicfile.WriteFile(path, []byte(content), nil); err != nil {
				results = append(results, fmt.Sprintf("WRITE FAIL %s: %v", path, err))
				failCount++
				successCount-- // undo the OK count
			}
			writtenFiles[path] = true
		}

		summary := fmt.Sprintf("\n--- %d succeeded, %d failed, %d files modified ---",
			successCount, failCount, len(writtenFiles))
		results = append(results, summary)
		return strings.Join(results, "\n"), nil
	}
}

// --- Tree tool ---
// Displays a directory tree with configurable depth.
// Helps the agent quickly understand project structure without multiple ls/find calls.

// skipDirs are directories always excluded from tree output to avoid noise.
var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, "__pycache__": true,
	".tox": true, ".mypy_cache": true, ".pytest_cache": true,
	"vendor": true, "target": true, "dist": true, "build": true,
	".next": true, ".nuxt": true, ".cache": true,
}

// ToolTree returns a tool that renders a directory tree rooted at defaultDir.
// Uses eza/exa when available for fast output; falls back to a pure-Go walker.
func ToolTree(defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Path       string `json:"path"`
			Depth      int    `json:"depth"`
			ShowHidden bool   `json:"show_hidden"`
			DirsOnly   bool   `json:"dirs_only"`
			Pattern    string `json:"pattern"`
		}
		if err := jsonutil.UnmarshalInto("tree params", input, &p); err != nil {
			return "", err
		}

		dir := defaultDir
		if p.Path != "" {
			dir = ResolvePath(p.Path, defaultDir)
		}
		maxDepth := p.Depth
		if maxDepth <= 0 {
			maxDepth = 3
		}
		if maxDepth > 6 {
			maxDepth = 6
		}

		if p.Pattern == "" && !p.DirsOnly {
			if out, used, err := treeWithEza(ctx, dir, maxDepth, p.ShowHidden); used {
				if err != nil {
					return "", err
				}
				return TruncateForLLM(out), nil
			}
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "%s/\n", filepath.Base(dir))

		fileCount, dirCount := buildTree(&sb, dir, "", maxDepth, 0, p.ShowHidden, p.DirsOnly, p.Pattern)

		fmt.Fprintf(&sb, "\n%d directories, %d files", dirCount, fileCount)
		return TruncateForLLM(sb.String()), nil
	}
}

func treeWithEza(ctx context.Context, dir string, maxDepth int, showHidden bool) (string, bool, error) {
	bin, ok := firstAvailableBinary("eza", "exa")
	if !ok {
		return "", false, nil
	}

	ignore := make([]string, 0, len(skipDirs))
	for name := range skipDirs {
		ignore = append(ignore, name)
	}

	args := []string{
		"--tree",
		fmt.Sprintf("--level=%d", maxDepth),
		"--color=never",
		"--icons=never",
		"--group-directories-first",
	}
	if showHidden {
		args = append(args, "--all")
	}
	args = append(args, "--ignore-glob", strings.Join(ignore, "|"), ".")

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return "", true, fmt.Errorf("eza tree failed: %w", err)
		}
		return "", true, fmt.Errorf("eza tree failed: %s", msg)
	}

	result := strings.TrimRight(string(out), "\n")
	if result == "" {
		result = filepath.Base(dir) + "/"
	}
	return result + "\n\n[fast eza tree listing; directories and files shown above]", true, nil
}

// buildTree recursively builds the tree string. Returns (fileCount, dirCount).
func buildTree(sb *strings.Builder, dir, prefix string, maxDepth, currentDepth int, showHidden, dirsOnly bool, pattern string) (int, int) {
	if currentDepth >= maxDepth {
		return 0, 0
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}

	// Sort: directories first, then files, alphabetically within each group.
	sort.Slice(entries, func(i, j int) bool {
		di, dj := entries[i].IsDir(), entries[j].IsDir()
		if di != dj {
			return di
		}
		return entries[i].Name() < entries[j].Name()
	})

	// Filter entries.
	var filtered []os.DirEntry
	for _, e := range entries {
		name := e.Name()
		// Skip hidden unless requested.
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		// Skip known noisy directories.
		if e.IsDir() && skipDirs[name] {
			continue
		}
		// Skip files if dirs_only.
		if dirsOnly && !e.IsDir() {
			continue
		}
		// Apply pattern filter (only to files).
		if pattern != "" && !e.IsDir() {
			matched, _ := filepath.Match(pattern, name)
			if !matched {
				continue
			}
		}
		filtered = append(filtered, e)
	}

	totalFiles := 0
	totalDirs := 0

	for i, entry := range filtered {
		isLast := i == len(filtered)-1
		connector := "├── "
		childPrefix := "│   "
		if isLast {
			connector = "└── "
			childPrefix = "    "
		}

		name := entry.Name()
		if entry.IsDir() {
			fmt.Fprintf(sb, "%s%s%s/\n", prefix, connector, name)
			totalDirs++
			childPath := filepath.Join(dir, name)
			fc, dc := buildTree(sb, childPath, prefix+childPrefix, maxDepth, currentDepth+1, showHidden, dirsOnly, pattern)
			totalFiles += fc
			totalDirs += dc
		} else {
			info, _ := entry.Info()
			if info != nil {
				fmt.Fprintf(sb, "%s%s%s (%s)\n", prefix, connector, name, formatSize(info.Size()))
			} else {
				fmt.Fprintf(sb, "%s%s%s\n", prefix, connector, name)
			}
			totalFiles++
		}
	}

	return totalFiles, totalDirs
}

// formatSize returns a human-readable file size string.
func formatSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// --- Diff tool ---
// Shows git diff, file comparison, or uncommitted changes.
// Coding agents need diff to review changes before committing.

func ToolDiff(defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Mode         string `json:"mode"`
			Path         string `json:"path"`
			Ref          string `json:"ref"`
			Ref2         string `json:"ref2"`
			StatOnly     bool   `json:"stat_only"`
			ContextLines int    `json:"context_lines"`
		}
		if err := jsonutil.UnmarshalInto("diff params", input, &p); err != nil {
			return "", err
		}
		if p.Mode == "" {
			p.Mode = "unstaged"
		}

		// Handle file-to-file comparison separately (no git needed).
		if p.Mode == "files" {
			return diffFiles(p.Path, p.Ref2, defaultDir)
		}

		// Build git diff command.
		args := []string{"diff", "--no-color"}

		// Context lines.
		contextLines := p.ContextLines
		if contextLines < 0 {
			contextLines = 0
		}
		if contextLines > 20 {
			contextLines = 20
		}
		if contextLines != 3 {
			args = append(args, fmt.Sprintf("-U%d", contextLines))
		}

		if p.StatOnly {
			args = append(args, "--stat")
		}

		switch p.Mode {
		case "staged":
			args = append(args, "--cached")
		case "unstaged":
			// default git diff (working tree vs index)
		case "all":
			args = append(args, "HEAD")
		case "commit":
			ref := p.Ref
			if ref == "" {
				ref = "HEAD"
			}
			// Show the diff introduced by a specific commit.
			args = []string{"show", "--no-color", "--format=commit %H%nAuthor: %an <%ae>%nDate: %ad%nSubject: %s%n", ref}
			if p.StatOnly {
				args = append(args, "--stat")
			}
		case "branch":
			if p.Ref == "" {
				return "", fmt.Errorf("ref is required for branch mode (base branch)")
			}
			ref2 := p.Ref2
			if ref2 == "" {
				ref2 = "HEAD"
			}
			args = append(args, fmt.Sprintf("%s...%s", p.Ref, ref2))
		default:
			return "", fmt.Errorf("unknown diff mode: %q", p.Mode)
		}

		// Add path filter.
		if p.Path != "" && p.Mode != "commit" {
			args = append(args, "--", p.Path)
		}

		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = defaultDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			// git diff exits 1 when there are differences in some modes,
			// but that's not an error. Only report actual failures.
			if len(out) > 0 {
				return string(out), nil
			}
			return "", fmt.Errorf("git diff failed: %w", err)
		}

		result := strings.TrimSpace(string(out))
		if result == "" {
			return "No differences found.", nil
		}

		// Truncate very large diffs to avoid blowing up context.
		const maxDiffLen = 64000
		if len(result) > maxDiffLen {
			result = result[:maxDiffLen] + fmt.Sprintf("\n\n[... truncated, %d total chars. Use path filter or stat_only to narrow.]", len(result))
		}

		return result, nil
	}
}

// diffFiles compares two files using the system diff command.
func diffFiles(file1, file2, defaultDir string) (string, error) {
	if file1 == "" || file2 == "" {
		return "", fmt.Errorf("files mode requires path (first file) and ref2 (second file)")
	}

	path1 := ResolvePath(file1, defaultDir)
	path2 := ResolvePath(file2, defaultDir)

	cmd := exec.Command("diff", "-u", "--color=never", path1, path2)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// diff exits 1 when files differ — that's expected.
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
			return string(out), nil
		}
		if len(out) > 0 {
			return string(out), nil
		}
		return "", fmt.Errorf("diff failed: %w", err)
	}

	result := strings.TrimSpace(string(out))
	if result == "" {
		return "Files are identical.", nil
	}
	return result, nil
}
