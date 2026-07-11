package filesystem

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/artifact"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// lineAnchorHash returns a short, stable content hash for a single line of
// text. The read tool surfaces it (hashes=true) and the edit tool resolves it
// (anchor=…) so the model can target a whole line without reproducing it as
// old_string — saving output tokens. 24-bit FNV-1a → 6 hex chars: short enough
// to keep the model's output cost near zero, wide enough that distinct-content
// collisions across a normal file are negligible. Lines with identical content
// share a hash by design; the edit path reports that as an ambiguous anchor so
// the model falls back to line= or old_string.
func lineAnchorHash(line string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(line))
	return fmt.Sprintf("%06x", h.Sum32()&0xffffff)
}

// --- Read tool ---

// listDirForRead renders a directory's entries as a readable listing. A read on
// a directory is a frequent, benign LLM move — exploring, or hitting a path that
// turned out to be a dir (e.g. a wiki page that was momentarily a directory
// during a structure change). Returning the listing rather than a hard error is
// more useful to the model and keeps the mistake out of the tool error stats
// (this case was the bulk of read's recorded failures). Entries are capped so a
// huge directory can't blow up the output.
func listDirForRead(absPath, displayPath string) (string, error) {
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to read directory %q: %w", displayPath, err)
	}
	const maxDirEntries = 200
	var b strings.Builder
	fmt.Fprintf(&b, "%q is a directory with %d entries:\n", displayPath, len(entries))
	shown := entries
	if len(shown) > maxDirEntries {
		shown = shown[:maxDirEntries]
	}
	for _, e := range shown {
		if e.IsDir() {
			fmt.Fprintf(&b, "  %s/\n", e.Name())
			continue
		}
		if info, statErr := e.Info(); statErr == nil {
			fmt.Fprintf(&b, "  %s (%d bytes)\n", e.Name(), info.Size())
		} else {
			fmt.Fprintf(&b, "  %s\n", e.Name())
		}
	}
	if len(entries) > maxDirEntries {
		fmt.Fprintf(&b, "  … and %d more (showing first %d)\n", len(entries)-maxDirEntries, maxDirEntries)
	}
	b.WriteString("\nRead a file inside by passing its full path.")
	return b.String(), nil
}

// trySkillRootFallback resolves the bundled-skill path collision. The skill
// catalog spans several roots (managed ~/.deneb/skills, bundled ~/deneb/skills,
// personal ~/.agents/skills); a skill lives under exactly one. The model, asked
// to read a skill, often points at the wrong root — most commonly it reads a
// bundled skill at ~/.deneb/skills/<rel> because every other Deneb path is under
// ~/.deneb, so the lone ~/deneb (repo) bundled path gets "corrected". That path
// is an allowed root but holds the managed set, so os.ReadFile 404s.
//
// When the missing path is under one skill root, this retries the same
// skills-relative remainder under every OTHER root and returns the first hit. It
// only runs after a failed read, only across the roots already passed to the read
// tool (so it cannot escape the allowed catalog), and the relative remainder is
// rejected if it escapes its root (".."), so it adds no reach and no hot-path cost.
func trySkillRootFallback(path string, skillRoots []string) (string, []byte, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", nil, false
	}
	for _, root := range skillRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(absRoot, abs)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue // path is not under this root
		}
		for _, other := range skillRoots {
			other = strings.TrimSpace(other)
			if other == "" || other == root {
				continue
			}
			cand := filepath.Join(other, rel)
			if data, e := os.ReadFile(cand); e == nil {
				return cand, data, true
			}
		}
		return "", nil, false // under this root but absent elsewhere
	}
	return "", nil, false
}

// ToolRead returns the file-read tool. extraReadRoots are directories outside
// the workspace that reads may reach (read-only; currently the skills catalog —
// the system prompt directs the model to read SKILL.md at those locations).
func ToolRead(defaultDir string, extraReadRoots ...string) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		var p struct {
			FilePath string `json:"file_path"`
			Offset   int    `json:"offset"`
			Limit    int    `json:"limit"`
			Function string `json:"function"`
			Force    bool   `json:"force"`
			Hashes   bool   `json:"hashes"`
		}
		if err := jsonutil.UnmarshalInto("read params", input, &p); err != nil {
			return "", err
		}
		if p.FilePath == "" {
			return "", fmt.Errorf("file_path is required")
		}

		dir := defaultDir
		path := artifact.ResolvePathWithRoots(p.FilePath, dir, extraReadRoots)
		if err := artifact.CheckProtectedPath(path, "read"); err != nil {
			return "", err
		}

		// File-read dedup: for default full-file reads (no offset/limit/function),
		// check cache before hitting disk.  Skip if force=true.
		fc := toolctx.FileCacheFromContext(ctx)
		// hashes=true emits per-line anchors, which the plain cached output does
		// not contain — bypass the dedup cache for those reads.
		useCache := fc != nil && !p.Force && !p.Hashes && p.Function == "" && p.Offset <= 0 && p.Limit <= 0
		if useCache {
			if entry := fc.Get(path); entry != nil && !agent.FileChanged(path, entry) {
				entry.ReadCount++
				return agent.FormatCachedRead(p.FilePath, entry), nil
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			// Cross-skill-root fallback: a bundled skill loads from ~/deneb/skills
			// (the repo) and is advertised there, but the model — primed by the
			// pervasive ~/.deneb/ convention everywhere else — frequently reads it at
			// ~/.deneb/skills/<rel>, an allowed root that holds a DIFFERENT (managed)
			// skill set, so the read 404s (this silently broke the 8am morning-letter
			// cron: it could not load its bundled SKILL.md). When the path is under
			// one skill root and missing, try the same skills-relative remainder under
			// the other roots so the bundled skill resolves regardless of which root
			// the model picked. Scoped to the already-allowed catalog roots.
			if altPath, altData, ok := trySkillRootFallback(path, extraReadRoots); ok {
				path, data, err = altPath, altData, nil
			}
		}
		if err != nil {
			// A read on a directory is a common, benign LLM move (exploring, or a
			// path that turned out to be a dir). Return the listing instead of a
			// hard error — more useful, and it keeps the mistake out of the error
			// stats (this hard error was the bulk of read's recorded failures).
			if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
				return listDirForRead(path, p.FilePath)
			}
			return "", fmt.Errorf("failed to read file: %w", err)
		}

		// Function extraction mode — needs the full content as string.
		if p.Function != "" {
			return readFunction(path, p.FilePath, string(data), p.Function)
		}

		// Count total lines cheaply (byte scan, no allocation).
		totalLines := bytes.Count(data, []byte{'\n'}) + 1

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

		// Stream through the byte slice, materializing only the lines in range.
		// This avoids strings.Split() which allocates a string per line.
		var sb strings.Builder
		if p.Hashes {
			fmt.Fprintf(&sb, "[File: %s | %d lines | columns: line<TAB>anchor<TAB>content — pass anchor=<hash> to edit]\n", p.FilePath, totalLines)
		} else {
			fmt.Fprintf(&sb, "[File: %s | %d lines]\n", p.FilePath, totalLines)
		}
		scanner := bufio.NewScanner(bytes.NewReader(data))
		scanner.Buffer(nil, bufio.MaxScanTokenSize)
		lineNum := 0
		for scanner.Scan() {
			if lineNum >= end {
				break
			}
			if lineNum >= start {
				if p.Hashes {
					fmt.Fprintf(&sb, "%d\t%s\t%s\n", lineNum+1, lineAnchorHash(scanner.Text()), scanner.Text())
				} else {
					fmt.Fprintf(&sb, "%d\t%s\n", lineNum+1, scanner.Text())
				}
			}
			lineNum++
		}
		if end < totalLines {
			fmt.Fprintf(&sb, "[... %d more lines. Use offset=%d to continue reading.]\n", totalLines-end, end+1)
		}
		output := sb.String()

		// Cache the result for future dedup (only for default full-file reads, ≤1MB).
		if useCache {
			if info, statErr := os.Stat(path); statErr == nil && info.Size() <= fc.MaxEntrySize() {
				fc.Set(path, &agent.FileCacheEntry{
					Path:        path,
					MTime:       info.ModTime(),
					Size:        info.Size(),
					Content:     output,
					ContentHash: agent.ContentHashOf(data),
					ReadAt:      time.Now(),
					ReadCount:   1,
				})
			}
		}

		return output, nil
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
			switch ch {
			case '{', '(':
				depth++
				started = true
			case '}', ')':
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
		fmt.Fprintf(&sb, "%d\t%s\n", i+1, lines[i])
	}
	return sb.String(), nil
}
