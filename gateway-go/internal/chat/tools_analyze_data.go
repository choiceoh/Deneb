package chat

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

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
