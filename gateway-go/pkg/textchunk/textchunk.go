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
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		ranges = markdownRanges(lines)
	case ".go":
		ranges = goRanges(text, lines)
	case ".kt", ".kts":
		ranges = kotlinRanges(lines)
	default:
		ranges = paragraphRanges(lines)
	}
	return materialize(lines, ranges, options)
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
	starts := []int{0}
	for _, decl := range file.Decls {
		line := fset.Position(decl.Pos()).Line - 1
		if line > 0 {
			starts = append(starts, line)
		}
	}
	sort.Ints(starts)
	starts = uniqueInts(starts)
	ranges := make([]lineRange, 0, len(starts))
	for i, start := range starts {
		end := len(lines)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		kind := "go"
		if i > 0 {
			kind = goDeclarationKind(file.Decls[i-1])
		}
		ranges = append(ranges, lineRange{start: start, end: end, kind: kind})
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

var kotlinDeclaration = regexp.MustCompile(`^\s*(?:(?:public|private|protected|internal|open|final|abstract|sealed|data|inline|suspend|operator|override|tailrec|external|expect|actual|annotation|value|companion)\s+)*(?:class|object|interface|fun|typealias|enum\s+class)\b`)

func kotlinRanges(lines []string) []lineRange {
	starts := []int{0}
	for i, line := range lines {
		if i > 0 && kotlinDeclaration.MatchString(line) {
			starts = append(starts, i)
		}
	}
	starts = uniqueInts(starts)
	ranges := make([]lineRange, 0, len(starts))
	for i, start := range starts {
		end := len(lines)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		ranges = append(ranges, lineRange{start: start, end: end, kind: "kotlin-declaration"})
	}
	return ranges
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

func uniqueInts(values []int) []int {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}
