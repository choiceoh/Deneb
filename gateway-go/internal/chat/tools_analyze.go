package chat

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
)

// --- Code analysis tool ---
// Provides AST-based code analysis for Go (go/ast) and regex-based analysis for Rust.

func analyzeToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Analysis action",
				"enum":        []string{"outline", "symbols", "references", "imports", "signature"},
			},
			"file": map[string]any{
				"type":        "string",
				"description": "File path for outline/imports/signature analysis",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory path for symbols/references/imports search",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Symbol name to search for (symbols/references/signature)",
			},
			"kind": map[string]any{
				"type":        "string",
				"description": "Filter symbols by kind",
				"enum":        []string{"func", "type", "method", "const", "var", "interface", "struct"},
			},
			"symbol": map[string]any{
				"type":        "string",
				"description": "Symbol name for references action",
			},
			"reverse": map[string]any{
				"type":        "boolean",
				"description": "For imports: show who imports this package (reverse dependency)",
				"default":     false,
			},
		},
		"required": []string{"action"},
	}
}

func toolAnalyze(defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p analyzeParams
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid analyze params: %w", err)
		}

		switch p.Action {
		case "outline":
			return analyzeOutline(p, defaultDir)
		case "symbols":
			return analyzeSymbols(ctx, p, defaultDir)
		case "references":
			return analyzeReferences(ctx, p, defaultDir)
		case "imports":
			return analyzeImports(p, defaultDir)
		case "signature":
			return analyzeSignature(p, defaultDir)
		default:
			return "", fmt.Errorf("unknown analyze action: %q", p.Action)
		}
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
	path := resolvePath(p.File, defaultDir)

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
		searchDir = resolvePath(p.Path, defaultDir)
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

// --- References ---

func analyzeReferences(ctx context.Context, p analyzeParams, defaultDir string) (string, error) {
	symbol := p.Symbol
	if symbol == "" {
		symbol = p.Query
	}
	if symbol == "" {
		return "", fmt.Errorf("symbol is required for references action")
	}

	searchDir := defaultDir
	if p.Path != "" {
		searchDir = resolvePath(p.Path, defaultDir)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## References to %q\n\n", symbol)

	const maxResults = 100
	found := 0

	// Use simple text search across source files.
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
		if !isSourceFile(path) {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		content := string(data)
		lines := strings.Split(content, "\n")
		rel, _ := filepath.Rel(defaultDir, path)

		for i, line := range lines {
			if strings.Contains(line, symbol) {
				fmt.Fprintf(&sb, "%s:%d: %s\n", rel, i+1, strings.TrimSpace(line))
				found++
				if found >= maxResults {
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("reference search failed: %w", err)
	}

	if found == 0 {
		return fmt.Sprintf("No references found for %q", symbol), nil
	}
	if found >= maxResults {
		sb.WriteString(fmt.Sprintf("\n[... capped at %d results]", maxResults))
	}
	return sb.String(), nil
}

// --- Imports ---

func analyzeImports(p analyzeParams, defaultDir string) (string, error) {
	if p.File == "" && p.Path == "" {
		return "", fmt.Errorf("file or path is required for imports")
	}

	if p.File != "" && !p.Reverse {
		// Show imports of a single file.
		path := resolvePath(p.File, defaultDir)
		if isGoFile(path) {
			return importsGoFile(path, p.File)
		}
		if isRustFile(path) {
			return importsRustFile(path, p.File)
		}
		return "", fmt.Errorf("imports analysis supports .go and .rs files")
	}

	// Reverse: find files that import a given package/module.
	if p.Reverse {
		query := p.File
		if query == "" {
			query = p.Query
		}
		if query == "" {
			return "", fmt.Errorf("file or query is required for reverse imports")
		}
		return reverseImports(query, p.Path, defaultDir)
	}

	return "", fmt.Errorf("file is required for imports (use reverse=true for reverse lookup)")
}

func importsGoFile(path, displayPath string) (string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return "", fmt.Errorf("failed to parse Go file: %w", err)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Imports: %s\n\n", displayPath)

	if len(node.Imports) == 0 {
		sb.WriteString("(no imports)\n")
		return sb.String(), nil
	}

	for _, imp := range node.Imports {
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name + " "
		}
		fmt.Fprintf(&sb, "  %s%s\n", name, imp.Path.Value)
	}
	return sb.String(), nil
}

func importsRustFile(path, displayPath string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Imports: %s\n\n", displayPath)

	found := 0
	for _, loc := range rustUsePattern.FindAllStringIndex(string(data), -1) {
		match := rustUsePattern.FindStringSubmatch(string(data)[loc[0]:loc[1]])
		if match != nil && len(match) > 3 {
			fmt.Fprintf(&sb, "  use %s\n", match[3])
			found++
		}
	}

	if found == 0 {
		sb.WriteString("(no imports)\n")
	}
	return sb.String(), nil
}

func reverseImports(query, searchPath, defaultDir string) (string, error) {
	dir := defaultDir
	if searchPath != "" {
		dir = resolvePath(searchPath, defaultDir)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Files importing %q\n\n", query)

	const maxResults = 50
	found := 0

	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
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
			fset := token.NewFileSet()
			node, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if parseErr != nil {
				return nil
			}
			for _, imp := range node.Imports {
				importPath := strings.Trim(imp.Path.Value, "\"")
				if strings.Contains(importPath, query) {
					fmt.Fprintf(&sb, "%s  imports %s\n", rel, imp.Path.Value)
					found++
					break
				}
			}
		} else if isRustFile(path) {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			content := string(data)
			for _, loc := range rustUsePattern.FindAllStringIndex(content, -1) {
				match := rustUsePattern.FindStringSubmatch(content[loc[0]:loc[1]])
				if match != nil && len(match) > 3 && strings.Contains(match[3], query) {
					fmt.Fprintf(&sb, "%s  uses %s\n", rel, match[3])
					found++
					break
				}
			}
		}
		return nil
	})

	if found == 0 {
		return fmt.Sprintf("No files found importing %q", query), nil
	}
	return sb.String(), nil
}

// --- Signature ---

func analyzeSignature(p analyzeParams, defaultDir string) (string, error) {
	query := p.Symbol
	if query == "" {
		query = p.Query
	}
	if query == "" {
		return "", fmt.Errorf("symbol or query is required for signature")
	}

	if p.File != "" {
		path := resolvePath(p.File, defaultDir)
		if isGoFile(path) {
			return signatureGoFile(path, query, p.File)
		}
		return "", fmt.Errorf("signature action requires a .go file")
	}

	return "", fmt.Errorf("file is required for signature")
}

func signatureGoFile(path, query, displayPath string) (string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("failed to parse: %w", err)
	}

	var sb strings.Builder
	found := false

	for _, decl := range node.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if !matchesQuery(fd.Name.Name, query) {
			continue
		}
		sig := formatGoFuncSignature(fd)
		line := fset.Position(fd.Pos()).Line

		// Extract doc comment.
		doc := ""
		if fd.Doc != nil {
			doc = strings.TrimSpace(fd.Doc.Text())
		}

		if doc != "" {
			fmt.Fprintf(&sb, "// %s\n", strings.ReplaceAll(doc, "\n", "\n// "))
		}
		fmt.Fprintf(&sb, "func %s  (%s:%d)\n\n", sig, displayPath, line)
		found = true
	}

	if !found {
		return fmt.Sprintf("No function matching %q found in %s", query, displayPath), nil
	}
	return sb.String(), nil
}

// --- Helpers ---

func isGoFile(path string) bool {
	return strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")
}

func isRustFile(path string) bool {
	return strings.HasSuffix(path, ".rs")
}

func isSourceFile(path string) bool {
	ext := filepath.Ext(path)
	switch ext {
	case ".go", ".rs", ".js", ".ts", ".tsx", ".jsx", ".py", ".java", ".c", ".h", ".cpp", ".hpp":
		return true
	}
	return false
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "target", "vendor", "dist", "build",
		"__pycache__", ".next", ".nuxt", ".cache", ".tox":
		return true
	}
	return false
}

func matchesQuery(name, query string) bool {
	return strings.EqualFold(name, query) || strings.Contains(strings.ToLower(name), strings.ToLower(query))
}

func goTypeKind(expr ast.Expr) string {
	switch expr.(type) {
	case *ast.StructType:
		return "struct"
	case *ast.InterfaceType:
		return "interface"
	case *ast.MapType:
		return "map"
	case *ast.ArrayType:
		return "array"
	case *ast.ChanType:
		return "chan"
	case *ast.FuncType:
		return "func"
	default:
		return "type"
	}
}

func formatGoFuncSignature(fd *ast.FuncDecl) string {
	var sb strings.Builder

	// Receiver.
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		recv := fd.Recv.List[0]
		sb.WriteString("(")
		sb.WriteString(formatGoExpr(recv.Type))
		sb.WriteString(") ")
	}

	sb.WriteString(fd.Name.Name)
	sb.WriteString("(")

	// Parameters.
	if fd.Type.Params != nil {
		params := formatGoFieldList(fd.Type.Params)
		sb.WriteString(params)
	}
	sb.WriteString(")")

	// Return types.
	if fd.Type.Results != nil && len(fd.Type.Results.List) > 0 {
		sb.WriteString(" ")
		if len(fd.Type.Results.List) == 1 && len(fd.Type.Results.List[0].Names) == 0 {
			sb.WriteString(formatGoExpr(fd.Type.Results.List[0].Type))
		} else {
			sb.WriteString("(")
			sb.WriteString(formatGoFieldList(fd.Type.Results))
			sb.WriteString(")")
		}
	}

	return sb.String()
}

func formatGoFieldList(fl *ast.FieldList) string {
	var parts []string
	for _, f := range fl.List {
		typeName := formatGoExpr(f.Type)
		if len(f.Names) == 0 {
			parts = append(parts, typeName)
		} else {
			for _, name := range f.Names {
				parts = append(parts, name.Name+" "+typeName)
			}
		}
	}
	return strings.Join(parts, ", ")
}

func formatGoExpr(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return formatGoExpr(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + formatGoExpr(t.X)
	case *ast.ArrayType:
		return "[]" + formatGoExpr(t.Elt)
	case *ast.MapType:
		return "map[" + formatGoExpr(t.Key) + "]" + formatGoExpr(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ellipsis:
		return "..." + formatGoExpr(t.Elt)
	case *ast.FuncType:
		return "func(...)"
	case *ast.ChanType:
		return "chan " + formatGoExpr(t.Value)
	case *ast.IndexExpr:
		return formatGoExpr(t.X) + "[" + formatGoExpr(t.Index) + "]"
	default:
		return "any"
	}
}
