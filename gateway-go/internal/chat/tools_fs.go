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
