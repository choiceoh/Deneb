package recall

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/recallops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/schema"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

// RegisterPolarisTools registers the unified Polaris tool (search/describe/expand).
// Called separately because the store and localAI are not part of CoreToolDeps.
func RegisterPolarisTools(registry toolport.ToolRegistrar, store *polaris.Store, localAI recallops.LocalAIFunc) {
	if store == nil {
		return
	}
	registry.RegisterTool(toolport.ToolDef{
		Name: "polaris",
		Description: "현재 세션의 압축된 과거 대화 회상 (모든 메시지가 SQLite FTS에 무손실 저장). " +
			"사용자가 컨텍스트에 없는 합의·숫자·인물·결정 또는 '아까 그거'·'지난번에' 같은 참조를 언급하면 " +
			"짐작하지 말고 먼저 호출하라. " +
			"action=search(키워드 검색) → describe(압축 요약 구간 ID 목록, time_range로 today/this_week/all) → " +
			"expand(특정 summary_id 원문 복원, question 추가 시 LLM이 원문 기반 답변). " +
			"`<recall-context>` 자동 주입은 첫 턴 cue 기반 preflight 한 번뿐이므로, 턴 도중 새 회상이 필요하면 이 도구를 직접 호출하라.",
		InputSchema: schema.PolarisToolSchema(),
		Fn:          recallops.ToolPolaris(store, localAI),
	})
}

// RegisterKnowledgeTool registers the knowledge tool over the wiki knowledge
// base behind one agent surface. Called separately because the knowledge
// router needs the wiki Store at construction time.
//
// Pass-through behavior: if router is nil (no backends configured) the tool
// is not registered so the agent does not see a dead surface.
func RegisterKnowledgeTool(registry toolport.ToolRegistrar, router *knowledge.Router) {
	if router == nil || len(router.Layers()) == 0 {
		return
	}
	registry.RegisterTool(toolport.ToolDef{
		Name: "knowledge",
		Description: "지식·기억 도구. 소스 카탈로그를 계획해 위키·파일을 의미+키워드로 검색·조회하고 위키를 기록. " +
			"op=recall(질의→소스 병렬 검색, sources/scopes로 명시 제한 가능, ref와 근거 문맥 머지) → " +
			"op=read(ref로 단건 fetch — `w:인물/박부장` 같이 prefix로 layer 자동 라우팅) → " +
			"op=record(wiki에 큐레이션 페이지 작성·갱신). " +
			"polaris(현재 세션 회상)·graphify(개념 그래프)는 별개 도구로 분리됨 — paradigm이 다름.",
		InputSchema: schema.KnowledgeToolSchema(),
		Fn:          recallops.ToolKnowledge(router),
	})
}
