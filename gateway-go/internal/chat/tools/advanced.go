package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ---------------------------------------------------------------------------
// batch_read — Read multiple files in one call
// ---------------------------------------------------------------------------

// ToolBatchRead reads up to 20 files in a single call.
// Each file supports the same options as the read tool (offset, limit, function).
// Per-file errors are reported inline without aborting the entire batch.
func ToolBatchRead(defaultDir string) ToolFunc {
	readFn := ToolRead(defaultDir)

	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Files []struct {
				FilePath string `json:"file_path"`
				Offset   int    `json:"offset"`
				Limit    int    `json:"limit"`
				Function string `json:"function"`
			} `json:"files"`
		}
		if err := jsonutil.UnmarshalInto("batch_read params", input, &p); err != nil {
			return "", err
		}
		if len(p.Files) == 0 {
			return "", fmt.Errorf("files is required and must not be empty")
		}

		var sb strings.Builder
		successCount := 0

		for i, f := range p.Files {
			if i > 0 {
				sb.WriteString("\n---\n\n")
			}

			// Marshal individual file request to reuse the read tool.
			fileInput, _ := json.Marshal(map[string]any{
				"file_path": f.FilePath,
				"offset":    f.Offset,
				"limit":     f.Limit,
				"function":  f.Function,
			})

			result, err := readFn(ctx, fileInput)
			if err != nil {
				fmt.Fprintf(&sb, "[Error reading %s: %s]\n", f.FilePath, err.Error())
				continue
			}

			sb.WriteString(result)
			successCount++
		}

		fmt.Fprintf(&sb, "\n---\n[batch_read: %d/%d files read successfully]\n", successCount, len(p.Files))
		return sb.String(), nil
	}
}

// ---------------------------------------------------------------------------
// search_and_read — grep + auto-read matching files
// ---------------------------------------------------------------------------

// ToolSearchAndRead combines grep and read: searches for a pattern, then
// automatically reads the matching files with context around match locations.
func ToolSearchAndRead(defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Pattern      string `json:"pattern"`
			Path         string `json:"path"`
			Include      string `json:"include"`
			FileType     string `json:"fileType"`
			ContextLines int    `json:"context_lines"`
			MaxFiles     int    `json:"max_files"`
		}
		if err := jsonutil.UnmarshalInto("search_and_read params", input, &p); err != nil {
			return "", err
		}
		if p.Pattern == "" {
			return "", fmt.Errorf("pattern is required")
		}

		contextLines := p.ContextLines
		if contextLines <= 0 {
			contextLines = 10
		}
		if contextLines > 50 {
			contextLines = 50
		}
		maxFiles := p.MaxFiles
		if maxFiles <= 0 {
			maxFiles = 5
		}
		if maxFiles > 20 {
			maxFiles = 20
		}

		searchPath := defaultDir
		if p.Path != "" {
			searchPath = ResolvePath(p.Path, defaultDir)
		}

		// Step 1: Run ripgrep to find matches with file:line format.
		args := []string{"-n", "--max-count=20", "--no-heading", p.Pattern}
		if p.Include != "" {
			args = append(args, "--glob", p.Include)
		}
		if p.FileType != "" {
			args = append(args, "--type", p.FileType)
		}
		args = append(args, searchPath)

		cmd := exec.CommandContext(ctx, "rg", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
				return "No matches found.", nil
			}
			return "", fmt.Errorf("grep failed: %s", strings.TrimSpace(string(out)))
		}

		// Step 2: Parse results into file → line numbers map.
		type fileMatch struct {
			path  string
			lines []int
		}
		fileMap := make(map[string]*fileMatch)
		var fileOrder []string

		lineRe := regexp.MustCompile(`^(.+?):(\d+):`)
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			m := lineRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			filePath := m[1]
			lineNum, _ := strconv.Atoi(m[2])

			if _, exists := fileMap[filePath]; !exists {
				fileMap[filePath] = &fileMatch{path: filePath}
				fileOrder = append(fileOrder, filePath)
			}
			fileMap[filePath].lines = append(fileMap[filePath].lines, lineNum)
		}

		if len(fileOrder) == 0 {
			return "No matches found.", nil
		}

		// Step 3: For each file (up to max_files), read context around matches.
		var sb strings.Builder
		fmt.Fprintf(&sb, "[search_and_read: pattern=%q, %d files matched", p.Pattern, len(fileOrder))
		if len(fileOrder) > maxFiles {
			fmt.Fprintf(&sb, ", showing first %d", maxFiles)
		}
		sb.WriteString("]\n")

		filesShown := 0
		for _, filePath := range fileOrder {
			if filesShown >= maxFiles {
				break
			}

			fm := fileMap[filePath]
			data, err := os.ReadFile(filePath)
			if err != nil {
				fmt.Fprintf(&sb, "\n---\n[Error reading %s: %s]\n", filePath, err.Error())
				filesShown++
				continue
			}

			lines := strings.Split(string(data), "\n")
			totalLines := len(lines)

			// Build display path relative to search path.
			displayPath := filePath
			if rel, err := filepath.Rel(defaultDir, filePath); err == nil {
				displayPath = rel
			}

			// Merge overlapping ranges from all match locations.
			ranges := mergeRanges(fm.lines, contextLines, totalLines)

			sb.WriteString("\n---\n")
			fmt.Fprintf(&sb, "[File: %s | %d lines | matches at lines: %v]\n",
				displayPath, totalLines, fm.lines)

			for ri, r := range ranges {
				if ri > 0 {
					sb.WriteString("  ...\n")
				}
				for i := r.start; i <= r.end && i < totalLines; i++ {
					marker := " "
					for _, ml := range fm.lines {
						if i+1 == ml {
							marker = ">"
							break
						}
					}
					fmt.Fprintf(&sb, "%s%6d\t%s\n", marker, i+1, lines[i])
				}
			}

			filesShown++
		}

		if len(fileOrder) > maxFiles {
			fmt.Fprintf(&sb, "\n[... %d more files not shown. Increase max_files to see more.]\n",
				len(fileOrder)-maxFiles)
		}

		return sb.String(), nil
	}
}

type lineRange struct {
	start, end int
}

// mergeRanges builds non-overlapping line ranges around match locations.
func mergeRanges(matchLines []int, context, totalLines int) []lineRange {
	if len(matchLines) == 0 {
		return nil
	}

	sort.Ints(matchLines)

	var ranges []lineRange
	for _, ml := range matchLines {
		// Convert to 0-based index.
		start := ml - 1 - context
		end := ml - 1 + context
		if start < 0 {
			start = 0
		}
		if end >= totalLines {
			end = totalLines - 1
		}

		// Merge with previous range if overlapping.
		if len(ranges) > 0 && start <= ranges[len(ranges)-1].end+1 {
			ranges[len(ranges)-1].end = end
		} else {
			ranges = append(ranges, lineRange{start, end})
		}
	}
	return ranges
}

// ---------------------------------------------------------------------------
// inspect — Deep code inspection (outline + imports + git history)
// ---------------------------------------------------------------------------

// ToolInspect provides deep code inspection by combining analyze, read, and
// git operations into a single tool call.
func ToolInspect(defaultDir string) ToolFunc {
	analyzeFn := ToolAnalyze(defaultDir)

	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			File   string `json:"file"`
			Symbol string `json:"symbol"`
			Depth  string `json:"depth"`
		}
		if err := jsonutil.UnmarshalInto("inspect params", input, &p); err != nil {
			return "", err
		}
		if p.File == "" {
			return "", fmt.Errorf("file is required")
		}

		depth := p.Depth
		if depth == "" {
			depth = "shallow"
		}

		// Auto-promote to symbol depth when symbol is specified.
		if p.Symbol != "" && depth == "shallow" {
			depth = "symbol"
		}

		filePath := ResolvePath(p.File, defaultDir)
		displayPath := p.File
		if rel, err := filepath.Rel(defaultDir, filePath); err == nil {
			displayPath = rel
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "# Inspect: %s", displayPath)
		if p.Symbol != "" {
			fmt.Fprintf(&sb, " :: %s", p.Symbol)
		}
		fmt.Fprintf(&sb, " (depth=%s)\n\n", depth)

		// --- File stats (always) ---
		info, err := os.Stat(filePath)
		if err != nil {
			return "", fmt.Errorf("file not found: %w", err)
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to read file: %w", err)
		}
		lineCount := len(strings.Split(string(data), "\n"))
		fmt.Fprintf(&sb, "## Stats\n- Size: %d bytes\n- Lines: %d\n- Modified: %s\n\n",
			info.Size(), lineCount, info.ModTime().Format("2006-01-02 15:04:05"))

		// --- Outline ---
		outlineInput, _ := json.Marshal(map[string]any{
			"action": "outline",
			"file":   p.File,
		})
		outlineResult, err := analyzeFn(ctx, outlineInput)
		if err != nil {
			fmt.Fprintf(&sb, "## Outline\n[Error: %s]\n\n", err.Error())
		} else {
			fmt.Fprintf(&sb, "## Outline\n%s\n", outlineResult)
		}

		// --- Imports ---
		importsInput, _ := json.Marshal(map[string]any{
			"action": "imports",
			"file":   p.File,
		})
		importsResult, err := analyzeFn(ctx, importsInput)
		if err != nil {
			fmt.Fprintf(&sb, "## Imports\n[Error: %s]\n\n", err.Error())
		} else {
			fmt.Fprintf(&sb, "## Imports\n%s\n", importsResult)
		}

		// --- Deep: git log ---
		if depth == "deep" || depth == "symbol" {
			gitLog := runGitCommand(ctx, defaultDir, "log", "--oneline", "-5", "--", filePath)
			fmt.Fprintf(&sb, "## Recent Git History\n```\n%s```\n\n", gitLog)
		}

		// --- Symbol-specific: definition + references + blame ---
		if depth == "symbol" && p.Symbol != "" {
			// Read the symbol definition.
			readFn := ToolRead(defaultDir)
			readInput, _ := json.Marshal(map[string]any{
				"file_path": p.File,
				"function":  p.Symbol,
			})
			readResult, err := readFn(ctx, readInput)
			if err != nil {
				fmt.Fprintf(&sb, "## Symbol Definition\n[Error: %s]\n\n", err.Error())
			} else {
				fmt.Fprintf(&sb, "## Symbol Definition\n%s\n", readResult)
			}

			// Find references.
			refsInput, _ := json.Marshal(map[string]any{
				"action": "references",
				"symbol": p.Symbol,
				"path":   filepath.Dir(p.File),
			})
			refsResult, err := analyzeFn(ctx, refsInput)
			if err != nil {
				fmt.Fprintf(&sb, "## References\n[Error: %s]\n\n", err.Error())
			} else {
				fmt.Fprintf(&sb, "## References\n%s\n", refsResult)
			}

			// Git blame for the symbol.
			blameOutput := runGitCommand(ctx, defaultDir,
				"log", "--oneline", "-3", "-S", p.Symbol, "--", filePath)
			if blameOutput != "" {
				fmt.Fprintf(&sb, "## Symbol Git History\n```\n%s```\n\n", blameOutput)
			}
		}

		return sb.String(), nil
	}
}

// runGitCommand runs a git command and returns stdout (or error message).
func runGitCommand(ctx context.Context, workDir string, args ...string) string {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("[git error: %s]\n", strings.TrimSpace(string(out)))
	}
	result := string(out)
	if result == "" {
		return "(no output)\n"
	}
	return result
}

// ---------------------------------------------------------------------------
// apply_patch — Apply unified diff patches via git apply
// ---------------------------------------------------------------------------

// ToolApplyPatch applies a unified diff patch using git apply.
// Supports dry_run mode for pre-validation.
func ToolApplyPatch(defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Patch  string `json:"patch"`
			Strip  int    `json:"strip"`
			DryRun bool   `json:"dry_run"`
		}
		if err := jsonutil.UnmarshalInto("apply_patch params", input, &p); err != nil {
			return "", err
		}
		if p.Patch == "" {
			return "", fmt.Errorf("patch is required")
		}
		if patchContainsSymlinkMode(p.Patch) {
			return "", fmt.Errorf("patch apply failed: symlink patches are not allowed")
		}

		strip := p.Strip
		if strip < 0 {
			strip = 1
		}

		// Write patch to temp file.
		tmpFile, err := os.CreateTemp("", "deneb-patch-*.diff")
		if err != nil {
			return "", fmt.Errorf("failed to create temp file: %w", err)
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)

		if _, err := tmpFile.WriteString(p.Patch); err != nil {
			tmpFile.Close()
			return "", fmt.Errorf("failed to write patch: %w", err)
		}
		tmpFile.Close()

		// Build git apply command.
		args := []string{"apply", fmt.Sprintf("-p%d", strip)}
		if p.DryRun {
			args = append(args, "--check")
		}
		args = append(args, "--verbose", tmpPath)

		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = defaultDir
		out, err := cmd.CombinedOutput()
		output := strings.TrimSpace(string(out))

		if err != nil {
			if p.DryRun {
				return fmt.Sprintf("Patch validation FAILED:\n%s", output), nil
			}
			return "", fmt.Errorf("patch apply failed:\n%s", output)
		}

		if p.DryRun {
			if output == "" {
				return "Patch validation OK: patch applies cleanly.", nil
			}
			return fmt.Sprintf("Patch validation OK: patch applies cleanly.\n%s", output), nil
		}

		if output == "" {
			return "Patch applied successfully.", nil
		}
		return fmt.Sprintf("Patch applied successfully.\n%s", output), nil
	}
}

func patchContainsSymlinkMode(patch string) bool {
	// Existing symlink updates are encoded as:
	//   index <old>..<new> 120000
	// so we must block those in addition to new/old mode markers.
	indexSymlinkMode := regexp.MustCompile(`(?m)^index [0-9a-fA-F]+\.\.[0-9a-fA-F]+ 120000$`)

	return strings.Contains(patch, "\nnew file mode 120000") ||
		strings.HasPrefix(patch, "new file mode 120000") ||
		strings.Contains(patch, "\nold mode 120000") ||
		strings.HasPrefix(patch, "old mode 120000") ||
		indexSymlinkMode.MatchString(patch)
}
