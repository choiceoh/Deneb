package ops

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/groupwareops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/phoneops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/runtimeops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/media"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/schema"
)

// RuntimeOpsDeps is the narrow wiring contract for host and operations tools.
type RuntimeOpsDeps struct {
	WorkspaceDir   string
	GatewayVersion string
	ObserveTool    tooldeps.ObserveToolFunc
	Fleet          tooldeps.FleetDeps
	Browser        tooldeps.BrowserDeps
	WikiStore      *wiki.Store
	SpilloverStore tooldeps.SpilloverStore
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

// RegisterRuntimeOps builds and registers gateway diagnostics and host
// operations tools from the narrow runtime-ops dependency contract.
func RegisterRuntimeOps(registry toolport.ToolRegistrar, deps RuntimeOpsDeps) {
	RegisterRuntimeOpsTools(registry, RuntimeOpsToolSetFromDeps(deps))
}

// RuntimeOpsToolSetFromDeps wires runtime-ops executors without leaking their
// concrete implementation packages to core tool registration.
func RuntimeOpsToolSetFromDeps(deps RuntimeOpsDeps) RuntimeOpsToolSet {
	observeFn := toolport.ToolFunc(deps.ObserveTool)
	if observeFn == nil {
		observeFn = toolport.ToolFunc(func(context.Context, json.RawMessage) (string, error) {
			return `{"ok":false,"error":"observe unavailable"}`, nil
		})
	}
	set := RuntimeOpsToolSet{
		Gateway:   runtimeops.ToolGatewayWithDeps(deps.WorkspaceDir, runtimeops.GatewayDeps{Version: deps.GatewayVersion}),
		Observe:   observeFn,
		Fleet:     runtimeops.ToolFleet(&deps.Fleet),
		Browser:   runtimeops.ToolBrowser(&deps.Browser),
		Groupware: groupwareops.ToolGroupware(deps.WikiStore),
		Solarflow: runtimeops.ToolSolarflow(),
	}
	if deps.SpilloverStore != nil {
		set.SpilloverRead = media.SpilloverReadTool(deps.SpilloverStore)
	}
	return set
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
func RegisterPhoneTools(registry toolport.ToolRegistrar, send tooldeps.PhoneActionFunc) {
	registry.RegisterTool(toolport.ToolDef{
		Name:        "phone_read",
		Description: "스마트폰 상태 읽기 전용 — '지금 어디야'·'배터리 몇 %'·'방금 폰에서 어떤 앱을 썼나/집중했나' 같은 질문이나 능동 판단 보조 맥락에 사용. 인자는 반드시 what 하나이며 값은 정확히 location(최근 위치) | battery(배터리·충전 상태) | usage(최근 앱 사용 리듬) 중 하나. 앱이 밀어주는 캐시만 읽고, 오래됐으면 앱에 갱신을 요청한 뒤 수 초 후 재호출하라고 안내한다. 휴대폰에 알림/음성/클립보드/앱 실행/문자/전화 같은 행동은 phone_write, 주소록 검색은 contacts, 화면·클립보드 읽기와 통화기록 조회는 지원하지 않는다. 사용 리듬만으로 선제 알림을 만들지 않는다.",
		InputSchema: schema.PhoneReadToolSchema(),
		Fn:          phoneops.ToolPhoneRead(send),
		Deferred:    true,
	})
	registry.RegisterTool(toolport.ToolDef{
		Name:        "phone_write",
		Description: "사용자 스마트폰에 직접 작용한다(전부 인앱 실행, SSH 불필요). to — notify(알림 띄우기, text 필수·title 선택) | speak(음성으로 말하기, text) | clipboard(클립보드에 넣기, text) | open_url(target=URL) | open_app(target=패키지/앱명) | share(text) | message(target=수신자,text) | dial(target=전화번호) | photo(카메라) | alarm(알람 설정, target=\"HH:MM\" 24h, text=라벨 — 일회성·Android 전용, 반복 알람 미지원) | timer(타이머, target=단위 포함 \"10m\"/\"90s\"/\"1h30m\", text=라벨 — 단위 없는 숫자 거부). 운전 중 음성 안내, 답을 클립보드에 꽂기, 링크/앱 열기, 메시지·전화·사진·알람·타이머.",
		InputSchema: schema.PhoneWriteToolSchema(),
		Fn:          phoneops.ToolPhoneWrite(send),
		Deferred:    true,
	})
}

// RegisterHeartbeatTool registers the agent's own HEARTBEAT.md update surface.
func RegisterHeartbeatTool(registry toolport.ToolRegistrar) {
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

// WorkstationDeps is the narrow wiring contract for desktop workstation control.
type WorkstationDeps struct {
	Send func(ctx context.Context, action string, args map[string]string) error
	Hint func(action string) string
}

// RegisterWorkstationTool registers the desktop workstation-control tool. The
// command travels over the events push channel (Kind=workspace) to connected
// Andromeda clients, which validate + execute it through their command bus and
// show a visible "화면 조정" nudge. Eager (not Deferred): the tool exists for
// interactive "화면에 띄워줘/나란히 보여줘" turns, where a fetch_tools round
// trip would cost more than the small schema does.
func RegisterWorkstationTool(registry toolport.ToolRegistrar, deps WorkstationDeps) {
	registry.RegisterTool(toolport.ToolDef{
		Name:        "workstation",
		Description: "데스크톱 워크스테이션(Andromeda)의 화면을 직접 조종한다 — 자료를 말로만 설명하지 말고 화면에 띄워라. action: open(화면 열기, view 또는 위키 path) | split(분할 추가, view — 최대 3분할) | close(분할 닫기) | focus(포커스) | layout(분할 일괄 지정, views=\"mail,calendar\") | wiki(위키 페이지, path) | spotlight(항목 강조 — view+ref를 열고 하이라이트, '여기 보세요') | prefill(할일 초안 — view=todo+title(+due,note)로 모달을 채워 열기, 저장은 사용자 클릭). view 키: today|projects|progress|todo|notebook|mail|calendar|wiki|search|people|crons|fleet|workfeed|approvals|groupware|skills|rsi|observe|sitemap. search는 query로 검색어 주입, mail/approvals는 open/split에 date(YYYY-MM-DD)를 더해 그 날짜 페이지로 점프. 쓰는 시점: ① 사용자가 '보여줘/띄워줘/나란히' 요청 ② 특정 프로젝트·문서·메일을 논의 중이면 관련 화면을 선제 오픈(예: 프로젝트 얘기 → layout \"wiki,mail\" + wiki path) — 묻지 말고 띄우고 한 줄로 알릴 것 ③ 모바일에서 '데스크톱에 준비해놔' 원격 예약 ④ 답변에서 특정 항목을 짚을 때 spotlight ⑤ 대화에서 할일이 도출되면 prefill로 초안 제시. '워룸' 요청 = layout으로 위키+메일+피드 3분할. 데스크톱 앱이 연결돼 있어야 한다(미연결이면 조용히 생략하고 말로 답).",
		InputSchema: schema.WorkstationToolSchema(),
		Fn:          runtimeops.ToolWorkstation(runtimeops.WorkstationCommandFunc(deps.Send), runtimeops.WorkstationUsageHintFunc(deps.Hint)),
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
