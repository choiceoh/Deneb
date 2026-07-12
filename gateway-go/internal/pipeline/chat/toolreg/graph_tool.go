package toolreg

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
)

// RegisterGraphTool registers the deferred graph-navigation tool.
func RegisterGraphTool(registry toolctx.ToolRegistrar, workspaceDir string) {
	// Graphify: knowledge-graph queries over the wiki concept graph (people,
	// projects, deals, decisions, etc.) built by the wiki dreamer each cycle.
	// Deferred: at ~1,200 tokens this was the single largest eager tool on the
	// wire (prompt audit 2026-06-12) while most turns never touch the graph.
	// The deferred listing shows the first sentence (80-char truncation); the
	// full usage-pattern coaching below ships with the schema at fetch_tools
	// time — exactly when the model is about to use it. No automation prompt
	// directs graphify by name, so the fetch round-trip only ever lands on
	// interactive turns (cf. heartbeat_update's eager rationale).
	registry.RegisterTool(toolctx.ToolDef{
		Name: "graphify",
		Description: "위키 지식 그래프 질의 (사람·프로젝트·거래·결정·선호 등 개념/관계 그래프, dreamer가 매 사이클 갱신). " +
			"graph=\"wiki\"(기본, ~/.deneb/wiki-graph) | graph=\"code\"(코드 호출/import/contains 그래프, `graphify update .`로 빌드, workspace/graphify-out). " +
			"액션: query (자연어 질문 → 관련 노드 탐색), explain (한 노드와 이웃 요약), path (두 노드 간 최단 경로). " +
			"**사용 패턴:** " +
			"(a) 단순 검색이 아니라 **그래프 탐색**으로 사고하라 — query로 후보 노드를 찾고 explain으로 이웃을 펼친 뒤 path로 다른 영역과 연결. " +
			"(b) explain 결과의 community 번호를 활용하라 — 같은 community 안의 노드는 의미적으로 한 묶음. " +
			"(c) 단발 질의로 끝내지 마라 — 한 질문에 query/explain/path를 2~3회 chaining해 답을 입체화. " +
			"(d) wiki search보다 graphify가 강한 상황: 관계·맥락·연쇄 추론이 필요할 때 (단순 키워드 룩업은 wiki/grep로 충분). " +
			"(e) wiki + code 두 그래프를 묶어서 답하라 — \"이 함수가 어떤 개념을 구현하나\"면 code에서 함수 노드 explain 후 wiki에서 같은 개념을 query.",
		InputSchema: graphifyToolSchema(),
		Fn:          tools.ToolGraphify(workspaceDir),
		Deferred:    true,
	})
}
