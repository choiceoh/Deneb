package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// --- Code analysis tool ---
// Provides AST-based code analysis for Go (go/ast) and regex-based analysis for Rust.

func ToolAnalyze(defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p analyzeParams
		if err := jsonutil.UnmarshalInto("analyze params", input, &p); err != nil {
			return "", err
		}

		var out string
		var err error
		switch p.Action {
		case "outline":
			out, err = analyzeOutline(p, defaultDir)
		case "symbols":
			out, err = analyzeSymbols(ctx, p, defaultDir)
		case "references":
			out, err = analyzeReferences(ctx, p, defaultDir)
		case "imports":
			out, err = analyzeImports(p, defaultDir)
		case "signature":
			out, err = analyzeSignature(p, defaultDir)
		default:
			return "", fmt.Errorf("unknown analyze action: %q", p.Action)
		}
		if err != nil {
			return "", err
		}
		return TruncateForLLM(out), nil
	}
}

type analyzeParams struct {
	Action  string `json:"action"`
	File    string `json:"file"`
	Path    string `json:"path"`
	Query   string `json:"query"`
	Kind    string `json:"kind"`
	Symbol  string `json:"symbol"`
	Reverse bool   `json:"reverse"`
}

// --- Outline ---

func analyzeOutline(p analyzeParams, defaultDir string) (string, error) {
	if p.File == "" {
		return "", fmt.Errorf("file is required for outline")
	}
	path := ResolvePath(p.File, defaultDir)

	if isGoFile(path) {
		return outlineGo(path, p.File)
	}
	if isRustFile(path) {
		return outlineRust(path, p.File)
	}
	return outlineGeneric(path, p.File)
}

func outlineGo(path, displayPath string) (string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("failed to parse Go file: %w", err)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s\n\n", displayPath)

	// Package.
	fmt.Fprintf(&sb, "package %s\n\n", node.Name.Name)

	// Imports.
	if len(node.Imports) > 0 {
		fmt.Fprintf(&sb, "### Imports (%d)\n", len(node.Imports))
		for _, imp := range node.Imports {
			name := ""
			if imp.Name != nil {
				name = imp.Name.Name + " "
			}
			fmt.Fprintf(&sb, "  %s%s\n", name, imp.Path.Value)
		}
		sb.WriteString("\n")
	}

	// Top-level declarations.
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			line := fset.Position(d.Pos()).Line
			sig := formatGoFuncSignature(d)
			fmt.Fprintf(&sb, "func %s  :%d\n", sig, line)

		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					line := fset.Position(s.Pos()).Line
					kind := goTypeKind(s.Type)
					fmt.Fprintf(&sb, "type %s (%s)  :%d\n", s.Name.Name, kind, line)

					// List methods for structs/interfaces inline.
					if st, ok := s.Type.(*ast.InterfaceType); ok && st.Methods != nil {
						for _, m := range st.Methods.List {
							if len(m.Names) > 0 {
								mLine := fset.Position(m.Pos()).Line
								fmt.Fprintf(&sb, "  .%s  :%d\n", m.Names[0].Name, mLine)
							}
						}
					}

				case *ast.ValueSpec:
					line := fset.Position(s.Pos()).Line
					kind := "var"
					if d.Tok == token.CONST {
						kind = "const"
					}
					for _, name := range s.Names {
						fmt.Fprintf(&sb, "%s %s  :%d\n", kind, name.Name, line)
					}
				}
			}
		}
	}

	return sb.String(), nil
}

// Rust outline patterns.
var (
	rustFnPattern     = regexp.MustCompile(`(?m)^(\s*)(pub\s+)?(async\s+)?fn\s+(\w+)`)
	rustStructPattern = regexp.MustCompile(`(?m)^(\s*)(pub\s+)?struct\s+(\w+)`)
	rustEnumPattern   = regexp.MustCompile(`(?m)^(\s*)(pub\s+)?enum\s+(\w+)`)
	rustTraitPattern  = regexp.MustCompile(`(?m)^(\s*)(pub\s+)?trait\s+(\w+)`)
	rustImplPattern   = regexp.MustCompile(`(?m)^(\s*)impl(?:<[^>]*>)?\s+(\w+)`)
	rustModPattern    = regexp.MustCompile(`(?m)^(\s*)(pub\s+)?mod\s+(\w+)`)
	rustUsePattern    = regexp.MustCompile(`(?m)^(\s*)(pub\s+)?use\s+(.+);`)
	rustConstPattern  = regexp.MustCompile(`(?m)^(\s*)(pub\s+)?const\s+(\w+)`)
)

func outlineRust(path, displayPath string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	content := string(data)
	lines := strings.Split(content, "\n")

	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s\n\n", displayPath)

	type entry struct {
		kind string
		name string
		line int
	}
	var entries []entry

	addMatches := func(re *regexp.Regexp, kind string, nameIdx int) {
		for _, loc := range re.FindAllStringIndex(content, -1) {
			lineNum := strings.Count(content[:loc[0]], "\n") + 1
			match := re.FindStringSubmatch(content[loc[0]:loc[1]])
			if match != nil && nameIdx < len(match) {
				// Skip items inside function bodies (indented).
				if lineNum <= len(lines) {
					lineContent := lines[lineNum-1]
					indent := len(lineContent) - len(strings.TrimLeft(lineContent, " \t"))
					// Top-level items have 0 indent; impl items have 4.
					if indent <= 4 {
						entries = append(entries, entry{kind: kind, name: match[nameIdx], line: lineNum})
					}
				}
			}
		}
	}

	addMatches(rustModPattern, "mod", 3)
	addMatches(rustStructPattern, "struct", 3)
	addMatches(rustEnumPattern, "enum", 3)
	addMatches(rustTraitPattern, "trait", 3)
	addMatches(rustImplPattern, "impl", 2)
	addMatches(rustFnPattern, "fn", 4)
	addMatches(rustConstPattern, "const", 3)

	// Sort by line number.
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].line < entries[i].line {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	for _, e := range entries {
		fmt.Fprintf(&sb, "%s %s  :%d\n", e.kind, e.name, e.line)
	}

	if len(entries) == 0 {
		sb.WriteString("(no symbols found)\n")
	}
	return sb.String(), nil
}

func outlineGeneric(path, displayPath string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Generic patterns for common languages.
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?m)^(\s*)(export\s+)?(async\s+)?function\s+(\w+)`), // JS/TS function
		regexp.MustCompile(`(?m)^(\s*)(export\s+)?(default\s+)?class\s+(\w+)`),  // JS/TS class
		regexp.MustCompile(`(?m)^(\s*)def\s+(\w+)`),                             // Python
		regexp.MustCompile(`(?m)^(\s*)class\s+(\w+)`),                           // Python class
	}

	content := string(data)
	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s\n\n", displayPath)

	found := 0
	for _, re := range patterns {
		for _, loc := range re.FindAllStringIndex(content, -1) {
			lineNum := strings.Count(content[:loc[0]], "\n") + 1
			line := strings.TrimSpace(strings.Split(content[loc[0]:], "\n")[0])
			// Truncate long lines.
			if len(line) > 120 {
				line = line[:117] + "..."
			}
			fmt.Fprintf(&sb, "%s  :%d\n", line, lineNum)
			found++
		}
	}

	if found == 0 {
		sb.WriteString("(no symbols found — unsupported language or empty file)\n")
	}
	return sb.String(), nil
}

// --- Symbols ---

func analyzeSymbols(ctx context.Context, p analyzeParams, defaultDir string) (string, error) {
	if p.Query == "" {
		return "", fmt.Errorf("query is required for symbols action")
	}

	searchDir := defaultDir
	if p.Path != "" {
		searchDir = ResolvePath(p.Path, defaultDir)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Symbol definitions matching %q\n\n", p.Query)

	const maxResults = 50
	found := 0

	err := filepath.WalkDir(searchDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if found >= maxResults {
			return filepath.SkipAll
		}

		rel, _ := filepath.Rel(defaultDir, path)

		if isGoFile(path) {
			results := searchGoSymbols(path, p.Query, p.Kind)
			for _, r := range results {
				fmt.Fprintf(&sb, "%s:%d  %s  %s\n", rel, r.line, r.kind, r.signature)
				found++
			}
		} else if isRustFile(path) {
			results := searchRustSymbols(path, p.Query, p.Kind)
			for _, r := range results {
				fmt.Fprintf(&sb, "%s:%d  %s  %s\n", rel, r.line, r.kind, r.name)
				found++
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("symbol search failed: %w", err)
	}

	if found == 0 {
		return fmt.Sprintf("No symbol definitions found for %q", p.Query), nil
	}
	return sb.String(), nil
}

type symbolResult struct {
	kind      string
	name      string
	signature string
	line      int
}

func searchGoSymbols(path, query, kindFilter string) []symbolResult {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil
	}

	var results []symbolResult
	ast.Inspect(node, func(n ast.Node) bool {
		switch d := n.(type) {
		case *ast.FuncDecl:
			if !matchesQuery(d.Name.Name, query) {
				return true
			}
			kind := "func"
			if d.Recv != nil {
				kind = "method"
			}
			if kindFilter != "" && kindFilter != kind {
				return true
			}
			sig := formatGoFuncSignature(d)
			results = append(results, symbolResult{
				kind:      kind,
				name:      d.Name.Name,
				signature: sig,
				line:      fset.Position(d.Pos()).Line,
			})
		case *ast.TypeSpec:
			if !matchesQuery(d.Name.Name, query) {
				return true
			}
			kind := goTypeKind(d.Type)
			if kindFilter != "" && kindFilter != kind && kindFilter != "type" {
				return true
			}
			results = append(results, symbolResult{
				kind: kind,
				name: d.Name.Name,
				line: fset.Position(d.Pos()).Line,
			})
		case *ast.ValueSpec:
			for _, name := range d.Names {
				if !matchesQuery(name.Name, query) {
					continue
				}
				// Determine var or const from parent GenDecl (approximate).
				kind := "var"
				if kindFilter != "" && kindFilter != "var" && kindFilter != "const" {
					continue
				}
				results = append(results, symbolResult{
					kind: kind,
					name: name.Name,
					line: fset.Position(name.Pos()).Line,
				})
			}
		}
		return true
	})
	return results
}

func searchRustSymbols(path, query, kindFilter string) []symbolResult {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := string(data)
	var results []symbolResult

	type patternDef struct {
		re      *regexp.Regexp
		kind    string
		nameIdx int
	}
	patterns := []patternDef{
		{rustFnPattern, "func", 4},
		{rustStructPattern, "struct", 3},
		{rustEnumPattern, "enum", 3},
		{rustTraitPattern, "trait", 3},
		{rustImplPattern, "impl", 2},
		{rustConstPattern, "const", 3},
	}

	for _, pd := range patterns {
		if kindFilter != "" && kindFilter != pd.kind && kindFilter != "type" {
			continue
		}
		for _, loc := range pd.re.FindAllStringIndex(content, -1) {
			match := pd.re.FindStringSubmatch(content[loc[0]:loc[1]])
			if match == nil || pd.nameIdx >= len(match) {
				continue
			}
			name := match[pd.nameIdx]
			if !matchesQuery(name, query) {
				continue
			}
			lineNum := strings.Count(content[:loc[0]], "\n") + 1
			results = append(results, symbolResult{
				kind: pd.kind,
				name: name,
				line: lineNum,
			})
		}
	}
	return results
}
