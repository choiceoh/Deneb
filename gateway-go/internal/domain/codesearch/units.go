package codesearch

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/pkg/textchunk"
)

const (
	maxRepositoryFileBytes = 512 << 10
	repositoryChunkRunes   = 1800
	// 512 KiB of single-byte text needs fewer than 300 target-sized chunks.
	// Keeping 320 means the file-size guard, not a head-only chunk cap, is the
	// coverage boundary: logic near the end of a long deploy script remains
	// searchable.
	maxChunksPerRepoFile = 320
)

// prepareSearchUnits hydrates CodeGraph nodes with compact source context and
// appends chunks for tracked text files CodeGraph did not model. The latter is
// essential for operational code: shell deploys, workflows, configuration,
// Markdown rules, and top-level scripts otherwise have no semantic unit at all.
func prepareSearchUnits(ctx context.Context, repo string, nodes []node) ([]node, error) {
	indexedFiles := make(map[string]bool, len(nodes))
	lineCache := make(map[string][]string)
	for i := range nodes {
		indexedFiles[nodes[i].File] = true
		nodes[i].Text = renderNodeText(repo, nodes[i], lineCache)
		nodes[i].Lexical = truncateRunes(nodes[i].Text, lexicalTextRunes)
	}

	chunks, err := loadRepositoryChunks(ctx, repo, indexedFiles)
	if err != nil {
		return nil, err
	}
	nodes = append(nodes, chunks...)
	// Stable metadata/vector order makes sidecar diffs and partial-build progress
	// deterministic even when SQLite changes its query plan.
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes, nil
}

func renderNodeText(repo string, n node, cache map[string][]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repository code unit\nLanguage: %s\nKind: %s\nSymbol: %s\nPath: %s\n",
		n.Language, n.Kind, n.Qualified, n.File)
	if n.Signature != "" {
		b.WriteString("Signature: ")
		b.WriteString(n.Signature)
		b.WriteByte('\n')
	}
	if n.Docstring != "" {
		b.WriteString("Documentation:\n")
		b.WriteString(n.Docstring)
		b.WriteByte('\n')
	}
	if source := sourceLines(repo, n.Entry, excerptLines, cache); source != "" {
		b.WriteString("Source:\n")
		b.WriteString(source)
	}
	return b.String()
}

func sourceLines(repo string, e Entry, maxLines int, cache map[string][]string) string {
	lines, ok := cache[e.File]
	if !ok {
		body, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(e.File)))
		if err != nil {
			cache[e.File] = nil
			return ""
		}
		lines = strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
		cache[e.File] = lines
	}
	lo := max(0, e.StartLine-1)
	if lo >= len(lines) {
		return ""
	}
	hi := e.EndLine
	if hi <= lo {
		hi = lo + maxLines
	}
	hi = min(hi, lo+maxLines, len(lines))
	return strings.Join(lines[lo:hi], "\n")
}

func loadRepositoryChunks(ctx context.Context, repo string, indexedFiles map[string]bool) ([]node, error) {
	paths, err := trackedFiles(ctx, repo)
	if err != nil {
		return nil, err
	}
	return repositoryChunks(repo, paths, indexedFiles), nil
}

func trackedFiles(ctx context.Context, repo string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repo, "ls-files", "-z").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	parts := strings.Split(string(out), "\x00")
	paths := parts[:0]
	for _, p := range parts {
		if p != "" {
			paths = append(paths, filepath.ToSlash(p))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func repositoryChunks(repo string, paths []string, indexedFiles map[string]bool) []node {
	var out []node
	for _, rel := range paths {
		if indexedFiles[rel] || !searchableRepositoryPath(rel) {
			continue
		}
		abs := filepath.Join(repo, filepath.FromSlash(rel))
		info, err := os.Lstat(abs)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxRepositoryFileBytes {
			continue
		}
		body, err := os.ReadFile(abs)
		if err != nil || len(body) == 0 || !utf8.Valid(body) || strings.IndexByte(string(body), 0) >= 0 {
			continue
		}
		pieces := textchunk.Split(rel, string(body), textchunk.Options{
			TargetRunes: repositoryChunkRunes,
			MaxChunks:   maxChunksPerRepoFile,
		})
		for ordinal, piece := range pieces {
			if len([]rune(strings.TrimSpace(piece.Text))) < 8 {
				continue
			}
			qualified := rel
			if piece.Heading != "" {
				qualified += "#" + piece.Heading
			}
			entry := Entry{
				ID:        fmt.Sprintf("repo:%s:%d:%d", rel, piece.StartLine, ordinal),
				Kind:      "file_chunk",
				Language:  repositoryLanguage(rel),
				Qualified: qualified,
				File:      rel,
				StartLine: piece.StartLine,
				EndLine:   piece.EndLine,
				UpdatedAt: contentVersion(rel, piece.Text),
			}
			var text strings.Builder
			fmt.Fprintf(&text, "Repository file chunk\nLanguage: %s\nKind: %s\nPath: %s\n",
				entry.Language, piece.Kind, rel)
			if piece.Heading != "" {
				fmt.Fprintf(&text, "Section: %s\n", piece.Heading)
			}
			fmt.Fprintf(&text, "Lines: %d-%d\nSource:\n%s", piece.StartLine, piece.EndLine, piece.Text)
			entry.Lexical = truncateRunes(text.String(), lexicalTextRunes)
			out = append(out, node{Entry: entry, Text: text.String()})
		}
	}
	return out
}

func searchableRepositoryPath(rel string) bool {
	lower := strings.ToLower(filepath.ToSlash(rel))
	base := strings.ToLower(filepath.Base(lower))
	for _, part := range []string{"/node_modules/", "/vendor/", "/dist/", "/build/", "/.gradle/"} {
		if strings.Contains("/"+lower, part) {
			return false
		}
	}
	if isTestPath(lower) || strings.HasSuffix(base, ".lock") || base == "go.sum" ||
		base == "pnpm-lock.yaml" || base == "package-lock.json" || base == "yarn.lock" {
		return false
	}
	// Do not turn tracked credential material into an automatically surfaced
	// retrieval unit. Example/template env files remain eligible documentation.
	if base == ".env" || strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") ||
		strings.HasSuffix(base, ".p12") || strings.HasSuffix(base, ".pfx") {
		return false
	}
	if base == "makefile" || strings.HasPrefix(base, "dockerfile") {
		return true
	}
	switch filepath.Ext(base) {
	case ".go", ".kt", ".kts", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".py", ".rs", ".swift", ".java",
		".c", ".cc", ".cpp", ".h", ".hpp", ".sh", ".bash", ".zsh",
		".md", ".markdown", ".txt", ".yml", ".yaml", ".json", ".json5", ".jsonc", ".toml",
		".gradle", ".properties", ".service", ".timer", ".socket", ".conf", ".xml", ".html", ".css":
		return true
	default:
		return false
	}
}

func isTestPath(lower string) bool {
	base := filepath.Base(lower)
	rooted := "/" + lower
	return strings.Contains(rooted, "/test/") || strings.Contains(rooted, "/tests/") ||
		strings.Contains(rooted, "/testdata/") || strings.Contains(rooted, "/commontest/") ||
		strings.Contains(rooted, "/androidtest/") || strings.Contains(rooted, "/jvmtest/") ||
		strings.HasSuffix(base, "_test.go") || strings.Contains(base, ".test.") ||
		strings.Contains(base, ".spec.") || strings.HasSuffix(base, "test.kt") ||
		(strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py")) || strings.HasSuffix(base, "_test.py")
}

func repositoryLanguage(rel string) string {
	base := strings.ToLower(filepath.Base(rel))
	if base == "makefile" {
		return "make"
	}
	if strings.HasPrefix(base, "dockerfile") {
		return "dockerfile"
	}
	switch filepath.Ext(base) {
	case ".md", ".markdown":
		return "markdown"
	case ".sh", ".bash", ".zsh":
		return "shell"
	case ".yml", ".yaml":
		return "yaml"
	case ".json", ".json5", ".jsonc":
		return "json"
	case ".service", ".timer", ".socket":
		return "systemd"
	default:
		ext := strings.TrimPrefix(filepath.Ext(base), ".")
		if ext == "" {
			return "text"
		}
		return ext
	}
}

func contentVersion(path, text string) int64 {
	sum := sha256.Sum256([]byte(codePreprocessing + "\x00" + path + "\x00" + text))
	return int64(binary.LittleEndian.Uint64(sum[:8]) & (^uint64(0) >> 1))
}

func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}
