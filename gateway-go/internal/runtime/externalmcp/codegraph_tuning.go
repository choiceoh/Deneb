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
	"regexp"
	"strings"

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

// tuneCodegraphTool wraps one codegraph tool's call. Only codegraph_explore is
// tuned; every other tool is returned unchanged.
func tuneCodegraphTool(
	remote string,
	base toolport.ToolFunc,
	call func(ctx context.Context, name string, args json.RawMessage) (string, error),
) toolport.ToolFunc {
	if remote != codegraphExploreTool {
		return base
	}
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		sym, ok := singleSymbolExploreQuery(input)
		if !ok {
			return base(ctx, input) // multi-token / NL / area term → real exploration
		}
		nodeArgs, err := json.Marshal(map[string]any{"symbol": sym, "includeCode": true})
		if err != nil {
			return base(ctx, input)
		}
		out, err := call(ctx, codegraphNodeTool, nodeArgs)
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
