// Package toolreg provides tool registration wiring — connecting tool
// implementations (from tools/) with their JSON schemas (from tool_schemas_gen.go)
// and registering them into a ToolRegistrar (implemented by chat.ToolRegistry).
//
// Dependency flow: toolreg/ -> toolctx/ (types), toolreg/ -> tools/ (implementations).
// toolreg/ never imports chat/ — the chat package calls toolreg functions.
package toolreg

import (
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/web"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

// RegisterCoreTools populates the tool registrar with all core agent tools.
// It delegates to domain-specific Register*Tools functions.
func RegisterCoreTools(registry toolctx.ToolRegistrar, deps *toolctx.CoreToolDeps) {
	RegisterFSTools(registry, deps)
	RegisterProcessTools(registry, &deps.Process)
	RegisterWebTools(registry, deps.SpilloverStore)
	RegisterSessionTools(registry, &deps.Sessions)
	RegisterChronoTools(registry)
	RegisterMediaTools(registry, deps.WorkspaceDir)
	var diaryDir, wikiDir string
	if deps.Wiki.Store != nil {
		diaryDir = deps.Wiki.Store.DiaryDir()
		wikiDir = deps.Wiki.Store.Dir()
	}
	RegisterRoutineTools(registry, &deps.Chrono, diaryDir, wikiDir, deps.FilesSemanticSearch)
	RegisterPhoneTools(registry)

	// Standing goal (Ralph loop). Eager: the agent must discover it to set a
	// goal on a multi-step request. Once set, the server's goalTask advances it
	// one run per idle tick, judges completion with the lightweight model, and a
	// per-goal idempotency ledger blocks repeated destructive actions.
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "goal",
		Description: "다단계·장기 작업을 여러 턴에 걸쳐 끝까지 진행해야 할 때 표준 목표(standing goal)를 설정·관리한다. action=set(목표 설정) | subgoal(완료 기준 추가) | status | pause | resume | stop. 설정하면 사용자가 자리를 비운 동안 자동으로 한 단계씩 진행하고 완료를 판정한다. 이미 실행한 작업은 멱등 가드로 중복되지 않는다.",
		InputSchema: goalToolSchema(),
		Fn:          tools.ToolGoal(),
	})

	// Mail archive reader. Deferred: only the daily-digest cron (background) and
	// occasional "what mail came in" asks need it, so interactive turns don't pay
	// for the schema. Reads the on-box deneb-mailarchive store over loopback IMAP.
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "mail_archive",
		Description: "자체 메일 아카이브(자동보관 수신 메일 + 과거 백필)를 조회해 ID/Locator를 얻고, 전체 스레드와 프로젝트 히스토리를 복원한다. action=list(오늘/최근 메일) | search(키워드) | read(Locator/ID 또는 query로 원문 열기) | thread(Message-ID/References 기반 전체 대화) | project_history(회사·프로젝트 키워드 시간선+스레드 후보). 업무 맥락·메일 기반 미팅 준비·프로젝트 과거 확인에는 이 도구를 우선 사용한다.",
		InputSchema: mailArchiveToolSchema(),
		Fn: tools.ToolMailArchive(tools.MailArchiveDeps{
			Wiki:     deps.Wiki.Store,
			Calendar: &deps.Calendar,
		}),
		Deferred: true,
	})

	// Research panel: fan a question out to every healthy model in parallel
	// (deep-research skill). Deferred — only deliberate deep research needs it,
	// so interactive turns don't pay for the schema. nil ConsultPanel (no model
	// registry / router wired) leaves the tool unregistered.
	if deps.ConsultPanel != nil {
		registry.RegisterTool(toolctx.ToolDef{
			Name:        "research_panel",
			Description: "하나의 질문을 현재 가동 중(헬시)인 모든 모델에게 병렬로 던져 모델별 답을 모아 온다(이종 모델 패널 팬아웃). 딥리서치·고위험 의사결정처럼 여러 관점과 교차검증이 가치 있을 때 사용. 반환된 모델별 답을 당신이 직접 종합하라 — 서로 다른 계열이 합의하면 강한 신뢰, 모순은 명시하고, 자신만만한 답에 닻 내리지 말 것. 단순 사실질문엔 쓰지 마라(비용이 모델 수만큼 N배). models로 특정 모델만 지정 가능, 비우면 전체.",
			InputSchema: researchPanelToolSchema(),
			Fn:          tools.ToolResearchPanel(deps.ConsultPanel),
			Deferred:    true,
		})
	}

	// NOTE: Pilot tool is registered separately by chat.RegisterCoreTools
	// because it depends on local AI hooks that live in the chat package.
	// NOTE: fetch_tools is registered by chat.RegisterCoreTools because it
	// needs a FetchToolsRegistry interface that chat.ToolRegistry implements.
}

// FetchToolsSchema returns the fetch_tools schema for external registration.
func FetchToolsSchema() map[string]any { return fetchToolsToolSchema() }

// RegisterPhoneTools registers the phone bridge tools — phone_read (location/
// clipboard/battery) and phone_write (notification/tts/clipboard) — which reach
// the user's phone over reverse SSH. No deps: the ssh target resolves from the
// "phone" ~/.ssh/config alias (or DENEB_PHONE_SSH).
//
// Deferred (prompt audit 2026-06-12): together ~1,050 wire tokens for 17 uses
// in 14 days, nearly all on phone-event turns. The one name-directing prompt
// (server_http_event_ingest.go) now teaches the fetch_tools step, and those
// turns are background — a fetch round-trip there is cheap, while every
// interactive turn stops paying for the schemas.
func RegisterPhoneTools(registry toolctx.ToolRegistrar) {
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "phone_read",
		Description: "사용자 스마트폰을 조회한다(reverse SSH→Termux). what=location(현재 GPS 좌표) | clipboard(방금 복사한 내용) | battery(배터리·충전 상태) | calllog(최근 통화기록 20건) | contacts(폰 주소록 — 특정 인물 검색은 `contacts` 도구가 더 낫다). '지금 어디', '방금 복사한 거', '최근 통화 누구랑' 같은 질문이나, 능동 판단 시 맥락 보강에 사용.",
		InputSchema: phoneReadToolSchema(),
		Fn:          tools.ToolPhoneRead(),
		Deferred:    true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "phone_write",
		Description: "사용자 스마트폰에 직접 작용한다(reverse SSH→Termux). to=notification(알림 띄우기) | tts(음성으로 읽어주기) | clipboard(클립보드에 넣기). text 필수, notification은 title 선택. 운전 중 음성 안내나, 작성한 답을 폰 클립보드에 바로 꽂을 때.",
		InputSchema: phoneWriteToolSchema(),
		// nil PhoneActionFunc: the Intent-backed P1 actions (open_url/share/…)
		// are validated + tested but dormant until the SSE/FCM delivery + app
		// dispatcher land (device-gated). The SSH notification/tts/clipboard ops
		// work today. Not advertised in the description yet.
		Fn:       tools.ToolPhoneWrite(nil),
		Deferred: true,
	})
}

// RegisterPolarisTools registers the unified Polaris tool (search/describe/expand).
// Called separately because the store and localAI are not part of CoreToolDeps.
func RegisterPolarisTools(registry toolctx.ToolRegistrar, store *polaris.Store, localAI tools.LocalAIFunc) {
	if store == nil {
		return
	}
	registry.RegisterTool(toolctx.ToolDef{
		Name: "polaris",
		Description: "현재 세션의 압축된 과거 대화 회상 (모든 메시지가 SQLite FTS에 무손실 저장). " +
			"사용자가 컨텍스트에 없는 합의·숫자·인물·결정 또는 '아까 그거'·'지난번에' 같은 참조를 언급하면 " +
			"짐작하지 말고 먼저 호출하라. " +
			"action=search(키워드 검색) → describe(압축 요약 구간 ID 목록, time_range로 today/this_week/all) → " +
			"expand(특정 summary_id 원문 복원, question 추가 시 LLM이 원문 기반 답변). " +
			"`<recall-context>` 자동 주입은 첫 턴 cue 기반 preflight 한 번뿐이므로, 턴 도중 새 회상이 필요하면 이 도구를 직접 호출하라.",
		InputSchema: polarisToolSchema(),
		Fn:          tools.ToolPolaris(store, localAI),
	})
}

// RegisterKnowledgeTool registers the knowledge tool over the wiki knowledge
// base behind one agent surface. Called separately because the knowledge
// router needs the wiki Store at construction time.
//
// Pass-through behavior: if router is nil (no backends configured) the tool
// is not registered so the agent does not see a dead surface.
func RegisterKnowledgeTool(registry toolctx.ToolRegistrar, router *knowledge.Router) {
	if router == nil || len(router.Layers()) == 0 {
		return
	}
	registry.RegisterTool(toolctx.ToolDef{
		Name: "knowledge",
		Description: "지식·기억 도구. 위키 지식베이스를 의미+키워드로 검색·조회·기록. " +
			"op=recall(질의→의미 기반 검색, ref와 함께 머지) → " +
			"op=read(ref로 단건 fetch — `w:인물/박부장` 같이 prefix로 layer 자동 라우팅) → " +
			"op=record(wiki에 큐레이션 페이지 작성·갱신). " +
			"polaris(현재 세션 회상)·graphify(개념 그래프)는 별개 도구로 분리됨 — paradigm이 다름.",
		InputSchema: knowledgeToolSchema(),
		Fn:          tools.ToolKnowledge(router),
	})
}

// RegisterFSTools registers file-system, code analysis, and git tools.
func RegisterFSTools(registry toolctx.ToolRegistrar, deps *toolctx.CoreToolDeps) {
	workspaceDir := deps.WorkspaceDir
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "read",
		Description: "Read file contents with line numbers for code review (default: 2000 lines). Use offset/limit for large files; equivalent to a clean bat/cat -n view",
		InputSchema: readToolSchema(),
		Fn:          tools.ToolRead(workspaceDir, deps.SkillsCatalogDirs...),
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "write",
		Description: "Create or overwrite a file. Auto-creates parent directories. Use edit for partial changes",
		InputSchema: writeToolSchema(),
		Fn:          tools.ToolWrite(workspaceDir),
	})
	// Deferred (prompt audit 2026-06-12): ~370 wire tokens for 2 uses in 14
	// days — Deneb is a chief-of-staff, not a coding agent, so partial file
	// edits are rare. read/write/grep stay eager; an editing turn fetches this.
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "edit",
		Description: "Search-and-replace in a file. old_string must be unique unless replace_all=true. Read first to find the exact string",
		InputSchema: editToolSchema(),
		Fn:          tools.ToolEdit(workspaceDir),
		Deferred:    true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "grep",
		Description: "Regex search across files (rg / ripgrep). Use include/fileType to narrow scope. Returns file:line:match format",
		InputSchema: grepToolSchema(),
		Fn:          tools.ToolGrep(workspaceDir),
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name: "gateway",
		Description: "Gateway self-management: status (버전/PID/포트/업타임/세션 수를 한 번에 반환), config_get/config_set (dotted paths), update (git pull + rebuild + restart), restart. " +
			"Destructive actions (restart/update/config_set) require approval — the first call returns a needs_approval envelope; relay the Korean summary to the user verbatim, and after approval call the .confirmed variant with the same action_token. " +
			"토큰/비밀번호/API 키는 절대 config_set으로 건드리지 마라.",
		InputSchema: gatewayToolSchema(),
		Fn:          tools.ToolGateway(workspaceDir),
		Deferred:    true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "observe",
		Description: "Observe your OWN runtime via the in-process observation plane: action=turn (runId → a past run's tokens/tools/cache + its captured logs), action=logs (recent log ring; filter by runId/session/level/contains), action=behavior (cross-session tool usage / proactive funnel / background-job health over N days, plus the local vLLM engine's prefix-cache hit rate), action=effort (adaptive effort-router scorecard: routed-off vs kept-on, escalation rate, savings), action=proactive (proactive-card engagement: FTR / over-intervention rate by source). Use it to diagnose your own slow or failing turns.",
		InputSchema: observeToolSchema(),
		Fn:          tools.ToolObserve(deps.LogCapture, deps.AgentLog, deps.WorkFeed, deps.VllmBaseURLs),
		Deferred:    true,
	})

	// Fleet: manage the SparkFleet GPU control plane (the machine's own model
	// servers) — the chat twin of the native 플릿 tab / the /api/v1/fleet
	// passthrough. Deferred like gateway/observe: niche but powerful, loaded via
	// fetch_tools when the user actually asks about the fleet.
	registry.RegisterTool(toolctx.ToolDef{
		Name: "fleet",
		Description: "SparkFleet GPU 컨트롤 플레인 관리 — 이 머신의 GPU 모델 서버를 띄우고 점검한다. " +
			"action=status (노드 GPU/메모리·레시피 실행 상태·최근 실패 작업 한눈에) · recipes (모델 레시피 목록) · jobs (백그라운드 작업) · " +
			"launch/stop/restart (recipe 이름으로 모델 기동·중지·재시작 — 실제 동작) · cancel (jobId로 작업 취소) · diagnose (실행 중 레시피 컨테이너 크래시 진단). " +
			"\"플릿 괜찮아?\" · \"qwen36 재시작해줘\" · \"왜 죽었어?\" 같은 요청에 사용.",
		InputSchema: fleetToolSchema(),
		Fn:          tools.ToolFleet(&deps.Fleet),
		Deferred:    true,
	})

	// Spillover: read full content of a previously spilled large tool result.
	// Registered eagerly so the trim marker's embedded spill ID can be used
	// in the same turn without a fetch_tools round-trip.
	if deps.SpilloverStore != nil {
		registry.RegisterTool(toolctx.ToolDef{
			Name:        "read_spillover",
			Description: "Read the full content of a previous large tool result by spill ID. Use when a tool result was too large and was replaced with a preview",
			InputSchema: readSpilloverToolSchema(),
			Fn:          tools.ToolSpilloverRead(deps.SpilloverStore),
		})
	}

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

// RegisterProcessTools registers exec and process management tools.
func RegisterProcessTools(registry toolctx.ToolRegistrar, d *toolctx.ProcessDeps) {
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "exec",
		Description: "Run a shell command (bash -c). Default timeout 60s, max 10min. Use background=true for long tasks, then process to check",
		InputSchema: execToolSchema(),
		Fn:          tools.ToolExec(d.Mgr, d.WorkspaceDir),
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "process",
		Description: "Manage background exec sessions: list running, poll/log output, kill by sessionId",
		InputSchema: processToolSchema(),
		Fn:          tools.ToolProcess(d.Mgr),
		Deferred:    true,
	})
}

// RegisterWebTools registers the unified web tool (search mode only).
// spill (optional) lets the YouTube path offload full transcripts to disk.
func RegisterWebTools(registry toolctx.ToolRegistrar, spill *agent.SpilloverStore) {
	webCache := web.NewFetchCache()
	localAI := web.NewLocalAIExtractor()

	registry.RegisterTool(toolctx.ToolDef{
		Name:        "web",
		Description: "Web access: search the web or fetch page content. Use query for keyword search, url for direct fetch",
		InputSchema: webToolSchema(),
		Fn:          web.MergedTool(webCache, localAI, spill),
	})
}

// RegisterSessionTools registers session management tools.
func RegisterSessionTools(registry toolctx.ToolRegistrar, d *toolctx.SessionDeps) {
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "sessions",
		Description: "Session management: list (active sessions), history (message log), search (transcript keyword search), send (cross-session message)",
		InputSchema: sessionsToolSchema(),
		Fn:          tools.ToolSessions(d),
		Deferred:    true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "sessions_spawn",
		Description: "Spawn a sub-agent to work in parallel — use for long tasks, research, or when the user is waiting. Faster than doing it yourself",
		InputSchema: sessionsSpawnToolSchema(),
		Fn:          tools.ToolSessionsSpawn(d),
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "subagents",
		Description: "Monitor and control sub-agents: list status, steer with messages, or kill. Defaults to list",
		InputSchema: subagentsToolSchema(),
		Fn:          tools.ToolSubagents(d),
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
func RegisterChronoTools(registry toolctx.ToolRegistrar) {
	registry.RegisterTool(toolctx.ToolDef{
		Name: "message",
		Description: "Send messages to the user's channel. Actions: send, reply, react, thread-reply. Use for proactive sends. " +
			"**사용자가 방금 보낸 메시지에 대한 응답에는 절대 쓰지 마라** — 일반 응답은 턴의 최종 텍스트가 자동 전달된다. " +
			"이 도구로 사용자에게 보일 내용을 이미 전송했다면, 중복 전달을 막기 위해 턴의 최종 텍스트는 정확히 NO_REPLY 한 단어만 출력하라(다른 텍스트와 섞지 말 것).",
		InputSchema: messageToolSchema(),
		Fn:          tools.ToolMessage(),
		Deferred:    true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name: "heartbeat_update",
		Description: "Overwrite ~/.deneb/HEARTBEAT.md with a new full content string. Pass empty content to clear the file. " +
			"Used by the 30-minute autonomous heartbeat to retire completed/cancelled items, update progress notes, " +
			"and archive stalled items. Also callable by the user via natural language (\"add X to my heartbeat\", \"remove the spark deploy task\"). " +
			"Auto-backs up the prior content to HEARTBEAT.md.prev. Eager registration: the autonomous heartbeat " +
			"trigger explicitly directs the agent to call this tool, so it must be visible in the default prompt " +
			"(deferring it would force a fetch_tools round-trip and add a fragile turn).",
		InputSchema: heartbeatUpdateToolSchema(),
		Fn:          tools.ToolHeartbeatUpdate(),
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name: "todo",
		Description: "Manage the user's 할일 (to-do) list — the SAME localtodo store the native client reads via miniapp.todo.*. " +
			"Actions: list | add (needs title; optional due YYYY-MM-DD) | done (needs id; optional done=false to un-complete) | delete (needs id). " +
			"Use THIS for the user's checkable tasks (a to-do added here appears on the user's device); heartbeat_update is the agent's own free-form work memo, not the user's task list.",
		InputSchema: todoToolSchema(),
		Fn:          tools.ToolTodo(),
	})
}

// RegisterRoutineTools registers tools for recurring/scheduled tasks —
// things that sit between always-on core tools and on-demand skills.
// Typical trigger: cron scheduler, daily routines, periodic checks.
// diaryDir is the wiki diary directory for morning letter logging; wikiDir is
// the wiki root for its deadline scan (either empty = that part disabled).
func RegisterRoutineTools(registry toolctx.ToolRegistrar, chrono *toolctx.ChronoDeps, diaryDir, wikiDir string, filesSemanticSearch tools.FilesSemanticSearchFunc) {
	// Deferred (prompt audit 2026-06-12): ~590 wire tokens — the second-largest
	// eager tool — for 11 interactive uses in 14 days. The scheduler itself runs
	// server-side; this tool only manages jobs, so a "매일 아침에 …" turn pays one
	// fetch round-trip instead of every turn paying the schema. No cron job
	// prompt directs the cron tool by name (the static Tool Usage trigger line
	// "for follow-ups use cron" stays, pointing at the deferred listing).
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "cron",
		Description: "Schedule recurring jobs (cron expressions). Actions: status, list, add, update, remove, run, get, runs, wake",
		InputSchema: cronToolSchema(),
		Fn:          tools.ToolCron(chrono),
		Deferred:    true,
	})

	registry.RegisterTool(toolctx.ToolDef{
		Name:        "files",
		Description: "파일 저장소 (로컬 디스크, 외부 클라우드 아님): list, search (이름·content=true로 내용, semantic=true로 의미 기반 벡터 검색), semantic_search (=search semantic=true), download (extract=true로 텍스트 추출 — PDF/이미지 OCR·Excel/Word/PowerPoint), upload (로컬 파일을 저장소에 저장), share (7일 유효 공유 링크), analyze (문서 내용 추출). 저장 위치: DENEB_FILES_DIR (기본 ~/.deneb/files). 인증 불필요.",
		InputSchema: filesToolSchema(),
		Fn:          tools.ToolFiles(filesSemanticSearch),
		Deferred:    true,
	})
	// Morning-letter data collection: six sections in parallel, raw JSON out;
	// the agent composes the letter. Deferred like the other routine tools —
	// the daily cron run loads it via fetch_tools, every other turn stays slim.
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "morning_letter",
		Description: "모닝레터 데이터 수집: 날씨·환율·구리시세·오늘 일정·미읽음 메일·위키 마감(due) 6개 섹션을 병렬 수집해 raw JSON 반환. 편지 작성(어조·해석·우선순위)은 에이전트 몫. No parameters",
		InputSchema: morningLetterToolSchema(),
		Fn:          tools.ToolMorningLetter(nil, tools.MorningLetterOpts{DiaryDir: diaryDir, WikiDir: wikiDir}),
		Deferred:    true,
	})
	// Evening-letter data collection: the end-of-day counterpart to
	// morning_letter — forward-looking sections (calendar, email, deadlines),
	// the morning-only market data omitted. Deferred like the other routine tools.
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "evening_letter",
		Description: "이브닝레터 데이터 수집: 일정(오늘+내일)·미처리 메일·임박 마감을 병렬 수집해 raw JSON 반환. 모닝레터의 저녁 짝 — 시장데이터(날씨·환율·구리)는 제외. 편지 작성(회고·내일 준비·우선순위)은 에이전트 몫. No parameters",
		InputSchema: eveningLetterToolSchema(),
		Fn:          tools.ToolEveningLetter(nil, tools.EveningLetterOpts{DiaryDir: diaryDir, WikiDir: wikiDir}),
		Deferred:    true,
	})
}

// RegisterSkillsTools registers the unified skills tool
// (list/create/patch/delete/read/list_files/write_file/remove_file).
func RegisterSkillsTools(registry toolctx.ToolRegistrar, getSnapshot tools.SkillsSnapshotProvider, workspaceDir, bundledSkillsDir string, invalidateCache tools.SkillManageInvalidateFn) {
	registry.RegisterTool(toolctx.ToolDef{
		Name: "skills",
		Description: "Skill management: list (browse/search), create, patch, read, delete, list_files, write_file, remove_file. " +
			"Use list when the current task might match a skill. Create reusable workflows from complex tasks.",
		InputSchema: skillsToolSchema(),
		Fn:          tools.ToolSkills(getSnapshot, workspaceDir, bundledSkillsDir, invalidateCache),
		Deferred:    true,
	})
}

// RegisterMediaTools registers media tools: file delivery (send_file) and
// video watching (watch). workspaceDir bounds the watch tool's local-file
// access; an empty string restricts watch to YouTube URLs only.
func RegisterMediaTools(registry toolctx.ToolRegistrar, workspaceDir string) {
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "send_file",
		Description: "Send a file to the user (auto-detects: photo/video/audio/document). Max 50 MB",
		InputSchema: sendFileToolSchema(),
		Fn:          tools.ToolSendFile(),
		Deferred:    true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name: "chart",
		Description: "숫자 데이터를 보기 좋은 차트 이미지(PNG)로 그린다 — 추이(line)·누적(area)·비교(bar)·구성비(doughnut). " +
			"표로 나열하기보다 한눈에 들어오는 게 나을 때(월별 추이, 거래처별 비교, 단계별 비율 등) 사용하라. " +
			"막대 위에 추세선을 얹는 콤보도 가능(한 시리즈에 type:line). " +
			"렌더된 PNG 경로를 돌려주므로, 그 경로를 send_file(type:\"photo\")로 사용자에게 전송해야 실제로 보인다.",
		InputSchema: chartToolSchema(),
		Fn:          tools.ToolChart(),
		Deferred:    true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name: "diagram",
		Description: "구조·흐름·일정을 다이어그램 이미지(PNG)로 그린다 — 절차/관계/상태도는 flowchart(노드+화살표), 일정은 gantt(작업별 기간 막대), 연혁/이력/로드맵은 timeline(시점별 사건). " +
			"인허가 절차, 결재 흐름, 프로젝트 일정, 회사 연혁처럼 말이나 표보다 그림이 나은 걸 설명할 때 쓴다. " +
			"숫자 비교·추이는 diagram이 아니라 chart를 써라. " +
			"렌더된 PNG 경로를 돌려주므로, 그 경로를 send_file(type:\"photo\")로 사용자에게 전송해야 실제로 보인다.",
		InputSchema: diagramToolSchema(),
		Fn:          tools.ToolDiagram(),
		Deferred:    true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name: "watch",
		Description: "Watch a video: extract frames + subtitles from a YouTube URL or local video file, " +
			"then analyze with the vision model so you can actually SEE and HEAR the content. " +
			"Use for analyzing video structure/hooks, diagnosing bugs from screen recordings, or summarizing long videos. " +
			"Supports start/end to focus on a time window.",
		InputSchema: watchToolSchema(),
		Fn:          tools.ToolWatch(workspaceDir),
		Deferred:    true,
	})
}

// RegisterContactsTool registers the address-book lookup tool (phone lookup +
// name/company search) over the contacts store mirrored from the native client's
// contacts sync. Skipped when the store isn't wired so the agent doesn't see a
// dead surface; a nil/empty store would otherwise reply "주소록이 비어 있습니다".
func RegisterContactsTool(registry toolctx.ToolRegistrar, contactsDeps *toolctx.ContactsDeps) {
	if contactsDeps.Store == nil {
		return
	}
	// Deferred (prompt audit 2026-06-12): ~220 wire tokens for 6 uses in 14
	// days. ASR hotword injection and wiki person enrichment read the contacts
	// store server-side and are unaffected; only the rare "이 번호 누구야" turn
	// pays a fetch round-trip.
	registry.RegisterTool(toolctx.ToolDef{
		Name: "contacts",
		Description: "주소록(연락처 DB)에서 전화번호로 인물을 찾거나(lookup) 이름·회사로 검색(search). " +
			"네이티브 클라이언트가 동기화한 연락처 전체를 조회한다. " +
			"사용자가 '이 번호 누구?', '010-xxxx 누구야', 'OOO 연락처/번호' 같이 물으면 짐작하지 말고 호출하라.",
		InputSchema: contactsToolSchema(),
		Fn:          tools.ToolContacts(contactsDeps),
		Deferred:    true,
	})
}

// RegisterCalendarTool registers the calendar tool: read merged Google (read-only)
// + local events, and create/update/delete local events. Skipped when neither a
// Google client factory nor a local store is wired, so the agent doesn't see a
// dead surface. This is the chat-side twin of the miniapp.calendar.* RPC surface.
func RegisterCalendarTool(registry toolctx.ToolRegistrar, calDeps *toolctx.CalendarDeps) {
	if calDeps.Client == nil && calDeps.Local == nil {
		return
	}
	registry.RegisterTool(toolctx.ToolDef{
		Name: "calendar",
		Description: "캘린더 일정 조회·관리. list(다가오는 일정), get(상세 — 참석자·장소·Meet·메모, 미팅 준비용), create(추가), update(수정), delete(삭제). " +
			"구글 캘린더(읽기)와 로컬 일정(읽기·쓰기)을 합쳐 보여주며 추가·수정·삭제는 로컬 일정에만 적용된다. " +
			"사용자가 '오늘/이번 주 일정', '내일 3시 미팅 잡아줘', 'OOO 일정 언제야', '미팅 준비' 같이 일정을 묻거나 시키면 짐작하지 말고 호출하라.",
		InputSchema: calendarToolSchema(),
		Fn:          tools.ToolCalendar(calDeps),
	})
}

// RegisterWikiTools registers wiki knowledge base tools for long-term knowledge
// access (search, read, write, log). Project-specific tools provide structured
// access to the "프로젝트" wiki category.
func RegisterWikiTools(registry toolctx.ToolRegistrar, wikiDeps *toolctx.WikiDeps, workspaceDir string) {
	// Wiki: unified knowledge base tool (search, read, write, log, daily, index, status).
	if wikiDeps.Store != nil {
		registry.RegisterTool(toolctx.ToolDef{
			Name:        "wiki",
			Description: "LLM 위키 지식베이스: search (검색), read (페이지 읽기), index (목차), write (작성/수정), log (일지), daily (최근 일지), status (통계). 과거 결정/맥락/인물/프로젝트 등 장기 지식을 마크다운 위키로 관리. write 시 related/[[wikilink]]로 연결하고, 새 사실이 기존 페이지를 대체하면 supersedes로 stale 페이지를 표시한다. 본문에서 인물을 [[이름]]으로 링크하면 주소록에 있는 사람은 인물 페이지가 자동 생성·연락처 기록된다(인물 페이지를 직접 쓰면 그 사람 연락처도 자동 채워짐)",
			InputSchema: wikiToolSchema(),
			Fn:          tools.ToolWiki(wikiDeps, workspaceDir),
		})
	}
}

// RegisterNotebookTool registers the notebook tool — NotebookLM-style scoped
// source collections for grounded, cited synthesis (딜/프로젝트 브리핑). Skipped
// when the notebook store is unavailable.
func RegisterNotebookTool(registry toolctx.ToolRegistrar, deps *toolctx.NotebookDeps) {
	if deps == nil || deps.Store == nil {
		return
	}
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "notebook",
		Description: "NotebookLM식 자료 노트북: create (노트북 생성), list (목록), show (자료 보기), add_source (자료 핀: kind=wiki 위키페이지 또는 kind=note 붙여넣기 텍스트), remove_source (자료 제거), delete (노트북 삭제), brief (핀된 자료에만 근거한 인용 포함 브리핑 생성). 특정 딜/프로젝트의 메일·문서·메모를 한데 모아 그 자료만으로 출처 추적 가능한 종합을 만들 때 사용. brief는 [S1] 형식으로 각 사실을 인용한다",
		InputSchema: notebookToolSchema(),
		Fn:          tools.ToolNotebook(deps),
	})
}
