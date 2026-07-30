package core

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/filesystem"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/runtimeops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/surface"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/media"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/ops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/schema"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/webtools"
)

// RegisterFileTools registers the workspace file read/write/edit/grep surface.
// extraReadRoots are curated read-only roots outside the workspace (skill
// catalogs, the memory root) honored by read and grep; write/edit stay
// workspace-jailed.
func RegisterFileTools(registry toolport.ToolRegistrar, workspaceDir string, extraReadRoots ...string) {
	registry.RegisterTool(toolport.ToolDef{
		Name:        "read",
		Description: "Read file contents with line numbers for code review (default: 2000 lines). Use offset/limit for large files; equivalent to a clean bat/cat -n view",
		InputSchema: schema.ReadToolSchema(),
		Fn:          filesystem.ToolRead(workspaceDir, extraReadRoots...),
	})
	registry.RegisterTool(toolport.ToolDef{
		Name:        "write",
		Description: "Create or overwrite a file. Auto-creates parent directories. Use edit for partial changes",
		InputSchema: schema.WriteToolSchema(),
		Fn:          filesystem.ToolWrite(workspaceDir),
	})
	// Deferred (prompt audit 2026-06-12): ~370 wire tokens for 2 uses in 14
	// days — Deneb is a chief-of-staff, not a coding agent, so partial file
	// edits are rare. read/write/grep stay eager; an editing turn fetches this.
	registry.RegisterTool(toolport.ToolDef{
		Name:        "edit",
		Description: "Search-and-replace in a file. old_string must be unique unless replace_all=true. Read first to find the exact string",
		InputSchema: schema.EditToolSchema(),
		Fn:          filesystem.ToolEdit(workspaceDir),
		Deferred:    true,
	})
	registry.RegisterTool(toolport.ToolDef{
		Name:        "grep",
		Description: "Regex search across files (rg / ripgrep). Use include/fileType to narrow scope. Returns file:line:match format",
		InputSchema: schema.GrepToolSchema(),
		Fn:          filesystem.ToolGrep(workspaceDir, extraReadRoots...),
	})
}

// RegisterGraphTool registers the deferred graph-navigation tool.
func RegisterGraphTool(registry toolport.ToolRegistrar, workspaceDir string) {
	// Graphify: knowledge-graph queries over the wiki concept graph (people,
	// projects, deals, decisions, etc.) built by the wiki dreamer each cycle.
	// Deferred: at ~1,200 tokens this was the single largest eager tool on the
	// wire (prompt audit 2026-06-12) while most turns never touch the graph.
	// The deferred listing shows the first sentence (80-char truncation); the
	// full usage-pattern coaching below ships with the schema at fetch_tools
	// time — exactly when the model is about to use it. No automation prompt
	// directs graphify by name, so the fetch round-trip only ever lands on
	// interactive turns (cf. heartbeat_update's eager rationale).
	registry.RegisterTool(toolport.ToolDef{
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
		InputSchema: schema.GraphifyToolSchema(),
		Fn:          surface.ToolGraphify(workspaceDir),
		Deferred:    true,
	})
}

// RegisterCodeSearchTool registers the eager `code_search` tool — semantic
// (concept) code search: Nemotron embeddings over symbols/repository chunks,
// RRF-fused with BM25+FTS and reranked by XProvence. It is the sibling of codegraph_explore:
// CodeGraph resolves structure/relations from a KNOWN symbol; code_search finds
// "where is the code that does X" when the symbol name is unknown. Eager (small
// schema, one required field) so it sits on the wire without a fetch round-trip,
// matching codegraph_explore's always-available ergonomics.
func RegisterCodeSearchTool(registry toolport.ToolRegistrar, workspaceDir string) {
	registry.RegisterTool(toolport.ToolDef{
		Name: "code_search",
		Description: "시맨틱 코드 검색 — 심볼 이름을 몰라도 \"무엇을 하는 코드가 어디 있나\"를 개념/자연어로 찾는다 " +
			"(Nemotron dense + BM25 + CodeGraph FTS 융합 + 리랭크, 한국어 질의 지원). " +
			"파싱되지 않는 배포 스크립트·설정·Markdown도 검색하며, 상위 결과의 실제 소스·안전한 인접 관계·적용 CLAUDE/AGENTS/README 섹션을 한 번에 반환한다. " +
			"더 깊은 전체 호출 그래프가 필요할 때만 codegraph_node/explore로 이어서 파고들라.",
		InputSchema: schema.CodeSearchToolSchema(),
		Fn:          surface.ToolCodeSearch(workspaceDir),
	})
}

// RegisterOfficeTool registers the eager `office` tool that reads and edits
// Office documents (.docx/.xlsx/.pptx) through the officecli binary, with no
// Office install required.
//
// Eager (not Deferred): the operator's core workflow is document work — reading
// received .xlsx/.docx, filling templates, extracting figures — so the tool
// should be on the wire without a fetch_tools round-trip. Its schema is compact
// (one enum + three fields), unlike graphify's ~1,200-token coaching block.
//
// NOTE on mutation classification: office is deliberately NOT added to
// toolport.mutationTools. That set invalidates the workspace grep cache after a
// write, but office operates on binary Office files (usually outside the
// workspace) that no grep result ever indexed — coarse per-name invalidation
// would only bust the cache on read-only view/get calls for no benefit.
func RegisterOfficeTool(registry toolport.ToolRegistrar, workspaceDir string) {
	registry.RegisterTool(toolport.ToolDef{
		Name: "office",
		Description: "Office 문서(.docx·.xlsx·.pptx) 읽기·편집 — Office 설치 없이 officecli로 구동. " +
			"**읽기**: view(모드 outline/stats/text/annotated/issues/html/screenshot 등)·get(경로로 노드)·query(CSS 유사 셀렉터)·validate(스키마 검사)·dump(하위트리→재생 batch)·raw(부품 원본 XML)·help(스키마 레퍼런스). " +
			"**쓰기**: create(빈 문서)·set(--prop로 속성)·add/remove/move/swap(요소)·import(CSV/TSV→xlsx)·merge(템플릿 {{key}}+JSON)·batch(한 번에 다건). " +
			"경로는 1-based — 셀은 /<시트명>/<A1참조>(예: /Sheet1/A3), 구조 노드는 인덱스형(예: /slide[1]/shape[2], /body/paragraph[1]). " +
			"수식은 자동 평가(350+ Excel 함수) — set args=[\"/Sheet1/B2\",\"--prop\",\"formula=SUM(B3:B9)\"]. " +
			"각 명령은 독립 실행이라 반환 시 이미 디스크에 반영됨(read/다운로드가 즉시 최신본). " +
			"정확한 verb·요소·--prop 키는 command=help로 조회(예: args=[\"xlsx\",\"set\",\"cell\"]).",
		InputSchema: schema.OfficeToolSchema(),
		Fn:          surface.ToolOffice(workspaceDir),
	})
}

// NewGoalGlanceFunc builds the ambient standing-goal glance for the dynamic
// system-prompt block. Re-exported so the chat parent does not import
// domain/goals or the tools package solely for ambient wiring.
func NewGoalGlanceFunc() func(ctx context.Context, sessionKey string) string {
	return surface.NewGoalGlanceFunc()
}

// HandleGoalCommand processes the /goal slash command against the process goal
// store. Re-exported so chat slash dispatch does not import domain/goals.
func HandleGoalCommand(sessionKey, args string, respond func(text string)) {
	surface.HandleGoalCommand(sessionKey, args, respond)
}

// RegisterCoreTools populates the tool registrar with all core agent tools.
// It delegates to domain-specific Register*Tools functions.
func Register(registry toolport.ToolRegistrar, deps *tooldeps.CoreToolDeps) {
	// Extra read-only roots outside the workspace: skill catalogs plus the
	// memory root (capture originals referenced by oversized-document digests).
	extraReadRoots := deps.SkillsCatalogDirs
	if deps.MemoryDir != "" {
		extraReadRoots = append(append([]string(nil), extraReadRoots...), deps.MemoryDir)
	}
	RegisterFileTools(registry, deps.WorkspaceDir, extraReadRoots...)
	ops.RegisterRuntimeOps(registry, ops.RuntimeOpsDeps{
		WorkspaceDir:   deps.WorkspaceDir,
		ObserveTool:    deps.ObserveTool,
		Fleet:          deps.Fleet,
		Browser:        deps.Browser,
		WikiStore:      deps.Wiki.Store,
		SpilloverStore: deps.SpilloverStore,
	})
	RegisterGraphTool(registry, deps.WorkspaceDir)
	RegisterCodeSearchTool(registry, deps.WorkspaceDir)
	RegisterOfficeTool(registry, deps.WorkspaceDir)
	ops.RegisterProcessTools(registry, &deps.Process)
	webtools.Register(registry, deps.SpilloverStore)
	ops.RegisterSessionTools(registry, &deps.Sessions)
	RegisterChronoTools(registry)
	media.RegisterMediaTools(registry, deps.WorkspaceDir, deps.SpilloverStore)
	ops.RegisterPhoneTools(registry, deps.PhoneActionSender)
	ops.RegisterWorkstationTool(registry, ops.WorkstationDeps{
		Send: deps.WorkstationCommandSender,
		Hint: deps.WorkstationUsageHint,
	})

	// Standing goal (Ralph loop). Eager: the agent must discover it to set a
	// goal on a multi-step request. Once set, the server's goalTask advances it
	// one run per idle tick, judges completion with the lightweight model, and a
	// per-goal idempotency ledger blocks repeated destructive actions.
	registry.RegisterTool(toolport.ToolDef{
		Name:        "goal",
		Description: "다단계·장기 작업을 여러 턴에 걸쳐 끝까지 진행해야 할 때 표준 목표(standing goal)를 설정·관리한다. action=set(목표 설정) | subgoal(완료 기준 추가) | status | pause | resume | stop. 설정하면 사용자가 자리를 비운 동안 자동으로 한 단계씩 진행하고 완료를 판정한다. 이미 실행한 작업은 멱등 가드로 중복되지 않는다.",
		InputSchema: schema.GoalToolSchema(),
		Fn:          surface.ToolGoal(),
	})

	// Typed blackboard: fail-closed I/O contracts for multi-tool workflows.
	// Prefer named keys over free-text handoffs between mail/wiki/web/etc.
	registry.RegisterTool(toolport.ToolDef{
		Name: "blackboard",
		Description: "크로스툴·다단계 워크플로에서 중간값을 typed key로 넘긴다(free-text 요약 대체). " +
			"action=plan(steps[{id,goal,inputs,outputs}]) → begin(step) → 작업 → end(step,outputs) | put/get/require/list/clear. " +
			"필수 입력·출력이 없으면 실패로 닫힌다. 다른 툴 인자에는 문자열 \"$board.<key>\"로 주입 가능.",
		InputSchema: schema.BlackboardToolSchema(),
		Fn:          surface.ToolBlackboard(),
	})

	// Research panel: fan a question out to every healthy model in parallel
	// (deep-research skill). Deferred — only deliberate deep research needs it,
	// so interactive turns don't pay for the schema. nil ConsultPanel (no model
	// registry / router wired) leaves the tool unregistered.
	if deps.ConsultPanel != nil {
		registry.RegisterTool(toolport.ToolDef{
			Name:        "research_panel",
			Description: "딥리서치·고위험 의사결정의 교차검증용 — 하나의 질문을 가동 중(헬시)인 모든 모델에게 병렬로 던져 모델별 답을 모아 온다(이종 모델 패널 팬아웃). 반환된 모델별 답을 당신이 직접 종합하라 — 서로 다른 계열이 합의하면 강한 신뢰, 모순은 명시하고, 자신만만한 답에 닻 내리지 말 것. 단순 사실질문엔 쓰지 마라(비용이 모델 수만큼 N배). models로 특정 모델만 지정 가능, 비우면 전체.",
			InputSchema: schema.ResearchPanelToolSchema(),
			Fn:          surface.ToolResearchPanel(deps.ConsultPanel),
			Deferred:    true,
		})
	}

	// Work-feed browse/settle: the agent's read surface over its OWN proactive
	// cards (native-sync-teeing wrapper, so agent reads/acks mirror to the
	// phone). Deferred — needed only when reviewing past nudges. nil = feed off.
	if deps.WorkFeedRW != nil {
		registry.RegisterTool(toolport.ToolDef{
			Name:        "workfeed",
			Description: "작업 피드(업무 피드) 도구 — 카드를 조회·정리하고, 요청받은 산출물을 카드로 발행한다. action=list(미처리 카드 목록) | read(id로 본문 — 열람 표시 겸함) | ack(처리 완료 표시) | publish(문서·계약서 검토처럼 사용자가 요청한 산출물을 작업 피드 카드로 발행 — title+body 필수, 위키에만 묻지 말고 사용자에게 딜리버). '이번 주 능동 알림 뭐 보냈지'·'그 카드 처리 표시'에도 사용. 집계 통계는 observe action=proactive.",
			InputSchema: schema.WorkfeedToolSchema(),
			Fn:          surface.ToolWorkFeed(deps.WorkFeedRW),
			Deferred:    true,
		})
	}

	media.RegisterExtractionTools(registry, deps.AsrHotwords)

	// Market quotes: same cache as the miniapp 오늘 dashboard (원/달러·코스피·
	// WTI·구리, 10m TTL). Deferred; nil = dashboard cache not wired.
	if deps.MarketSummary != nil {
		registry.RegisterTool(toolport.ToolDef{
			Name:        "market",
			Description: "시장 시세 스냅샷 — 원/달러 환율·코스피·WTI 유가·구리(LME) 현재가와 전일 대비 등락. '환율 지금 얼마'·'구리 시세 어때' 류 질문에 사용. 인자 없음, 10분 캐시.",
			InputSchema: schema.MarketToolSchema(),
			Fn:          surface.ToolMarket(surface.MarketSummaryFunc(deps.MarketSummary)),
			Deferred:    true,
		})
	}

	// Org chart (read-only): the operator-curated group→company→team tree with
	// 직급/직책. Deferred; loads {stateDir}/org.json on demand — empty file just
	// reports unset, so no dep/nil-guard needed.
	registry.RegisterTool(toolport.ToolDef{
		Name: "org",
		Description: "큐레이션 조직도(읽기 전용, org.json) — '1팀 팀장 누구'·'회사 조직 어떻게 되지'·직급/직책. " +
			"query로 사람/팀/회사 검색, 생략 시 전체 트리. 라이브 휴대폰·부서·생년월일은 groupware(area=people); 번호↔이름은 contacts.",
		InputSchema: schema.OrgToolSchema(),
		Fn:          surface.ToolOrg(),
		Deferred:    true,
	})

	// NOTE: fetch_tools and code_action are registered by
	// RegisterRegistryBridgeTools (called from chat.RegisterCoreTools) because
	// they need the concrete registry surface (FetchToolsRegistry / ToolInvoker).
}

// RegisterChronoTools registers messaging tools (non-periodic).
//
// message is deferred (prompt audit 2026-06-12): 1 use in 14 days for its wire
// tokens. Normal replies auto-route as the turn's final text, so message only
// matters for rare mid-turn/proactive sends. Its usage protocol moved from the
// dynamic Messaging block into the description below — it ships at fetch_tools
// time, exactly when the model has the tool in hand (graphify pattern). The boot
// prompt is the one automation that names message, and it already runs with
// fetch_tools in its preset.
func RegisterChronoTools(registry toolport.ToolRegistrar) {
	registry.RegisterTool(toolport.ToolDef{
		Name: "message",
		Description: "Send messages to the user's channel. Actions: send, reply, react, thread-reply. Use for proactive sends. " +
			"**사용자가 방금 보낸 메시지에 대한 응답에는 절대 쓰지 마라** — 일반 응답은 턴의 최종 텍스트가 자동 전달된다. " +
			"이 도구로 사용자에게 보일 내용을 이미 전송했다면, 중복 전달을 막기 위해 턴의 최종 텍스트는 정확히 NO_REPLY 한 단어만 출력하라(다른 텍스트와 섞지 말 것).",
		InputSchema: schema.MessageToolSchema(),
		Fn:          surface.ToolMessage(),
		Deferred:    true,
	})
	registry.RegisterTool(toolport.ToolDef{
		Name: "heartbeat_update",
		Description: "Overwrite ~/.deneb/HEARTBEAT.md with a new full content string. Pass empty content to clear the file. " +
			"Used by the 30-minute autonomous heartbeat to retire completed/cancelled items, update progress notes, " +
			"and archive stalled items. Also callable by the user via natural language (\"add X to my heartbeat\", \"remove the spark deploy task\"). " +
			"Auto-backs up the prior content to HEARTBEAT.md.prev. Eager registration: the autonomous heartbeat " +
			"trigger explicitly directs the agent to call this tool, so it must be visible in the default prompt " +
			"(deferring it would force a fetch_tools round-trip and add a fragile turn).",
		InputSchema: schema.HeartbeatUpdateToolSchema(),
		Fn:          runtimeops.ToolHeartbeatUpdate(),
	})
}
