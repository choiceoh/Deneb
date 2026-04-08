package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolGrep returns a tool that searches file contents using ripgrep with defaultDir as
// the base search path when no explicit path is provided.
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

		// Use ripgrep directly. Do not fall back to slower system grep.
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
			for _, glob := range splitGlobs(p.Include) {
				args = append(args, "--glob", glob)
			}
		}
		if p.FileType != "" {
			args = append(args, "--type", normalizeFileType(p.FileType))
		}
		// Use -e to avoid flag confusion when pattern starts with '-'.
		args = append(args, "-e", p.Pattern, "--", searchPath)

		// Build bare-minimum args for last-resort fallback.
		bareMinArgs := []string{"-F", "-n", fmt.Sprintf("--max-count=%d", maxResults)}
		if p.Mode == "files_only" {
			bareMinArgs = append(bareMinArgs, "-l")
		} else if p.Mode == "count" {
			bareMinArgs = append(bareMinArgs, "-c")
		}
		bareMinArgs = append(bareMinArgs, "-e", p.Pattern, "--", searchPath)

		stdout, err := rgWithFallbacks(ctx, args, bareMinArgs, p.FileType)
		if err != nil {
			return "", err
		}
		if stdout == nil {
			return "No matches found.", nil
		}
		return formatRgOutput(string(stdout), p.Mode), nil
	}
}

// formatRgOutput groups content-mode output by file, or returns raw output for other modes.
func formatRgOutput(raw, mode string) string {
	if mode == "" || mode == "content" {
		return groupGrepOutput(raw)
	}
	return raw
}

// groupGrepOutput groups ripgrep content-mode output by file path.
// Input: "file:42:match\nfile:89:match2\n" → "file:\n  42: match\n  89: match2\n"
func groupGrepOutput(raw string) string {
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	if len(lines) <= 1 {
		return raw
	}

	var sb strings.Builder
	currentFile := ""
	for _, line := range lines {
		if line == "--" {
			continue
		}
		file, lineNum, content, ok := parseGrepLine(line)
		if !ok {
			sb.WriteString(line)
			sb.WriteByte('\n')
			continue
		}

		if file != currentFile {
			if currentFile != "" {
				sb.WriteByte('\n')
			}
			sb.WriteString(file)
			sb.WriteString(":\n")
			currentFile = file
		}
		sb.WriteString("  ")
		sb.WriteString(lineNum)
		sb.WriteString(": ")
		sb.WriteString(content)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// parseGrepLine parses "file:linenum:content" or "file-linenum-content" format.
// Returns (file, linenum, content, ok).
func parseGrepLine(line string) (file, lineNum, content string, ok bool) {
	// Try ":" separator first (match lines), then "-" (context lines).
	for _, sep := range []byte{':', '-'} {
		// Find first separator.
		idx := strings.IndexByte(line, sep)
		if idx <= 0 {
			continue
		}
		file := line[:idx]
		rest := line[idx+1:]
		// Find second separator of the same type (linenum:content).
		idx2 := strings.IndexByte(rest, sep)
		if idx2 <= 0 {
			continue
		}
		lineNum := rest[:idx2]
		// Validate lineNum is all digits.
		allDigits := true
		for _, c := range lineNum {
			if c < '0' || c > '9' {
				allDigits = false
				break
			}
		}
		if !allDigits || lineNum == "" {
			continue
		}
		return file, lineNum, rest[idx2+1:], true
	}
	return "", "", "", false
}

// clampInt clamps v to [lo, hi].
func clampInt(v, lo, hi int) int { //nolint:unparam // lo is always 0 at current call sites but kept generic
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
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
		var err error

		// Prefer fd for fast glob/path matching with sane defaults.
		if _, ok := firstAvailableBinary("fd", "fdfind"); ok {
			fdMatches, err := findWithFD(ctx, searchDir, p.Pattern, p.ShowHidden, maxResults)
			if err != nil {
				return "", err
			}
			if len(fdMatches) == 0 {
				return "No files found matching pattern.", nil
			}
			return strings.Join(fdMatches, "\n"), nil
		}

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
		err = filepath.WalkDir(searchDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil //nolint:nilerr // skip inaccessible entries in walk
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

func findWithFD(ctx context.Context, dir, pattern string, showHidden bool, maxResults int) ([]string, error) {
	bin, ok := firstAvailableBinary("fd", "fdfind")
	if !ok {
		return nil, fmt.Errorf("fd unavailable")
	}

	args := []string{"--glob", "--type", "f"}
	if showHidden {
		args = append(args, "--hidden")
	}
	args = append(args, pattern, ".")

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		// fd exit code 1 means no matches.
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("fd failed: %s", strings.TrimSpace(string(out)))
	}

	lines := nonEmptyCommandLines(string(out))
	if len(lines) > maxResults {
		lines = lines[:maxResults]
	}
	return lines, nil
}

// findWithRipgrep uses `rg --files --glob` to find files matching ** patterns.
func findWithRipgrep(ctx context.Context, dir, pattern string, showHidden bool, maxResults int) ([]string, error) {
	args := []string{"--files", "--glob", pattern}
	if showHidden {
		args = append(args, "--hidden")
	}
	args = append(args, dir)

	stdout, _, err := runRg(ctx, args)
	if err != nil {
		// rg exit code 1 means no matches.
		if rgExitCode(err) == 1 {
			return nil, nil
		}
		// If stdout has file paths despite the error, use them.
		if len(stdout) == 0 {
			return nil, err
		}
	}

	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
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

// normalizeFileType maps common LLM aliases to ripgrep --type values.
// LLMs frequently emit full language names ("golang", "python") instead of
// the short names ripgrep expects ("go", "py").
func normalizeFileType(ft string) string {
	aliases := map[string]string{
		"golang":      "go",
		"python":      "py",
		"javascript":  "js",
		"typescript":  "ts",
		"csharp":      "cs",
		"c#":          "cs",
		"c++":         "cpp",
		"cplusplus":   "cpp",
		"objective-c": "objc",
		"proto":       "protobuf",
		"shellscript": "sh",
		"shell":       "sh",
		"bash":        "sh",
		"yml":         "yaml",
		"dockerfile":  "docker",
		"makefile":    "make",
	}
	lower := strings.ToLower(strings.TrimSpace(ft))
	if mapped, ok := aliases[lower]; ok {
		return mapped
	}
	return lower
}

// stripRgFlag removes a flag and its value from a ripgrep argument list.
// For example, stripRgFlag(args, "--type") removes both "--type" and the
// following value argument.
func stripRgFlag(args []string, flag string) []string { //nolint:unparam // flag is always "--type" now but generalizable
	var result []string
	skip := false
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if a == flag {
			skip = true // skip this flag and the next arg (its value)
			continue
		}
		result = append(result, a)
	}
	return result
}

// splitGlobs splits a comma-separated glob string into individual patterns.
// LLMs often pass "*.go,*.rs" instead of separate --glob args, so we split
// on commas unless the value already uses brace expansion (e.g., "*.{go,rs}").
func splitGlobs(include string) []string {
	// Already using brace expansion — pass through as-is.
	if strings.Contains(include, "{") {
		return []string{include}
	}
	parts := strings.Split(include, ",")
	var globs []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			globs = append(globs, p)
		}
	}
	if len(globs) == 0 {
		return []string{include}
	}
	return globs
}

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

// rgWithFallbacks runs ripgrep with automatic retry on failure.
// Returns (output, nil) on success, (nil, nil) on no matches, or (nil, err) on failure.
// primaryArgs are the initial rg arguments. bareMinArgs are used as last resort.
// fileType triggers an extra retry that strips --type when non-empty.
func rgWithFallbacks(ctx context.Context, primaryArgs, bareMinArgs []string, fileType string) ([]byte, error) {
	out, _, err := runRg(ctx, primaryArgs)
	if err == nil {
		return out, nil
	}

	exitCode := rgExitCode(err)
	if exitCode == 1 {
		return nil, nil
	}

	// Stdout may contain valid matches despite the error (e.g. partial file-access errors).
	if len(out) > 0 && hasGrepMatches(out) {
		return out, nil
	}

	if exitCode == 2 {
		// Retry 1: treat pattern as literal string (-F).
		fixedArgs := append([]string{"-F"}, primaryArgs...)
		if retryOut, _, retryErr := runRg(ctx, fixedArgs); retryErr == nil {
			return retryOut, nil
		} else if rgExitCode(retryErr) == 1 {
			return nil, nil
		} else if fileType != "" {
			// Retry 2: strip unrecognized --type, keep -F.
			bareArgs := stripRgFlag(fixedArgs, "--type")
			if bareOut, _, bareErr := runRg(ctx, bareArgs); bareErr == nil {
				return bareOut, nil
			} else if rgExitCode(bareErr) == 1 {
				return nil, nil
			}
		}
	}

	// Last resort: bare minimum search.
	if bareMinOut, _, bareMinErr := runRg(ctx, bareMinArgs); bareMinErr == nil {
		return bareMinOut, nil
	} else if rgExitCode(bareMinErr) == 1 {
		return nil, nil
	}

	return nil, fmt.Errorf("grep failed (rg %s): %s", strings.Join(primaryArgs, " "), strings.TrimSpace(string(out)))
}

// runRg executes ripgrep with separate stdout/stderr capture.
// Returns (stdout, stderr, error). Using separate pipes prevents stderr
// warnings (permission denied, binary file skipped) from contaminating
// valid match output on stdout.
func runRg(ctx context.Context, args []string) (stdout, stderr []byte, err error) { //nolint:unparam // stderr kept for diagnostic use
	cmd := exec.CommandContext(ctx, "rg", args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// rgExitCode extracts the exit code from a command error.
// Returns -1 if the process state is unavailable (e.g. binary not found).
func rgExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if ok := errors.As(err, &exitErr); ok {
		return exitErr.ExitCode()
	}
	return -1
}

// hasGrepMatches returns true if the rg stdout output contains at least one
// line that looks like a match (file:linenum:content or file:linenum-content).
// This is used to salvage partial results when rg exits with a non-zero code
// due to file-access errors but still found matches in accessible files.
func hasGrepMatches(out []byte) bool {
	// Quick check: a valid match line must have at least "a:1:x" (5 chars).
	if len(out) < 5 {
		return false
	}
	// Check the first few lines for the file:linenum:content pattern.
	lines := bytes.SplitN(out, []byte("\n"), 5)
	for _, line := range lines {
		if _, _, _, ok := parseGrepLine(string(line)); ok {
			return true
		}
	}
	return false
}
