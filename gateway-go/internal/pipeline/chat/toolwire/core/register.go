package core

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/artifact"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/filesystem"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/runtimeops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/surface"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/schema"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/web"
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

// RuntimeOpsToolSet contains the already-wired executors whose registration
// policy belongs to the runtime-operations group. SpilloverRead is optional.
type RuntimeOpsToolSet struct {
	Gateway       toolport.ToolFunc
	Observe       toolport.ToolFunc
	Fleet         toolport.ToolFunc
	Browser       toolport.ToolFunc
	Groupware     toolport.ToolFunc
	Solarflow     toolport.ToolFunc
	SpilloverRead toolport.ToolFunc
}

// RegisterRuntimeOpsTools registers gateway diagnostics and host operations.
func RegisterRuntimeOpsTools(registry toolport.ToolRegistrar, set RuntimeOpsToolSet) {
	registry.RegisterTool(toolport.ToolDef{
		Name: "gateway",
		Description: "Gateway self-management: status (버전/PID/포트/업타임/세션 수를 한 번에 반환), config_get/config_set (dotted paths), update (git pull + rebuild + restart), restart. " +
			"Destructive actions (restart/update/config_set) require approval — the first call returns a needs_approval envelope; relay the Korean summary to the user verbatim, and after approval call the .confirmed variant with the same action_token. " +
			"토큰/비밀번호/API 키는 절대 config_set으로 건드리지 마라.",
		InputSchema: schema.GatewayToolSchema(),
		Fn:          set.Gateway,
		Deferred:    true,
	})
	registry.RegisterTool(toolport.ToolDef{
		Name:        "observe",
		Description: "Self-diagnosis: why a turn was slow/failed, tool-usage stats, improvement-loop health. Observe your OWN runtime via the in-process observation plane: action=turn (runId → a past run's tokens/tools/cache + its captured logs), action=logs (recent log ring; filter by runId/session/level/contains), action=behavior (cross-session tool usage / proactive funnel / background-job health over N days, plus the local vLLM engine's prefix-cache hit rate), action=effort (adaptive effort-router scorecard: routed-off vs kept-on, escalation rate, savings), action=proactive (proactive-card engagement: FTR / over-intervention rate by source), action=health (self-improvement machinery digest: loop liveness, skill-decision mix, dreamer backlog, no-op frontier, silent-failure counts — same data as the loopback /api/observatory, read mid-reasoning).",
		InputSchema: schema.ObserveToolSchema(),
		Fn:          set.Observe,
		Deferred:    true,
	})

	// Fleet: manage the SparkFleet GPU control plane (the machine's own model
	// servers) — the chat twin of the native 플릿 tab / the /api/v1/fleet
	// passthrough. Deferred like gateway/observe: niche but powerful, loaded via
	// fetch_tools when the user actually asks about the fleet.
	registry.RegisterTool(toolport.ToolDef{
		Name: "fleet",
		Description: "SparkFleet GPU 컨트롤 플레인 관리 — 이 머신의 GPU 모델 서버를 띄우고 점검한다. " +
			"action=status (노드 GPU/메모리·레시피 실행 상태·최근 실패 작업 한눈에) · recipes (모델 레시피 목록) · jobs (백그라운드 작업) · " +
			"launch/stop/restart (recipe 이름으로 모델 기동·중지·재시작 — 실제 동작) · cancel (jobId로 작업 취소) · diagnose (실행 중 레시피 컨테이너 크래시 진단). " +
			"\"플릿 괜찮아?\" · \"qwen36 재시작해줘\" · \"왜 죽었어?\" 같은 요청에 사용.",
		InputSchema: schema.FleetToolSchema(),
		Fn:          set.Fleet,
		Deferred:    true,
	})

	// Browser: operate the user's real Chrome via a workstation Page Agent
	// bridge (login sessions + SPA clicks). Deferred like fleet — niche but
	// powerful; fetch_tools when a turn needs interactive web control.
	registry.RegisterTool(toolport.ToolDef{
		Name: "browser",
		Description: "사용자 PC의 실제 Chrome 브라우저를 자연어로 조작한다 (Page Agent 브리지). " +
			"로그인된 SaaS·SPA처럼 `web`(HTTP fetch)으로 못 읽는 화면을 클릭·입력·스크롤한다. " +
			"action=status (허브 연결/작업 중 여부) · execute (task=자연어 지시, 블로킹) · stop (진행 중 작업 중단). " +
			"\"이 사이트에서 …해줘\" · \"로그인한 페이지에서 폼 채워\" 류에 사용. DENEB_BROWSER_URL 미설정 시 연동 꺼짐.",
		InputSchema: schema.BrowserToolSchema(),
		Fn:          set.Browser,
		Deferred:    true,
	})

	// Groupware: srv4 headless Amaranth — 전자결재·게시판·매출·재고·발주·입고·출고·단가·사원.
	// Eager: ops asks about 재고/출고/매출/사원 often; fetch_tools round-trip was pure latency.
	registry.RegisterTool(toolport.ToolDef{
		Name: "groupware",
		Description: "아마란스 읽기 — 트리거: '모듈 재고'→stock, 'YTD/당월 매출'→sales summary, '인버터 단가'→price, " +
			"'발주/입고/출고'→po|receive|ship, '김○○ 휴대폰/부서'→people, 미결·공지→approval|board. " +
			"action=status|list|read|attachment|summary · area=approval|board|sales|stock|po|receive|ship|price|people. " +
			"결재함 folder=pending|done|cc|total|all · ERP 기간 folder=ytd|month|today|year|last_year. " +
			"query=키워드 또는 YYYYMMDD:YYYYMMDD · price는 모듈→M-/인버터→I- · people는 이름 필수(위키 인물 보강·생성, org.json 읽기 매칭). " +
			"승인·반려·상신·전표 작성 금지. DENEB_GROUPWARE_USER/PASSWORD 미설정 시 꺼짐.",
		InputSchema: schema.GroupwareToolSchema(),
		Fn:          set.Groupware,
	})

	// Solarflow: read-only topsolar SolarFlow ERP analytics engine (/api/calc/*).
	// Complements groupware — derived intelligence (마진·미수금·LC·수급·회전율) +
	// a Korean natural-language search. Eager: same "재고/매출/미수금" ask surface
	// as groupware, so a fetch_tools round-trip would just be latency.
	if set.Solarflow != nil {
		registry.RegisterTool(toolport.ToolDef{
			Name: "solarflow",
			Description: "SolarFlow ERP 분석 조회(읽기 전용) — 아마란스 원장에서 파생된 인텔리전스. " +
				"자연어는 action=search(query=\"모듈 재고\"·\"미수금 많은 거래처\"). " +
				"구조화: inventory(재고집계) · margin(마진분석) · customer(거래처분석) · outstanding(미수금, customer=거래처명) · " +
				"turnover(재고회전율) · supply_forecast(수급전망) · lc_maturity(LC만기, horizon=일수) · lc_limit(한도) · lc_fee · " +
				"landed_cost(수입원가) · exchange_compare(환율비교) · price_trend(단가추이) · order_risk(수주충당위험) · receipt_match(수금매칭). " +
				"기간=horizon(정수) · 거래처는 customer(이름) 또는 customer_id(uuid). 기본 회사=탑솔라(DENEB_SOLARFLOW_COMPANY_ID). " +
				"승인·전표·쓰기 금지. groupware가 원장 원본이면 solarflow는 계산된 분석이다.",
			InputSchema: schema.SolarflowToolSchema(),
			Fn:          set.Solarflow,
		})
	}

	// Spillover: read full content of a previously spilled large tool result.
	// Registered eagerly so the trim marker's embedded spill ID can be used
	// in the same turn without a fetch_tools round-trip.
	if set.SpilloverRead != nil {
		registry.RegisterTool(toolport.ToolDef{
			Name:        "read_spillover",
			Description: "Read a previous large tool result by spill ID, paged — offset/limit line window (default 400 lines) or grep to jump to matching lines. Use when a tool result was too large and was replaced with a preview; follow the [계속: offset=N] tail hint to page",
			InputSchema: schema.ReadSpilloverToolSchema(),
			Fn:          set.SpilloverRead,
		})
	}
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
	observeFn := toolport.ToolFunc(deps.ObserveTool)
	if observeFn == nil {
		observeFn = toolport.ToolFunc(func(context.Context, json.RawMessage) (string, error) {
			return `{"ok":false,"error":"observe unavailable"}`, nil
		})
	}
	runtimeOps := RuntimeOpsToolSet{
		Gateway:   runtimeops.ToolGateway(deps.WorkspaceDir),
		Observe:   observeFn,
		Fleet:     runtimeops.ToolFleet(&deps.Fleet),
		Browser:   runtimeops.ToolBrowser(&deps.Browser),
		Groupware: runtimeops.ToolGroupware(deps.Wiki.Store),
		Solarflow: runtimeops.ToolSolarflow(),
	}
	if deps.SpilloverStore != nil {
		runtimeOps.SpilloverRead = artifact.ToolSpilloverRead(deps.SpilloverStore)
	}
	RegisterRuntimeOpsTools(registry, runtimeOps)
	RegisterGraphTool(registry, deps.WorkspaceDir)
	RegisterCodeSearchTool(registry, deps.WorkspaceDir)
	RegisterOfficeTool(registry, deps.WorkspaceDir)
	RegisterProcessTools(registry, &deps.Process)
	RegisterWebTools(registry, deps.SpilloverStore)
	RegisterSessionTools(registry, &deps.Sessions)
	RegisterChronoTools(registry)
	RegisterMediaTools(registry, deps.WorkspaceDir)
	RegisterPhoneTools(registry, deps.PhoneActionSender)
	RegisterWorkstationTool(registry, deps.WorkstationCommandSender, deps.WorkstationUsageHint)

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

	// Audio transcription: resident MOSS-Transcribe-Diarize ASR sidecar over a file on disk.
	// Deferred — capture RPCs cover app-shared audio; this is for files the
	// agent encounters itself (downloads, exec artifacts, file store).
	registry.RegisterTool(toolport.ToolDef{
		Name:        "transcribe",
		Description: "디스크의 오디오 파일(회의 녹음·음성 메모, m4a/mp3/oga/wav 등 최대 60분)을 화자분리+타임스탬프로 전사한다 — '이 녹음 정리해줘'에 사용. hotwords로 거래처·인명 교정 힌트 추가 가능(주소록/위키 힌트 자동 병합). 앱에서 공유된 오디오는 이미 자동 전사되므로 이 도구는 경로로 받은 파일용.",
		InputSchema: schema.TranscribeToolSchema(),
		Fn:          artifact.ToolTranscribe(deps.AsrHotwords),
		Deferred:    true,
	})

	// Document/image text extraction over a file on disk (PaddleOCR-VL +
	// tesseract fallback; born-digital PDFs via pdftotext). Deferred.
	registry.RegisterTool(toolport.ToolDef{
		Name:        "ocr",
		Description: "디스크의 이미지·스캔 PDF·오피스 문서에서 텍스트를 추출한다(OCR) — 영수증 사진·스캔 계약서·팩스 PDF를 읽어야 할 때 사용. read 도구는 바이너리를 그대로 덤프하므로 이미지/스캔물은 반드시 이 도구로. 파일스토어 파일은 files action=analyze로도 가능.",
		InputSchema: schema.OcrToolSchema(),
		Fn:          artifact.ToolOCR(),
		Deferred:    true,
	})

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

// RegisterPhoneTools registers the phone bridge tools — phone_read
// (location/battery/usage from the app-pushed state cache) and phone_write. Every
// operation travels through send — the PhoneActionFunc the server backs with
// its native-app push channel (SSE foreground / FCM data background). The
// Termux/SSH transport is retired (2026-07-05): reads serve the cache the app
// pushes (stale → sync_state refresh request), writes execute in-app
// (NotificationManager / TTS engine / ClipboardManager / Intents). A nil send
// leaves actions reporting unavailable.
//
// Deferred (prompt audit 2026-06-12): together ~1,050 wire tokens for 17 uses
// in 14 days, nearly all on phone-event turns. The one name-directing prompt
// (phoneevents/handler.go) now teaches the fetch_tools step, and those
// turns are background — a fetch round-trip there is cheap, while every
// interactive turn stops paying for the schemas.
func RegisterPhoneTools(registry toolport.ToolRegistrar, send runtimeops.PhoneActionFunc) {
	registry.RegisterTool(toolport.ToolDef{
		Name:        "phone_read",
		Description: "'지금 어디'·'배터리 몇 %'·'방금 폰에서 뭐에 집중했나' 질문에 사용 — 사용자 스마트폰 위치·배터리·앱 사용 리듬 조회(앱이 밀어주는 상태 캐시 기반, SSH 불필요). what=location(최근 위치) | battery(배터리·충전 상태) | usage(최근 앱 사용 리듬). 캐시가 오래됐으면 앱에 갱신을 요청하고 잠시 후 재시도하라고 안내한다. 능동 판단 시 맥락 보강에도 사용하되, 사용 리듬만으로 알림을 만들지 않는다. 주소록은 `contacts` 도구.",
		InputSchema: schema.PhoneReadToolSchema(),
		Fn:          runtimeops.ToolPhoneRead(send),
		Deferred:    true,
	})
	registry.RegisterTool(toolport.ToolDef{
		Name:        "phone_write",
		Description: "사용자 스마트폰에 직접 작용한다(전부 인앱 실행, SSH 불필요). to — notify(알림 띄우기, text 필수·title 선택) | speak(음성으로 말하기, text) | clipboard(클립보드에 넣기, text) | open_url(target=URL) | open_app(target=패키지/앱명) | share(text) | message(target=수신자,text) | dial(target=전화번호) | photo(카메라) | alarm(알람 설정, target=\"HH:MM\" 24h, text=라벨 — 일회성·Android 전용, 반복 알람 미지원) | timer(타이머, target=단위 포함 \"10m\"/\"90s\"/\"1h30m\", text=라벨 — 단위 없는 숫자 거부). 운전 중 음성 안내, 답을 클립보드에 꽂기, 링크/앱 열기, 메시지·전화·사진·알람·타이머.",
		InputSchema: schema.PhoneWriteToolSchema(),
		Fn:          runtimeops.ToolPhoneWrite(send),
		Deferred:    true,
	})
}

// RegisterWorkstationTool registers the desktop workstation-control tool. The
// command travels over the events push channel (Kind=workspace) to connected
// Andromeda clients, which validate + execute it through their command bus and
// show a visible "화면 조정" nudge. Eager (not Deferred): the tool exists for
// interactive "화면에 띄워줘/나란히 보여줘" turns, where a fetch_tools round
// trip would cost more than the small schema does.
func RegisterWorkstationTool(registry toolport.ToolRegistrar, send runtimeops.WorkstationCommandFunc, hint runtimeops.WorkstationUsageHintFunc) {
	registry.RegisterTool(toolport.ToolDef{
		Name:        "workstation",
		Description: "데스크톱 워크스테이션(Andromeda)의 화면을 직접 조종한다 — 자료를 말로만 설명하지 말고 화면에 띄워라. action: open(화면 열기, view 또는 위키 path) | split(분할 추가, view — 최대 3분할) | close(분할 닫기) | focus(포커스) | layout(분할 일괄 지정, views=\"mail,calendar\") | wiki(위키 페이지, path) | spotlight(항목 강조 — view+ref를 열고 하이라이트, '여기 보세요') | prefill(할일 초안 — view=todo+title(+due,note)로 모달을 채워 열기, 저장은 사용자 클릭). view 키: today|projects|progress|todo|notebook|mail|calendar|wiki|search|people|crons|fleet|workfeed|approvals|groupware|skills|rsi|observe|sitemap. search는 query로 검색어 주입, mail/approvals는 open/split에 date(YYYY-MM-DD)를 더해 그 날짜 페이지로 점프. 쓰는 시점: ① 사용자가 '보여줘/띄워줘/나란히' 요청 ② 특정 프로젝트·문서·메일을 논의 중이면 관련 화면을 선제 오픈(예: 프로젝트 얘기 → layout \"wiki,mail\" + wiki path) — 묻지 말고 띄우고 한 줄로 알릴 것 ③ 모바일에서 '데스크톱에 준비해놔' 원격 예약 ④ 답변에서 특정 항목을 짚을 때 spotlight ⑤ 대화에서 할일이 도출되면 prefill로 초안 제시. '워룸' 요청 = layout으로 위키+메일+피드 3분할. 데스크톱 앱이 연결돼 있어야 한다(미연결이면 조용히 생략하고 말로 답).",
		InputSchema: schema.WorkstationToolSchema(),
		Fn:          runtimeops.ToolWorkstation(send, hint),
	})
}

// RegisterProcessTools registers exec and process management tools.
func RegisterProcessTools(registry toolport.ToolRegistrar, d *tooldeps.ProcessDeps) {
	registry.RegisterTool(toolport.ToolDef{
		Name:        "exec",
		Description: "Run a shell command (bash -c). Default timeout 60s, max 10min. Use background=true for long tasks, then process to check",
		InputSchema: schema.ExecToolSchema(),
		Fn:          runtimeops.ToolExec(d.Mgr, d.WorkspaceDir),
	})
	registry.RegisterTool(toolport.ToolDef{
		Name:        "process",
		Description: "Manage background exec sessions: list running, poll/log output, kill by sessionId",
		InputSchema: schema.ProcessToolSchema(),
		Fn:          runtimeops.ToolProcess(d.Mgr),
		Deferred:    true,
	})
}

// RegisterWebTools registers the unified web tool (search, fetch, search+fetch).
// spill (optional) lets the YouTube path offload full transcripts to disk.
func RegisterWebTools(registry toolport.ToolRegistrar, spill tooldeps.SpilloverStore) {
	webCache := web.NewFetchCache()
	localAI := web.NewLocalAIExtractor()

	registry.RegisterTool(toolport.ToolDef{
		Name: "web",
		Description: "Web access — search and/or fetch pages in one tool. " +
			"query: keyword search (Serper→Brave→DuckDuckGo). " +
			"queries: up to 5 parallel searches. " +
			"url: fetch a page (HTML extract + bot evasion; YouTube → transcript/chapters — use watch only when you need frames). " +
			"fetch=1..3 with query: search then auto-fetch top N pages. " +
			"type=news|scholar|autocomplete: Serper-only typed search (incompatible with fetch).",
		InputSchema: schema.WebToolSchema(),
		Fn:          web.MergedTool(webCache, localAI, spill),
	})
	registry.RegisterTool(toolport.ToolDef{
		Name: "browse",
		Description: "상주 실브라우저(서버의 headful Chromium, 운영자가 noVNC로 로그인해 둔 세션 보유)로 페이지를 열어 본문 텍스트를 읽는다. " +
			"web 도구가 막히는 곳에 쓴다: 로그인 필요 페이지(그룹웨어 웹·포털·카페·멤버십), JS 렌더가 무거운 SPA, 봇 감지에 걸리는 사이트. " +
			"공개 정적 페이지는 web이 더 빠르니 web 먼저. 읽기 전용(클릭·입력 없음)·http(s)만. " +
			"로그인이 풀려 있으면 운영자에게 noVNC 재로그인(scripts/browser/start-browser-sidecar.sh view)을 안내하라.",
		InputSchema: schema.BrowseToolSchema(),
		Fn:          tools.ToolBrowse(),
		Deferred:    true,
	})
}

// RegisterSessionTools registers session management tools.
func RegisterSessionTools(registry toolport.ToolRegistrar, d *tooldeps.SessionDeps) {
	registry.RegisterTool(toolport.ToolDef{
		Name:        "sessions",
		Description: "Sessions: list / history / search / send — other sessions' message logs, transcript keyword search, cross-session messaging",
		InputSchema: schema.SessionsToolSchema(),
		Fn:          runtimeops.ToolSessions(d),
		Deferred:    true,
	})
	registry.RegisterTool(toolport.ToolDef{
		Name:        "sessions_spawn",
		Description: "Spawn a sub-agent to work in parallel — use for long tasks, research, or when the user is waiting. Faster than doing it yourself",
		InputSchema: schema.SessionsSpawnToolSchema(),
		Fn:          runtimeops.ToolSessionsSpawn(d),
	})
	// Deferred (2026-07-09): the Sub-Agents prompt section tells the model NOT to
	// poll with subagents — child completions auto-deliver via the notify relay
	// (subagent_notify.go). Its only live use is the edge-case steer/kill of a
	// running child, so that rare turn fetches it. sessions_spawn stays eager: the
	// prompt directs delegation by name and it must stay frictionless.
	registry.RegisterTool(toolport.ToolDef{
		Name:        "subagents",
		Description: "Monitor and control sub-agents: list status, steer with messages, or kill. Defaults to list",
		InputSchema: schema.SubagentsToolSchema(),
		Fn:          runtimeops.ToolSubagents(d),
		Deferred:    true,
	})
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

// RegisterMediaTools registers media tools: file delivery (send_file) and
// video watching (watch). workspaceDir bounds the watch tool's local-file
// access; an empty string restricts watch to YouTube URLs only.
func RegisterMediaTools(registry toolport.ToolRegistrar, workspaceDir string) {
	registry.RegisterTool(toolport.ToolDef{
		Name:        "send_file",
		Description: "Send a file to the user (auto-detects: photo/video/audio/document). Max 50 MB",
		InputSchema: schema.SendFileToolSchema(),
		Fn:          artifact.ToolSendFile(),
		Deferred:    true,
	})
	registry.RegisterTool(toolport.ToolDef{
		Name: "chart",
		Description: "숫자 데이터를 보기 좋은 차트 이미지(PNG)로 그린다 — 추이(line)·누적(area)·비교(bar)·구성비(doughnut). " +
			"표로 나열하기보다 한눈에 들어오는 게 나을 때(월별 추이, 거래처별 비교, 단계별 비율 등) 사용하라. " +
			"막대 위에 추세선을 얹는 콤보도 가능(한 시리즈에 type:line). " +
			"렌더된 PNG 경로를 돌려주므로, 그 경로를 send_file(type:\"photo\")로 사용자에게 전송해야 실제로 보인다.",
		InputSchema: schema.ChartToolSchema(),
		Fn:          artifact.ToolChart(),
		Deferred:    true,
	})
	registry.RegisterTool(toolport.ToolDef{
		Name: "diagram",
		Description: "구조·흐름·일정을 다이어그램 이미지(PNG)로 그린다 — 절차/관계/상태도는 flowchart(노드+화살표), 일정은 gantt(작업별 기간 막대), 연혁/이력/로드맵은 timeline(시점별 사건). " +
			"인허가 절차, 결재 흐름, 프로젝트 일정, 회사 연혁처럼 말이나 표보다 그림이 나은 걸 설명할 때 쓴다. " +
			"숫자 비교·추이는 diagram이 아니라 chart를 써라. " +
			"렌더된 PNG 경로를 돌려주므로, 그 경로를 send_file(type:\"photo\")로 사용자에게 전송해야 실제로 보인다.",
		InputSchema: schema.DiagramToolSchema(),
		Fn:          artifact.ToolDiagram(),
		Deferred:    true,
	})
	registry.RegisterTool(toolport.ToolDef{
		Name: "watch",
		Description: "Watch a video: extract frames + subtitles from a YouTube URL or local video file, " +
			"then analyze with the vision model so you can actually SEE and HEAR the content. " +
			"Use for analyzing video structure/hooks, diagnosing bugs from screen recordings, or summarizing long videos. " +
			"Supports start/end to focus on a time window.",
		InputSchema: schema.WatchToolSchema(),
		Fn:          artifact.ToolWatch(workspaceDir),
		Deferred:    true,
	})
}
