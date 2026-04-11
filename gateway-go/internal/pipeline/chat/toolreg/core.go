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
}

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
		Name:        "find",
		Description: "Fast file search by glob pattern (fd-backed when available; e.g. \"**/*.go\"). Use grep to search inside files instead",
		InputSchema: findToolSchema(),
		Fn:          tools.ToolFind(workspaceDir),
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "diff",
		Description: "Git diff and file comparison. Modes: staged, unstaged, all (vs HEAD), commit (show commit), branch (compare branches), files (compare two files). Options: stat_only, context_lines, path filter",
		InputSchema: diffToolSchema(),
		Fn:          tools.ToolDiff(workspaceDir),
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "git",
		Description: "Git operations: status, commit, log, branch, stash, blame, tag, merge, rebase, reset, remote, clean",
		InputSchema: gitToolSchema(),
		Fn:          tools.ToolGit(workspaceDir),
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "gateway",
		Description: "Gateway self-management: config read/write, restart (SIGUSR1), git pull + rebuild",
		InputSchema: gatewayToolSchema(),
		Fn:          tools.ToolGateway(workspaceDir),
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
