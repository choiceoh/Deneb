package chat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
			"function": map[string]any{
				"type":        "string",
				"description": "Read only this function/method/type. For .go files uses AST; for others uses regex. Overrides offset/limit",
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
			Function string `json:"function"`
		}
		if err := jsonutil.UnmarshalInto("read params", input, &p); err != nil {
			return "", err
		}
		if p.FilePath == "" {
			return "", fmt.Errorf("file_path is required")
		}

		path := resolvePath(p.FilePath, defaultDir)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("failed to read file: %w", err)
		}

		// Function extraction mode.
		if p.Function != "" {
			return readFunction(path, p.FilePath, string(data), p.Function)
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

// readFunction extracts a specific function/type from a file.
// For .go files, uses go/ast for precise extraction.
// For other files, uses regex heuristics.
func readFunction(path, displayPath, content, funcName string) (string, error) {
	lines := strings.Split(content, "\n")

	if strings.HasSuffix(path, ".go") {
		return readGoFunction(path, displayPath, lines, funcName)
	}

	// Regex fallback for non-Go files.
	return readFunctionRegex(displayPath, lines, funcName)
}

// readGoFunction uses go/ast to find and extract a function or type declaration.
func readGoFunction(path, displayPath string, lines []string, funcName string) (string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		// Fall back to regex if parsing fails.
		return readFunctionRegex(displayPath, lines, funcName)
	}

	// Search all declarations.
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !strings.EqualFold(d.Name.Name, funcName) {
				continue
			}
			start := fset.Position(d.Pos()).Line
			end := fset.Position(d.End()).Line

			// Include doc comments.
			if d.Doc != nil {
				docStart := fset.Position(d.Doc.Pos()).Line
				if docStart < start {
					start = docStart
				}
			}
			return formatFunctionLines(displayPath, lines, start, end, funcName)

		case *ast.GenDecl:
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !strings.EqualFold(ts.Name.Name, funcName) {
					continue
				}
				start := fset.Position(d.Pos()).Line
				end := fset.Position(d.End()).Line
				if d.Doc != nil {
					docStart := fset.Position(d.Doc.Pos()).Line
					if docStart < start {
						start = docStart
					}
				}
				return formatFunctionLines(displayPath, lines, start, end, funcName)
			}
		}
	}

	return "", fmt.Errorf("symbol %q not found in %s", funcName, displayPath)
}

// readFunctionRegex uses regex to find a function definition and extract it.
func readFunctionRegex(displayPath string, lines []string, funcName string) (string, error) {
	// Patterns for common languages.
	patterns := []string{
		`(?i)^(\s*)(pub\s+)?(async\s+)?fn\s+` + regexp.QuoteMeta(funcName),          // Rust
		`(?i)^(\s*)(export\s+)?(async\s+)?function\s+` + regexp.QuoteMeta(funcName), // JS/TS
		`(?i)^(\s*)def\s+` + regexp.QuoteMeta(funcName),                             // Python
		`(?i)^(\s*)(pub\s+)?struct\s+` + regexp.QuoteMeta(funcName),                 // Rust struct
		`(?i)^(\s*)class\s+` + regexp.QuoteMeta(funcName),                           // Python/JS class
	}

	for _, pat := range patterns {
		re := regexp.MustCompile(pat)
		for i, line := range lines {
			if re.MatchString(line) {
				// Find the end of the block by tracking brace depth.
				end := findBlockEnd(lines, i)
				return formatFunctionLines(displayPath, lines, i+1, end+1, funcName)
			}
		}
	}

	return "", fmt.Errorf("symbol %q not found in %s", funcName, displayPath)
}

// findBlockEnd finds the end of a code block starting at startIdx by tracking brace depth.
func findBlockEnd(lines []string, startIdx int) int {
	depth := 0
	started := false

	for i := startIdx; i < len(lines); i++ {
		for _, ch := range lines[i] {
			if ch == '{' || ch == '(' {
				depth++
				started = true
			} else if ch == '}' || ch == ')' {
				depth--
			}
		}
		if started && depth <= 0 {
			return i
		}
		// Safety: don't scan more than 500 lines.
		if i-startIdx > 500 {
			return i
		}
	}
	// If no braces found, return a reasonable block (30 lines).
	end := startIdx + 30
	if end >= len(lines) {
		end = len(lines) - 1
	}
	return end
}

func formatFunctionLines(displayPath string, lines []string, start, end int, funcName string) (string, error) {
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "[%s: %s (lines %d-%d)]\n", displayPath, funcName, start, end)
	for i := start - 1; i < end; i++ {
		fmt.Fprintf(&sb, "%6d\t%s\n", i+1, lines[i])
	}
	return sb.String(), nil
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
		if err := jsonutil.UnmarshalInto("write params", input, &p); err != nil {
			return "", err
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
				"description": "The text to replace (must be unique unless replace_all is true). When regex=true, treated as a regex pattern",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "The text to replace it with. When regex=true, supports $1/$2 backreferences",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "Replace all occurrences instead of requiring a unique match (default: false)",
				"default":     false,
			},
			"regex": map[string]any{
				"type":        "boolean",
				"description": "Treat old_string as a regex pattern (default: false)",
				"default":     false,
			},
			"line": map[string]any{
				"type":        "number",
				"description": "Replace at a specific line number (1-based). When set, old_string matches only on this line",
				"minimum":     1,
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
			Regex      bool   `json:"regex"`
			Line       int    `json:"line"`
		}
		if err := jsonutil.UnmarshalInto("edit params", input, &p); err != nil {
			return "", err
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

		// Regex-based replacement.
		if p.Regex {
			return editWithRegex(path, p.FilePath, content, p.OldString, p.NewString, p.ReplaceAll)
		}

		// Line-targeted replacement.
		if p.Line > 0 {
			return editAtLine(path, p.FilePath, content, p.OldString, p.NewString, p.Line)
		}

		count := strings.Count(content, p.OldString)
		if count == 0 {
			hint := editFuzzyHint(content, p.OldString)
			return "", fmt.Errorf("old_string not found in file%s", hint)
		}
		if count > 1 && !p.ReplaceAll {
			return "", fmt.Errorf("old_string is not unique in file (%d occurrences). Use replace_all=true to replace all, or use line= to target a specific line", count)
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

// editWithRegex performs regex-based search and replace.
func editWithRegex(path, displayPath, content, pattern, replacement string, replaceAll bool) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex pattern: %w", err)
	}

	matches := re.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return "", fmt.Errorf("regex pattern not found in file")
	}
	if len(matches) > 1 && !replaceAll {
		return "", fmt.Errorf("regex pattern matches %d times. Use replace_all=true to replace all", len(matches))
	}

	var newContent string
	if replaceAll {
		newContent = re.ReplaceAllString(content, replacement)
	} else {
		// Replace only the first match.
		loc := matches[0]
		newContent = content[:loc[0]] + re.ReplaceAllString(content[loc[0]:loc[1]], replacement) + content[loc[1]:]
	}

	if err := os.WriteFile(path, []byte(newContent), 0o644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	return fmt.Sprintf("Successfully edited %s (regex, %d matches)", displayPath, len(matches)), nil
}

// editAtLine performs replacement only on a specific line.
func editAtLine(path, displayPath, content, oldStr, newStr string, lineNum int) (string, error) {
	lines := strings.Split(content, "\n")
	if lineNum > len(lines) {
		return "", fmt.Errorf("line %d out of range (file has %d lines)", lineNum, len(lines))
	}

	idx := lineNum - 1
	if !strings.Contains(lines[idx], oldStr) {
		return "", fmt.Errorf("old_string not found on line %d: %q", lineNum, lines[idx])
	}

	lines[idx] = strings.Replace(lines[idx], oldStr, newStr, 1)
	newContent := strings.Join(lines, "\n")

	if err := os.WriteFile(path, []byte(newContent), 0o644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	return fmt.Sprintf("Successfully edited %s (line %d)", displayPath, lineNum), nil
}

// editFuzzyHint provides a hint when old_string is not found.
func editFuzzyHint(content, oldStr string) string {
	// Check if it's a whitespace issue.
	normalized := strings.Join(strings.Fields(oldStr), " ")
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		normalizedLine := strings.Join(strings.Fields(line), " ")
		if strings.Contains(normalizedLine, normalized) {
			return fmt.Sprintf(". Possible whitespace mismatch on line %d", i+1)
		}
	}

	// Check first line of old_string for partial match.
	firstLine := strings.Split(oldStr, "\n")[0]
	if firstLine != "" {
		for i, line := range lines {
			if strings.Contains(line, strings.TrimSpace(firstLine)) {
				return fmt.Sprintf(". Similar text found on line %d — check for whitespace or trailing characters", i+1)
			}
		}
	}

	return ""
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
			"before": map[string]any{
				"type":        "number",
				"description": "Lines of context before each match (overrides contextLines for before)",
				"minimum":     0,
				"maximum":     10,
			},
			"after": map[string]any{
				"type":        "number",
				"description": "Lines of context after each match (overrides contextLines for after)",
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
			"multiline": map[string]any{
				"type":        "boolean",
				"description": "Enable multiline matching (patterns can span lines, . matches newlines)",
				"default":     false,
			},
			"mode": map[string]any{
				"type":        "string",
				"description": "Output mode: content (default, matching lines), files_only (file paths only), count (match counts per file)",
				"enum":        []string{"content", "files_only", "count"},
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
		if err := jsonutil.UnmarshalInto("find params", input, &p); err != nil {
			return "", err
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
		if err := jsonutil.UnmarshalInto("ls params", input, &p); err != nil {
			return "", err
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
