// Package toolreg provides tool registration wiring — connecting tool
// implementations (from tools/) with their JSON schemas (from tool_schemas_gen.go)
// and registering them into a ToolRegistrar (implemented by chat.ToolRegistry).
//
// Dependency flow: toolreg/ -> toolctx/ (types), toolreg/ -> tools/ (implementations).
// toolreg/ never imports chat/ — the chat package calls toolreg functions.
package toolreg

import (
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
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
	RegisterWebTools(registry)
	RegisterSessionTools(registry, &deps.Sessions)
	RegisterChronoTools(registry)
	RegisterMediaTools(registry)
	var diaryDir string
	if deps.Wiki.Store != nil {
		diaryDir = deps.Wiki.Store.DiaryDir()
	}
	RegisterRoutineTools(registry, &deps.Chrono, deps.LLMClient, deps.DefaultModel, diaryDir)

	// NOTE: Pilot tool is registered separately by chat.RegisterCoreTools
	// because it depends on local AI hooks that live in the chat package.
	// NOTE: fetch_tools is registered by chat.RegisterCoreTools because it
	// needs a FetchToolsRegistry interface that chat.ToolRegistry implements.
}

// FetchToolsSchema returns the fetch_tools schema for external registration.
func FetchToolsSchema() map[string]any { return fetchToolsToolSchema() }

// RegisterPolarisTools registers the unified Polaris tool (search/describe/expand).
// Called separately because the store and localAI are not part of CoreToolDeps.
func RegisterPolarisTools(registry toolctx.ToolRegistrar, store *polaris.Store, localAI tools.LocalAIFunc) {
	if store == nil {
		return
	}
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "polaris",
		Description: "압축된 대화 이력 관리. search (키워드 검색), describe (요약 구조 조회), expand (원본 복원)",
		InputSchema: polarisToolSchema(),
		Fn:          tools.ToolPolaris(store, localAI),
		Deferred:    true,
	})
}

// RegisterFSTools registers file-system, code analysis, and git tools.
func RegisterFSTools(registry toolctx.ToolRegistrar, deps *toolctx.CoreToolDeps) {
	workspaceDir := deps.WorkspaceDir
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "read",
		Description: "Read file contents with line numbers for code review (default: 2000 lines). Use offset/limit for large files; equivalent to a clean bat/cat -n view",
		InputSchema: readToolSchema(),
		Fn:          tools.ToolRead(workspaceDir),
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "write",
		Description: "Create or overwrite a file. Auto-creates parent directories. Use edit for partial changes",
		InputSchema: writeToolSchema(),
		Fn:          tools.ToolWrite(workspaceDir),
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "edit",
		Description: "Search-and-replace in a file. old_string must be unique unless replace_all=true. Read first to find the exact string",
		InputSchema: editToolSchema(),
		Fn:          tools.ToolEdit(workspaceDir),
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "grep",
		Description: "Regex search across files (rg / ripgrep). Use include/fileType to narrow scope. Returns file:line:match format",
		InputSchema: grepToolSchema(),
		Fn:          tools.ToolGrep(workspaceDir),
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "git",
		Description: "Git operations: status, commit, log, branch, stash, blame, tag, merge, rebase, reset, remote, clean",
		InputSchema: gitToolSchema(),
		Fn:          tools.ToolGit(workspaceDir),
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "gateway",
		Description: "Gateway self-management: status, config_get/config_set (dotted paths), update (git pull + rebuild + restart), restart. Destructive actions require approval — the first call returns a needs_approval envelope; relay the Korean summary to the user and call the .confirmed variant after approval.",
		InputSchema: gatewayToolSchema(),
		Fn:          tools.ToolGateway(workspaceDir),
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

	// Graphify: knowledge-graph queries via the `graphify` CLI. Two graphs
	// are addressable via the `graph` arg: "wiki" (default; built each wiki
	// dream cycle) and "code" (built by `graphify update .` in the workspace).
	//
	// Description encourages **fused / connected** use: chain query → explain
	// → path across both graphs to answer "어떤 개념이 어느 코드로 구현되나"
	// or "이 함수가 어떤 결정/사람과 엮여 있나"-style questions.
	registry.RegisterTool(toolctx.ToolDef{
		Name: "graphify",
		Description: "지식 그래프 질의 (위키 개념 그래프 + 코드 호출 그래프). " +
			"graph=\"wiki\" (기본, 사람·프로젝트·기술·결정·선호 등 개념/관계 그래프, dreamer가 매 사이클 갱신) | " +
			"graph=\"code\" (코드 호출/import/contains 그래프, `graphify update .`로 빌드). " +
			"액션: query (자연어 질문→관련 노드 탐색), explain (한 노드와 이웃 요약), path (두 노드 간 최단 경로). " +
			"**융합적 사용 패턴 (필수 숙지):** " +
			"(a) 단순 검색이 아니라 **그래프 탐색**으로 사고하라 — query로 후보 노드를 찾고 explain으로 이웃을 펼친 뒤 path로 다른 영역과 연결. " +
			"(b) wiki+code 두 그래프를 **묶어서** 답하라 — '이 함수가 어떤 개념을 구현하나'면 code에서 함수 노드 → explain → 관련 docs/주석 노드 식별 후 wiki에서 같은 개념 query. " +
			"(c) explain 결과의 community 번호를 활용하라 — 같은 community 안의 노드는 의미적으로 한 묶음. " +
			"(d) 단발 질의로 끝내지 마라 — 한 질문에 query/explain/path를 2~3회 chaining해 답을 입체화. " +
			"(e) wiki search보다 graphify가 강한 상황: 관계·맥락·연쇄 추론이 필요할 때 (단순 키워드 룩업은 wiki/grep로 충분).",
		InputSchema: graphifyToolSchema(),
		Fn:          tools.ToolGraphify(workspaceDir),
		// Eager registration: this tool is core to the agent's
		// fused/connected reasoning over wiki + code, so we want it visible
		// in the default prompt rather than gated behind fetch_tools.
		Deferred: false,
	})
}

// RegisterProcessTools registers exec and process management tools.
func RegisterProcessTools(registry toolctx.ToolRegistrar, d *toolctx.ProcessDeps) {
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "exec",
		Description: "Run a shell command (bash -c). Default timeout 30s, max 5min. Use background=true for long tasks, then process to check",
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
func RegisterWebTools(registry toolctx.ToolRegistrar) {
	webCache := web.NewFetchCache()
	localAI := web.NewLocalAIExtractor()

	registry.RegisterTool(toolctx.ToolDef{
		Name:        "web",
		Description: "Web access: search the web or fetch page content. Use query for keyword search, url for direct fetch",
		InputSchema: webToolSchema(),
		Fn:          web.MergedTool(webCache, localAI),
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
func RegisterChronoTools(registry toolctx.ToolRegistrar) {
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "message",
		Description: "Send messages to the user's channel. Actions: send, reply, react, thread-reply. Use for proactive sends",
		InputSchema: messageToolSchema(),
		Fn:          tools.ToolMessage(),
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "clarify",
		Description: "Ask the user to resolve ambiguity with a Telegram button-tap choice. Sends the question + 2-5 labeled buttons; the agent's turn ends, and the user's choice arrives as a new user message on the next turn. Use only for genuine ambiguity the agent cannot resolve itself",
		InputSchema: clarifyToolSchema(),
		Fn:          tools.ToolClarify(),
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
}

// RegisterRoutineTools registers tools for recurring/scheduled tasks —
// things that sit between always-on core tools and on-demand skills.
// Typical trigger: cron scheduler, daily routines, periodic checks.
// diaryDir is the wiki diary directory for morning letter logging (empty = disabled).
func RegisterRoutineTools(registry toolctx.ToolRegistrar, chrono *toolctx.ChronoDeps, llmClient *llm.Client, defaultModel, diaryDir string) {
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "cron",
		Description: "Schedule recurring jobs (cron expressions). Actions: status, list, add, update, remove, run, wake",
		InputSchema: cronToolSchema(),
		Fn:          tools.ToolCron(chrono),
	})

	// Build gmail pipeline deps from available subsystems.
	gmailPipelineDeps := tools.GmailPipelineDeps{
		LLMClient:    llmClient,
		DefaultModel: defaultModel,
	}
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "gmail",
		Description: "Gmail (native OAuth2): inbox, search, read, send, reply, labels, analyze (LLM 이메일 분석, multi-stage pipeline). Auth: ~/.deneb/credentials/gmail_client.json + gmail_token.json",
		InputSchema: gmailToolSchema(),
		Fn:          tools.ToolGmail(gmailPipelineDeps),
		Deferred:    true,
	})
	// NOTE: morning_letter is no longer registered as a tool. The data collection
	// function (CollectMorningLetterData) is called directly by the boot/routine handler.
}

// RegisterSkillsTools registers the unified skills tool (list/create/patch/delete/read/list_files).
func RegisterSkillsTools(registry toolctx.ToolRegistrar, getSnapshot tools.SkillsSnapshotProvider, workspaceDir string, invalidateCache tools.SkillManageInvalidateFn) {
	registry.RegisterTool(toolctx.ToolDef{
		Name: "skills",
		Description: "Skill management: list (browse/search), create, patch, read, delete, list_files. " +
			"Use list when the current task might match a skill. Create reusable workflows from complex tasks.",
		InputSchema: skillsToolSchema(),
		Fn:          tools.ToolSkills(getSnapshot, workspaceDir, invalidateCache),
		Deferred:    true,
	})
}

// RegisterMediaTools registers media delivery tools.
func RegisterMediaTools(registry toolctx.ToolRegistrar) {
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "send_file",
		Description: "Send a file to the user (auto-detects: photo/video/audio/document). Max 50 MB",
		InputSchema: sendFileToolSchema(),
		Fn:          tools.ToolSendFile(),
		Deferred:    true,
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
			Description: "LLM 위키 지식베이스: search (검색), read (페이지 읽기), index (목차), write (작성/수정), log (일지), daily (최근 일지), status (통계). 과거 결정/맥락/인물/프로젝트 등 장기 지식을 마크다운 위키로 관리",
			InputSchema: wikiToolSchema(),
			Fn:          tools.ToolWiki(wikiDeps, workspaceDir),
		})
	}
}
