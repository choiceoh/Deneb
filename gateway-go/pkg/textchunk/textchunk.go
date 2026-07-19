// Package textchunk splits searchable text while preserving useful source
// boundaries and absolute line numbers. It is deliberately dependency-free so
// wiki and file recall can share the same preprocessing contract.
package textchunk

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	DefaultTargetRunes = 1800
	DefaultMaxChunks   = 20
)

// Options bounds chunk size and count. Zero values select production defaults.
type Options struct {
	TargetRunes int
	MaxChunks   int
}

// Chunk is one source-addressable retrieval unit.
type Chunk struct {
	Text      string `json:"text"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Kind      string `json:"kind,omitempty"`
	Heading   string `json:"heading,omitempty"`
}

type lineRange struct {
	start   int
	end     int
	kind    string
	heading string
}

// Split chooses a structure-aware splitter from name's extension and falls
// back to paragraph/line boundaries for extracted or unknown text.
func Split(name, text string, options Options) []Chunk {
	options = normalizeOptions(options)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var ranges []lineRange
	codeLanguage := ""
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		ranges = markdownRanges(lines)
	case ".go":
		ranges = goRanges(text, lines)
		codeLanguage = "go"
	case ".kt", ".kts":
		ranges = kotlinRanges(lines)
		codeLanguage = "kotlin"
	case ".py":
		ranges = pythonRanges(lines)
		codeLanguage = "python"
	case ".js", ".jsx", ".ts", ".tsx":
		ranges = javascriptRanges(lines)
		codeLanguage = "javascript"
	case ".java":
		ranges = javaRanges(lines)
		codeLanguage = "java"
	case ".rs":
		ranges = rustRanges(lines)
		codeLanguage = "rust"
	default:
		ranges = paragraphRanges(lines)
	}
	chunks := materialize(lines, ranges, options)
	if codeLanguage != "" {
		chunks = prependCodeOverview(lines, chunks, codeLanguage, options)
	}
	return chunks
}

func normalizeOptions(options Options) Options {
	if options.TargetRunes <= 0 {
		options.TargetRunes = DefaultTargetRunes
	}
	if options.MaxChunks <= 0 {
		options.MaxChunks = DefaultMaxChunks
	}
	return options
}

func markdownRanges(lines []string) []lineRange {
	var ranges []lineRange
	start := 0
	inFence := false
	heading := ""
	flush := func(end int, kind string) {
		if end <= start || blankLines(lines, start, end) {
			start = end
			return
		}
		ranges = append(ranges, lineRange{start: start, end: end, kind: kind, heading: heading})
		start = end
	}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			marker := strings.TrimLeft(trimmed, "#")
			if len(marker) < len(trimmed) && strings.HasPrefix(marker, " ") {
				flush(i, "markdown")
				heading = strings.TrimSpace(marker)
				start = i
			}
			continue
		}
		if trimmed == "" && i > start {
			// Keep a heading with the first block beneath it. Markdown commonly
			// inserts one blank line immediately after a heading; treating that
			// blank as a boundary creates a useless heading-only vector.
			if i == start+1 && strings.HasPrefix(strings.TrimSpace(lines[start]), "#") {
				continue
			}
			flush(i+1, "markdown")
		}
	}
	flush(len(lines), "markdown")
	return ranges
}

func goRanges(text string, lines []string) []lineRange {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "source.go", text, parser.SkipObjectResolution)
	if err != nil || file == nil {
		return paragraphRanges(lines)
	}
	declarations := make([]codeDeclaration, 0, len(file.Decls))
	for _, decl := range file.Decls {
		declarations = append(declarations, codeDeclaration{
			line: fset.Position(decl.Pos()).Line - 1, kind: goDeclarationKind(decl), heading: goDeclarationHeading(decl),
		})
	}
	return declarationRanges(lines, declarations)
}

func goDeclarationHeading(decl ast.Decl) string {
	switch declaration := decl.(type) {
	case *ast.FuncDecl:
		if declaration.Recv != nil && len(declaration.Recv.List) > 0 {
			return goReceiverName(declaration.Recv.List[0].Type) + "." + declaration.Name.Name
		}
		return declaration.Name.Name
	case *ast.GenDecl:
		var names []string
		for _, spec := range declaration.Specs {
			switch value := spec.(type) {
			case *ast.TypeSpec:
				names = append(names, value.Name.Name)
			case *ast.ValueSpec:
				for _, name := range value.Names {
					names = append(names, name.Name)
				}
			}
		}
		return strings.Join(names, ", ")
	default:
		return ""
	}
}

func goReceiverName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return goReceiverName(value.X)
	case *ast.IndexExpr:
		return goReceiverName(value.X)
	case *ast.IndexListExpr:
		return goReceiverName(value.X)
	default:
		return "receiver"
	}
}

type codeDeclaration struct {
	line    int
	kind    string
	heading string
}

func declarationRanges(lines []string, declarations []codeDeclaration) []lineRange {
	if len(declarations) == 0 {
		return paragraphRanges(lines)
	}
	sort.SliceStable(declarations, func(i, j int) bool { return declarations[i].line < declarations[j].line })
	ranges := make([]lineRange, 0, len(declarations))
	for i, declaration := range declarations {
		start := declaration.line
		end := len(lines)
		if i+1 < len(declarations) {
			end = declarations[i+1].line
		}
		if start < 0 || start >= end {
			continue
		}
		ranges = append(ranges, lineRange{start: start, end: end, kind: declaration.kind, heading: declaration.heading})
	}
	return ranges
}

func goDeclarationKind(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil {
			return "go-method"
		}
		return "go-function"
	case *ast.GenDecl:
		return "go-" + strings.ToLower(d.Tok.String())
	default:
		return "go"
	}
}

var kotlinDeclaration = regexp.MustCompile(`^\s*(?:(?:public|private|protected|internal|open|final|abstract|sealed|data|inline|suspend|operator|override|tailrec|external|expect|actual|annotation|value|companion)\s+)*(class|object|interface|fun|typealias|enum\s+class)\s+([A-Za-z_][A-Za-z0-9_]*)`)

func kotlinRanges(lines []string) []lineRange {
	var declarations []codeDeclaration
	parent := ""
	parentIndent := -1
	for i, line := range lines {
		match := kotlinDeclaration.FindStringSubmatch(line)
		if len(match) == 0 {
			continue
		}
		kind := "kotlin-" + strings.ReplaceAll(match[1], " ", "-")
		heading := match[2]
		indent := leadingWhitespace(line)
		if strings.Contains(match[1], "class") || match[1] == "object" || match[1] == "interface" {
			parent, parentIndent = heading, indent
		} else if match[1] == "fun" && parent != "" && indent > parentIndent {
			kind = "kotlin-method"
			heading = parent + "." + heading
		} else if indent <= parentIndent {
			parent, parentIndent = "", -1
		}
		declarations = append(declarations, codeDeclaration{line: i, kind: kind, heading: heading})
	}
	return declarationRanges(lines, declarations)
}

var pythonDeclaration = regexp.MustCompile(`^(\s*)(?:(async)\s+)?(class|def)\s+([A-Za-z_][A-Za-z0-9_]*)`)

func pythonRanges(lines []string) []lineRange {
	var declarations []codeDeclaration
	parent := ""
	parentIndent := -1
	for i, line := range lines {
		match := pythonDeclaration.FindStringSubmatch(line)
		if len(match) == 0 {
			continue
		}
		indent := len(match[1])
		kind := "python-" + match[3]
		heading := match[4]
		if match[3] == "class" {
			parent, parentIndent = heading, indent
		} else if parent != "" && indent > parentIndent {
			kind = "python-method"
			heading = parent + "." + heading
		} else if indent <= parentIndent {
			parent, parentIndent = "", -1
		}
		declarations = append(declarations, codeDeclaration{line: i, kind: kind, heading: heading})
	}
	return declarationRanges(lines, declarations)
}

var (
	javascriptClass  = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(class|interface)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	javascriptFunc   = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	javascriptArrow  = regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=.*=>`)
	javascriptMethod = regexp.MustCompile(`^\s*(?:public\s+|private\s+|protected\s+|static\s+|async\s+)*(?:get\s+|set\s+)?([A-Za-z_$][A-Za-z0-9_$]*)\s*\([^)]*\)\s*(?::[^={]+)?\s*\{`)
)

func javascriptRanges(lines []string) []lineRange {
	return braceLanguageRanges(lines, "javascript", javascriptDeclarations)
}

func javascriptDeclarations(line string) (string, string, bool) {
	if match := javascriptClass.FindStringSubmatch(line); len(match) > 0 {
		return "javascript-" + match[1], match[2], true
	}
	if match := javascriptFunc.FindStringSubmatch(line); len(match) > 0 {
		return "javascript-function", match[1], true
	}
	if match := javascriptArrow.FindStringSubmatch(line); len(match) > 0 {
		return "javascript-function", match[1], true
	}
	if match := javascriptMethod.FindStringSubmatch(line); len(match) > 0 && !codeControlWord(match[1]) {
		return "javascript-method", match[1], true
	}
	return "", "", false
}

var (
	javaType   = regexp.MustCompile(`^\s*(?:(?:public|private|protected|abstract|final|static|sealed|non-sealed)\s+)*(class|interface|enum|record)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	javaMethod = regexp.MustCompile(`^\s*(?:(?:public|private|protected|abstract|final|static|synchronized|native|default)\s+)*(?:[A-Za-z_][A-Za-z0-9_<>,.?\[\]]*\s+)+([A-Za-z_][A-Za-z0-9_]*)\s*\([^;]*\)\s*(?:throws[^\{]+)?\{`)
)

func javaRanges(lines []string) []lineRange {
	return braceLanguageRanges(lines, "java", func(line string) (string, string, bool) {
		if match := javaType.FindStringSubmatch(line); len(match) > 0 {
			return "java-" + match[1], match[2], true
		}
		if match := javaMethod.FindStringSubmatch(line); len(match) > 0 && !codeControlWord(match[1]) {
			return "java-method", match[1], true
		}
		return "", "", false
	})
}

var (
	rustType = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(struct|enum|trait|impl)\s+([^\s<{(]+)`)
	rustFunc = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?fn\s+([A-Za-z_][A-Za-z0-9_]*)`)
)

func rustRanges(lines []string) []lineRange {
	return braceLanguageRanges(lines, "rust", func(line string) (string, string, bool) {
		if match := rustType.FindStringSubmatch(line); len(match) > 0 {
			return "rust-" + match[1], match[2], true
		}
		if match := rustFunc.FindStringSubmatch(line); len(match) > 0 {
			return "rust-function", match[1], true
		}
		return "", "", false
	})
}

func braceLanguageRanges(lines []string, language string, parse func(string) (string, string, bool)) []lineRange {
	var declarations []codeDeclaration
	parent := ""
	parentDepth := -1
	depth := 0
	for i, line := range lines {
		kind, heading, ok := parse(line)
		if ok {
			if parent != "" && depth > parentDepth &&
				(strings.HasSuffix(kind, "method") || strings.HasSuffix(kind, "function")) {
				heading = parent + "." + heading
				kind = language + "-method"
			} else if strings.Contains(kind, "class") || strings.Contains(kind, "interface") || strings.Contains(kind, "trait") || strings.Contains(kind, "impl") {
				parent, parentDepth = heading, depth
			}
			declarations = append(declarations, codeDeclaration{line: i, kind: kind, heading: heading})
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if parent != "" && depth <= parentDepth {
			parent, parentDepth = "", -1
		}
	}
	return declarationRanges(lines, declarations)
}

func codeControlWord(name string) bool {
	switch strings.ToLower(name) {
	case "if", "for", "while", "switch", "catch", "with":
		return true
	default:
		return false
	}
}

func leadingWhitespace(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

func prependCodeOverview(lines []string, chunks []Chunk, language string, options Options) []Chunk {
	if len(chunks) == 0 || options.MaxChunks < 2 {
		return chunks
	}
	var symbols []string
	for _, chunk := range chunks {
		if chunk.Heading != "" {
			symbols = append(symbols, chunk.Kind+": "+chunk.Heading)
		}
	}
	headerEnd := min(len(lines), 30)
	header := strings.TrimSpace(strings.Join(lines[:headerEnd], "\n"))
	overview := strings.TrimSpace("Symbols:\n" + strings.Join(symbols, "\n") + "\n\n" + header)
	if runes := []rune(overview); len(runes) > options.TargetRunes {
		overview = strings.TrimSpace(string(runes[:options.TargetRunes]))
	}
	if overview == "" {
		return chunks
	}
	out := make([]Chunk, 0, min(options.MaxChunks, len(chunks)+1))
	out = append(out, Chunk{Text: overview, StartLine: 1, EndLine: len(lines), Kind: language + "-file", Heading: "file overview"})
	out = append(out, chunks...)
	if len(out) > options.MaxChunks {
		out = out[:options.MaxChunks]
	}
	return out
}

func paragraphRanges(lines []string) []lineRange {
	var ranges []lineRange
	start := 0
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			continue
		}
		if i > start && !blankLines(lines, start, i) {
			ranges = append(ranges, lineRange{start: start, end: i + 1, kind: "paragraph"})
		}
		start = i + 1
	}
	if start < len(lines) && !blankLines(lines, start, len(lines)) {
		ranges = append(ranges, lineRange{start: start, end: len(lines), kind: "paragraph"})
	}
	if len(ranges) == 0 {
		ranges = append(ranges, lineRange{start: 0, end: len(lines), kind: "text"})
	}
	return ranges
}

func materialize(lines []string, ranges []lineRange, options Options) []Chunk {
	var out []Chunk
	var pending *lineRange
	flush := func() {
		if pending == nil || len(out) >= options.MaxChunks {
			pending = nil
			return
		}
		out = appendRangeChunks(out, lines, *pending, options)
		pending = nil
	}
	for _, current := range ranges {
		if current.end <= current.start || blankLines(lines, current.start, current.end) {
			continue
		}
		if pending == nil {
			pendingRange := current
			pending = &pendingRange
			continue
		}
		combined := strings.Join(lines[pending.start:current.end], "\n")
		if len([]rune(combined)) <= options.TargetRunes && pending.kind == current.kind && pending.heading == current.heading {
			pending.end = current.end
			if pending.heading == "" {
				pending.heading = current.heading
			}
			continue
		}
		flush()
		if len(out) >= options.MaxChunks {
			break
		}
		pendingRange := current
		pending = &pendingRange
	}
	flush()
	if len(out) > options.MaxChunks {
		out = out[:options.MaxChunks]
	}
	return out
}

func appendRangeChunks(out []Chunk, lines []string, r lineRange, options Options) []Chunk {
	start := r.start
	for start < r.end && len(out) < options.MaxChunks {
		lineRunes := []rune(lines[start])
		if len(lineRunes) > options.TargetRunes {
			for offset := 0; offset < len(lineRunes) && len(out) < options.MaxChunks; offset += options.TargetRunes {
				endOffset := min(offset+options.TargetRunes, len(lineRunes))
				text := strings.TrimSpace(string(lineRunes[offset:endOffset]))
				if text != "" {
					out = append(out, Chunk{Text: text, StartLine: start + 1, EndLine: start + 1, Kind: r.kind, Heading: r.heading})
				}
			}
			start++
			continue
		}
		end := start
		runes := 0
		for end < r.end {
			n := len([]rune(lines[end])) + 1
			if end > start && runes+n > options.TargetRunes {
				break
			}
			runes += n
			end++
		}
		if end == start {
			end++
		}
		text := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
		if text != "" {
			out = append(out, Chunk{Text: text, StartLine: start + 1, EndLine: end, Kind: r.kind, Heading: r.heading})
		}
		start = end
	}
	return out
}

func blankLines(lines []string, start, end int) bool {
	for _, line := range lines[start:end] {
		if strings.TrimSpace(line) != "" {
			return false
		}
	}
	return true
}
