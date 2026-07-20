// codegraph_tuning.go — Deneb-side search tuning for the codegraph MCP server.
//
// codegraph exposes the runtime agent's own source as deferred codegraph_*
// tools (mcp-servers.conf), but its ranking is a black box with no precision
// knobs (query/explore take only limit/max-files). Its `explore` fuzzily
// decomposes a lone PascalCase token into sub-tokens — measured: `explore
// GatewayHub` returns 71 symbols across 3 files, dragging in the unrelated
// localai `Hub` and the Android `GatewayTab` (Gateway→Tab). `node GatewayHub`
// returns just that struct + its 25 members, no noise.
//
// So when explore is called with a SINGLE specific-looking symbol, we reroute
// to node — the precise answer the caller wanted. Multi-token / NL explores,
// lowercase area terms, and every other codegraph tool pass through untouched;
// a node miss falls back to the original explore, so a reroute never drops
// results. This is the enforcement half of the CLAUDE.md guidance ("정확한
// 심볼엔 explore보다 node 우선").
package externalmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/codesearch"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// codegraphServerName is the DENEB_MCP_SERVERS namespace whose tools get tuned.
const codegraphServerName = "codegraph"

// codegraphExploreTool / codegraphNodeTool are the remote MCP tool names (the
// reroute target must be reachable on the same client).
const (
	codegraphExploreTool = "codegraph_explore"
	codegraphNodeTool    = "codegraph_node"
)

// codegraphNodeMiss is the substring codegraph_node returns for an unknown
// symbol (`Symbol "X" not found in the codebase`). Matching it lets a reroute
// fall back to explore instead of surfacing a bare miss.
const codegraphNodeMiss = "not found in the codebase"

// codegraphSymbolRe matches a bare identifier (no dots, spaces, or operators),
// min 3 chars. Dotted names (RPC methods) and multi-word queries are excluded.
var codegraphSymbolRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{2,}$`)

// tuneCodegraphTool wraps one codegraph tool's call with two Deneb-side tunings:
//   - reroute: a single-symbol explore → node (precision);
//   - enrich: append the nearest folder's CLAUDE.md map to explore/node results,
//     so the runtime agent gets the folder's INTENT (role/rules/gotchas) alongside
//     codegraph's STRUCTURE. The runtime prompt loads NEITHER root nor subtree
//     CLAUDE.md (context_files.go), so this is the only place the agent sees them.
//
// Keyed on the tool's KIND, not its exact remote name: codegraph self-prefixes
// (codegraph_explore/codegraph_node), but a server that advertises the plain
// explore/node still gets tuned. Every other codegraph tool is passed through.
func tuneCodegraphTool(
	remote string,
	base toolport.ToolFunc,
	call func(ctx context.Context, name string, args json.RawMessage) (string, error),
) toolport.ToolFunc {
	kind := strings.TrimPrefix(remote, "codegraph_")
	inner := base
	if kind == "explore" {
		// Derive the sibling node tool from this remote's own prefix scheme
		// (codegraph_explore→codegraph_node, explore→node) so the reroute dials
		// the tool the same server actually advertises.
		nodeTarget := strings.TrimSuffix(remote, "explore") + "node"
		inner = exploreRerouteFn(base, call, nodeTarget)
	}
	// Only the source-returning tools carry file paths worth mapping to a folder.
	if kind != "explore" && kind != "node" {
		return inner
	}
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		out, err := inner(ctx, input)
		if err != nil || strings.TrimSpace(out) == "" {
			return out, err
		}
		// os.Getwd failure returns an empty dir, which enrichWithFolderDocs
		// treats as "no root → no enrichment" — so a bare read is safe and the
		// tool call never fails on it.
		root, _ := os.Getwd()
		return enrichWithFolderDocsQuery(out, root, codegraphContextQuery(input), os.ReadFile), nil
	}
}

func codegraphContextQuery(input json.RawMessage) string {
	var p struct {
		Query  string `json:"query"`
		Symbol string `json:"symbol"`
	}
	if json.Unmarshal(input, &p) != nil {
		return ""
	}
	if strings.TrimSpace(p.Query) != "" {
		return p.Query
	}
	return p.Symbol
}

// exploreRerouteFn reroutes a single specific-symbol explore to node (nodeTarget
// is the sibling node tool's remote name).
func exploreRerouteFn(
	base toolport.ToolFunc,
	call func(ctx context.Context, name string, args json.RawMessage) (string, error),
	nodeTarget string,
) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		sym, ok := singleSymbolExploreQuery(input)
		if !ok {
			return base(ctx, input) // multi-token / NL / area term → real exploration
		}
		nodeArgs, err := json.Marshal(map[string]any{"symbol": sym, "includeCode": true})
		if err != nil {
			return base(ctx, input)
		}
		out, err := call(ctx, nodeTarget, nodeArgs)
		if err != nil || strings.TrimSpace(out) == "" || strings.Contains(out, codegraphNodeMiss) {
			return base(ctx, input) // not a known symbol → the caller meant to explore
		}
		return codegraphRerouteNote(sym) + out, nil
	}
}

// singleSymbolExploreQuery reports whether an explore payload is really a lone
// SPECIFIC symbol lookup — one bare identifier that carries an uppercase letter
// or underscore (PascalCase/camelCase/snake_case, i.e. a named symbol like
// GatewayHub or run_agent), the case node answers without sub-token noise. A
// lowercase area word ("auth", "polaris") is left to explore even if it happens
// to name a symbol, because there the caller usually wants the surrounding area.
func singleSymbolExploreQuery(input json.RawMessage) (string, bool) {
	var p struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return "", false
	}
	q := strings.TrimSpace(p.Query)
	if !codegraphSymbolRe.MatchString(q) {
		return "", false
	}
	if !strings.ContainsAny(q, "ABCDEFGHIJKLMNOPQRSTUVWXYZ_") {
		return "", false
	}
	return q, true
}

// codegraphRerouteNote tells the caller its lone-symbol explore was answered by
// node, and how to force a real area exploration when that is what it wanted.
func codegraphRerouteNote(sym string) string {
	return fmt.Sprintf(
		"[codegraph: %q는 단일 심볼이라 explore 대신 node로 정밀 조회했다(서브토큰 노이즈 제거). 주변 영역·여러 심볼·흐름을 보려면 공백으로 구분한 여러 토큰이나 질문 문장으로 explore를 다시 불러라.]\n\n",
		sym,
	)
}

// --- Folder-doc enrichment: attach the nearest CLAUDE.md subtree map ---

const (
	// maxFolderDocs caps how many distinct folder maps ride one result — the
	// top few by result order (relevance); more would crowd the model's budget.
	maxFolderDocs = 3
	// perFolderDocCap caps one map's attached bytes. Subtree maps run 1–15 KB;
	// the role/intro + directory table (what a searcher needs) lead the file, so
	// a head-truncation keeps the useful part. Budgeted for a local model.
	perFolderDocCap = 1000
)

// sourcePathRe matches a repo-relative source path in codegraph output
// (`gateway-go/internal/…/file.go`), across the languages codegraph indexes.
var sourcePathRe = regexp.MustCompile(`[A-Za-z0-9_][\w./-]*\.(?:go|kt|kts|ts|tsx|js|jsx|rs|py|java|swift|c|cc|cpp|h|hpp)`)

// enrichWithFolderDocs preserves the historical test/helper surface. Runtime
// calls enrichWithFolderDocsQuery so the attached Markdown is section-ranked
// against the actual CodeGraph query rather than blindly head-truncated.
func enrichWithFolderDocs(out, root string, readFile func(string) ([]byte, error)) string {
	return enrichWithFolderDocsQuery(out, root, "", readFile)
}

// enrichWithFolderDocsQuery appends applicable CLAUDE/AGENTS rules and nearby
// query-relevant repository documentation for result paths. Selection is
// hierarchy-aware, deduplicated, UTF-8 safe, and bounded. Fail-open: no paths,
// docs, or reads leaves the CodeGraph result unchanged.
func enrichWithFolderDocsQuery(out, root, query string, readFile func(string) ([]byte, error)) string {
	if root == "" {
		return out
	}
	seenPath := map[string]bool{}
	var sourcePaths []string
	for _, m := range sourcePathRe.FindAllString(out, -1) {
		// Need a folder to map; skip bare basenames + repeats. Reject any `..`:
		// the path comes from external tool output, and a `..` segment could
		// escape root once joined (path traversal). Fail-open — just skip it.
		if !strings.Contains(m, "/") || strings.Contains(m, "..") || seenPath[m] {
			continue
		}
		seenPath[m] = true
		sourcePaths = append(sourcePaths, m)
	}
	docs := codesearch.ApplicableRepositoryDocs(root, query, sourcePaths, readFile, maxFolderDocs, perFolderDocCap)
	if len(docs) == 0 {
		return out
	}
	var b strings.Builder
	b.WriteString(out)
	b.WriteString("\n\n## 폴더 맥락 (적용 문서)\n")
	b.WriteString("검색 결과 경로에 적용되는 규칙과 질의 관련 섹션이다. rules는 제약, reference는 근거다.\n")
	for _, d := range docs {
		fmt.Fprintf(&b, "\n### %s (%s)\n%s\n", d.Path, d.Role, d.Content)
	}
	return b.String()
}

// nearestClaudeMd walks up from a repo-relative dir to the closest CLAUDE.md,
// falling back to the repo-root CLAUDE.md as a last resort — the runtime prompt
// loads none of these (context_files.go), so root is the only applicable map for
// areas without a subtree one (scripts/, top-level files). Returns the
// repo-relative map path + its content, or "","" if even root has none.
func nearestClaudeMd(dir, root string, readFile func(string) ([]byte, error)) (string, string) {
	for {
		rel := "CLAUDE.md"
		atRoot := dir == "." || dir == "/" || dir == ""
		if !atRoot {
			rel = dir + "/CLAUDE.md"
		}
		if b, err := readFile(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			return rel, string(b)
		}
		if atRoot {
			return "", ""
		}
		dir = path.Dir(dir)
	}
}

// capHead truncates to n bytes, preferring a line boundary, else backing off to
// a UTF-8 rune boundary so a multibyte rune (subtree maps carry Korean) is never
// cut in half. Notes the cut so the model knows the map continues in the file.
func capHead(s string, n int) string {
	if len(s) <= n {
		return strings.TrimRight(s, "\n")
	}
	cut := s[:n]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i] // '\n' is a rune boundary — safe
	} else {
		// No line break in the window: back off until the next byte starts a
		// rune, so cut ends on a boundary and stays valid UTF-8.
		for len(cut) > 0 && !utf8.RuneStart(s[len(cut)]) {
			cut = cut[:len(cut)-1]
		}
	}
	return strings.TrimRight(cut, "\n") + "\n…(생략 — 전문은 해당 CLAUDE.md 참조)"
}
