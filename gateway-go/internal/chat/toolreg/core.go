// Package toolreg provides tool registration wiring — connecting tool
// implementations (from tools/) with their JSON schemas (from tool_schemas_gen.go)
// and registering them into a ToolRegistrar (implemented by chat.ToolRegistry).
//
// Dependency flow: toolreg/ -> toolctx/ (types), toolreg/ -> tools/ (implementations).
// toolreg/ never imports chat/ — the chat package calls toolreg functions.
package toolreg

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/autoresearch"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/web"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// SglangDeps holds optional sglang integration functions for tools that need
// local LLM access (pilot). Injected by chat/ to avoid importing sglang code
// from toolreg/.
type SglangDeps struct {
	CheckSglangHealth func() bool   // may be nil
	BaseURL           func() string // returns sglang base URL; may be nil
}

// RegisterCoreTools populates the tool registrar with all core agent tools.
// It delegates to domain-specific Register*Tools functions.
//
// sglang may be nil; tools that require sglang (pilot) degrade gracefully.
func RegisterCoreTools(registry toolctx.ToolRegistrar, deps *toolctx.CoreToolDeps, sglang *SglangDeps) {
	RegisterFSTools(registry, deps, sglang)
	RegisterProcessTools(registry, &deps.Process)
	RegisterWebTools(registry)
	RegisterSessionTools(registry, &deps.Sessions)
	RegisterChronoTools(registry)
	RegisterInfraTools(registry, &deps.Vega, sglang)
	RegisterMediaTools(registry, deps.LLMClient, deps.DefaultModel)
	RegisterDataTools(registry)
	RegisterRoutineTools(registry, &deps.Chrono, deps.LLMClient, deps.DefaultModel, &deps.Vega)
	RegisterAdvancedTools(registry, deps.WorkspaceDir)
	RegisterHiddenTools(registry, deps.AgentLog)

	// Autonomous continuation: LLM calls this to request a new run after current completes.
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "continue_run",
		Description: "Signal that you have more work to do and want to start a new autonomous run. Use when the current run's tool-call budget is nearly exhausted but the task is not yet complete.",
		InputSchema: continueRunToolSchema(),
		Fn:          tools.ToolContinueRun(),
		Deferred:    true,
	})

	// NOTE: Pilot tool is registered separately by chat.RegisterCoreTools
	// because it depends on sglang hooks that live in the chat package.
	// NOTE: fetch_tools is registered by chat.RegisterCoreTools because it
	// needs a FetchToolsRegistry interface that chat.ToolRegistry implements.
}

// FetchToolsSchema returns the fetch_tools schema for external registration.
func FetchToolsSchema() map[string]any { return fetchToolsToolSchema() }

// RegisterAutoresearchTool registers the autoresearch tool with the given runner.
// Called separately from RegisterCoreTools because the runner is created by the
// server layer and not part of CoreToolDeps.
func RegisterAutoresearchTool(registry toolctx.ToolRegistrar, runner *autoresearch.Runner) {
	if runner == nil {
		return
	}
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "autoresearch",
		Description: "Autonomous experiment loop (karpathy/autoresearch). Iteratively modifies code, runs experiments, evaluates a scalar metric, and keeps improvements or reverts failures — all without human intervention. Actions: init (configure), start (begin loop), stop (halt), status (check progress), results (get log)",
		InputSchema: autoresearchToolSchema(),
		Fn:          tools.ToolAutoresearch(runner),
		Deferred:    true,
	})
}

// RegisterFSTools registers file-system, code analysis, and git tools.
func RegisterFSTools(registry toolctx.ToolRegistrar, deps *toolctx.CoreToolDeps, sglang *SglangDeps) {
	workspaceDir := deps.WorkspaceDir
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "read",
		Description:     "Read file contents with line numbers for code review (default: 2000 lines). Use offset/limit for large files; equivalent to a clean bat/cat -n view",
		InputSchema:     readToolSchema(),
		Fn:              tools.ToolRead(workspaceDir),
		ConcurrencySafe: true,
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
		Name:            "grep",
		Description:     "Regex search across files (rg / ripgrep). Use include/fileType to narrow scope. Returns file:line:match format",
		InputSchema:     grepToolSchema(),
		Fn:              tools.ToolGrep(workspaceDir),
		ConcurrencySafe: true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "find",
		Description:     "Fast file search by glob pattern (fd-backed when available; e.g. \"**/*.go\"). Use grep to search inside files instead",
		InputSchema:     findToolSchema(),
		Fn:              tools.ToolFind(workspaceDir),
		ConcurrencySafe: true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "multi_edit",
		Description: "Batch search-and-replace across multiple files in one call. Up to 50 edits. Essential for refactoring, renaming symbols, updating imports",
		InputSchema: multiEditToolSchema(),
		Fn:          tools.ToolMultiEdit(workspaceDir),
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "tree",
		Description:     "Display directory tree with depth control. Uses eza/exa for fast default listings when available. Filters: dirs_only, pattern glob, show_hidden. Skips node_modules/.git/target etc",
		InputSchema:     treeToolSchema(),
		Fn:              tools.ToolTree(workspaceDir),
		ConcurrencySafe: true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "diff",
		Description:     "Git diff and file comparison. Modes: staged, unstaged, all (vs HEAD), commit (show commit), branch (compare branches), files (compare two files). Options: stat_only, context_lines, path filter",
		InputSchema:     diffToolSchema(),
		Fn:              tools.ToolDiff(workspaceDir),
		ConcurrencySafe: true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "analyze",
		Description:     "Code analysis: outline (file structure), symbols (find definitions), references (find usages), imports (dependency graph), signature (function signatures). Supports Go (AST) and Rust (regex)",
		InputSchema:     analyzeToolSchema(),
		Fn:              tools.ToolAnalyze(workspaceDir),
		Deferred:        true,
		ConcurrencySafe: true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "test",
		Description: "Run tests/builds (go/cargo/make). Actions: run, build, check. Structured pass/fail/skip results",
		InputSchema: testToolSchema(),
		Fn:          tools.ToolTest(workspaceDir),
		Deferred:    true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "git",
		Description: "Git operations: status, commit, log, branch, stash, blame, tag, merge, rebase, reset, remote, clean",
		InputSchema: gitToolSchema(),
		Fn:          tools.ToolGit(workspaceDir),
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "memory",
		Description:     "Unified memory: search facts + files, get/set/forget individual facts, deep recall, diary logging. Actions: search (default), get, set, forget, recall (deep), status, browse, log (append detailed narrative to diary), daily (read recent diary entries)",
		InputSchema:     memoryToolSchema(),
		Fn:              tools.ToolMemory(&deps.Vega, workspaceDir, slog.Default()),
		ConcurrencySafe: true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "gateway",
		Description: "Gateway self-management: config read/write, restart (SIGUSR1), git pull + rebuild",
		InputSchema: gatewayToolSchema(),
		Fn:          tools.ToolGateway(workspaceDir),
	})

	// Spillover: read full content of a previously spilled large tool result.
	if deps.SpilloverStore != nil {
		registry.RegisterTool(toolctx.ToolDef{
			Name:            "read_spillover",
			Description:     "Read the full content of a previous large tool result by spill ID. Use when a tool result was too large and was replaced with a preview",
			InputSchema:     readSpilloverToolSchema(),
			Fn:              tools.ToolSpilloverRead(deps.SpilloverStore),
			Deferred:        true,
			ConcurrencySafe: true,
		})
	}

	registry.RegisterTool(toolctx.ToolDef{
		Name:        "github_webhook",
		Description: "Manage GitHub webhooks for KAIROS: register Deneb's /webhook/github endpoint on a repo, list existing webhooks, delete, or check Deneb-side config status",
		InputSchema: githubWebhookToolSchema(),
		Fn:          tools.ToolGitHubWebhook(),
		Deferred:    true,
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
		Name:            "process",
		Description:     "Manage background exec sessions: list running, poll/log output, kill by sessionId",
		InputSchema:     processToolSchema(),
		Fn:              tools.ToolProcess(d.Mgr),
		ConcurrencySafe: true,
	})
}

// RegisterWebTools registers web fetch and HTTP tools.
func RegisterWebTools(registry toolctx.ToolRegistrar) {
	webCache := web.NewFetchCache()
	sglang := web.NewSGLangExtractor()
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "web",
		Description:     "Search the web, fetch URLs, or search+auto-fetch in one call. Modes: {url:...} fetch, {query:...} search, {query:...,fetch:N} search+fetch",
		InputSchema:     webToolSchema(),
		Fn:              web.Tool(webCache, sglang),
		ConcurrencySafe: true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "http",
		Description:     "Make HTTP API requests with headers, JSON body, and auth. Returns status + headers + body",
		InputSchema:     httpToolSchema(),
		Fn:              tools.ToolHTTP(),
		ConcurrencySafe: true,
	})
}

// RegisterSessionTools registers session management tools.
func RegisterSessionTools(registry toolctx.ToolRegistrar, d *toolctx.SessionDeps) {
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "sessions_list",
		Description:     "List active sessions with kind/status. Filter by kinds: main, group, cron, hook",
		InputSchema:     sessionsListToolSchema(),
		Fn:              tools.ToolSessionsList(d.Manager),
		Deferred:        true,
		ConcurrencySafe: true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "sessions_history",
		Description:     "Fetch message history from another session (default: last 20 messages)",
		InputSchema:     sessionsHistoryToolSchema(),
		Fn:              tools.ToolSessionsHistory(d.Transcript),
		Deferred:        true,
		ConcurrencySafe: true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "sessions_search",
		Description:     "Search all past session transcripts by keyword",
		InputSchema:     sessionsSearchToolSchema(),
		Fn:              tools.ToolSessionsSearch(d.Transcript),
		Deferred:        true,
		ConcurrencySafe: true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "sessions_send",
		Description: "Send a message to another session (defaults to \"main\" if sessionKey omitted)",
		InputSchema: sessionsSendToolSchema(),
		Fn:          tools.ToolSessionsSend(d),
		Deferred:    true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "sessions_spawn",
		Description: "Create an isolated sub-agent session for parallel work. Use subagents to monitor",
		InputSchema: sessionsSpawnToolSchema(),
		Fn:          tools.ToolSessionsSpawn(d),
		Deferred:    true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "subagents",
		Description: "Monitor and control sub-agents: list status, steer with messages, or kill. Defaults to list",
		InputSchema: subagentsToolSchema(),
		Fn:          tools.ToolSubagents(d),
		Deferred:    true,
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
func RegisterRoutineTools(registry toolctx.ToolRegistrar, chrono *toolctx.ChronoDeps, llmClient *llm.Client, defaultModel string, vega *toolctx.VegaDeps) {
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "cron",
		Description: "Schedule recurring jobs (cron expressions). Actions: status, list, add, update, remove, run, wake",
		InputSchema: cronToolSchema(),
		Fn:          tools.ToolCron(chrono),
		Deferred:    true,
	})

	// Build gmail pipeline deps from available subsystems.
	gmailPipelineDeps := tools.GmailPipelineDeps{
		LLMClient:    llmClient,
		DefaultModel: defaultModel,
	}
	if vega != nil {
		gmailPipelineDeps.MemStore = vega.MemoryStore
		gmailPipelineDeps.MemEmbed = vega.MemoryEmbedder
	}
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "gmail",
		Description: "Gmail (native OAuth2): inbox, search, read, send, reply, labels, analyze (LLM 이메일 분석, multi-stage pipeline). Auth: ~/.deneb/credentials/gmail_client.json + gmail_token.json",
		InputSchema: gmailToolSchema(),
		Fn:          tools.ToolGmail(gmailPipelineDeps),
		Deferred:    true,
	})
	// Morning letter: needs executor for web search calls.
	if exec, ok := registry.(toolctx.ToolExecutor); ok {
		registry.RegisterTool(toolctx.ToolDef{
			Name:        "morning_letter",
			Description: "Collect daily morning briefing data (모닝레터). Fetches weather, exchange rates, copper price (MetalpriceAPI), calendar, and email in parallel. Returns structured JSON for you to compose the final letter",
			InputSchema: morningLetterToolSchema(),
			Fn:          tools.ToolMorningLetter(exec),
			Deferred:    true,
		})
	}
}

// buildSglangProbe converts toolreg SglangDeps into the tools.SglangProbe
// struct expected by ToolHealthCheck.
func buildSglangProbe(sglang *SglangDeps) tools.SglangProbe {
	var probe tools.SglangProbe
	if sglang != nil {
		probe.CheckHealth = sglang.CheckSglangHealth
		probe.BaseURL = sglang.BaseURL
	}
	if probe.CheckHealth == nil {
		probe.CheckHealth = func() bool { return false }
	}
	if probe.BaseURL == nil {
		probe.BaseURL = func() string { return "http://localhost:30000/v1" }
	}
	return probe
}

// RegisterInfraTools registers infrastructure health-check tools.
// RegisterSkillsTools registers skill discovery tools.
func RegisterSkillsTools(registry toolctx.ToolRegistrar, getSnapshot tools.SkillsSnapshotProvider) {
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "skills_list",
		Description:     "List available skills for specialized tasks. Use when the current task might match a skill not in the system prompt.",
		InputSchema:     skillsListToolSchema(),
		Fn:              tools.ToolSkillsList(getSnapshot),
		Deferred:        true,
		ConcurrencySafe: true,
	})
}

func RegisterInfraTools(registry toolctx.ToolRegistrar, d *toolctx.VegaDeps, sglang *SglangDeps) {
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "health_check",
		Description:     "인프라 상태 점검: embedding (Gemini), reranker (Jina), sglang (로컬 LLM), memory (aurora-memory DB). component: all (기본), embedding, reranker, sglang, memory",
		InputSchema:     healthCheckToolSchema(),
		Fn:              tools.ToolHealthCheck(d, buildSglangProbe(sglang)),
		Deferred:        true,
		ConcurrencySafe: true,
	})
}

// RegisterMediaTools registers image analysis and media delivery tools.
func RegisterMediaTools(registry toolctx.ToolRegistrar, llmClient *llm.Client, defaultModel string) {
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "image",
		Description:     "Analyze images with a vision model (up to 20 local files or URLs). Accepts optional prompt",
		InputSchema:     imageToolSchema(),
		Fn:              tools.ToolImage(llmClient, defaultModel),
		Deferred:        true,
		ConcurrencySafe: true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "youtube_transcript",
		Description:     "Extract transcript/subtitles and metadata from a YouTube video",
		InputSchema:     youtubeTranscriptToolSchema(),
		Fn:              tools.ToolYouTubeTranscript(),
		Deferred:        true,
		ConcurrencySafe: true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "send_file",
		Description: "Send a file to the user (auto-detects: photo/video/audio/document). Max 50 MB",
		InputSchema: sendFileToolSchema(),
		Fn:          tools.ToolSendFile(),
		Deferred:    true,
	})
}

// RegisterDataTools registers persistent storage tools.
func RegisterDataTools(registry toolctx.ToolRegistrar) {
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "kv",
		Description:     "Persistent key-value store (survives restarts). Actions: get, set, delete, list. Dot-separated keys for namespaces",
		InputSchema:     kvToolSchema(),
		Fn:              tools.ToolKV(),
		Deferred:        true,
		ConcurrencySafe: true,
	})
}

// RegisterAdvancedTools registers composed tools that combine basic tool
// operations into higher-level, multi-step workflows.
func RegisterAdvancedTools(registry toolctx.ToolRegistrar, workspaceDir string) {
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "batch_read",
		Description:     "Read multiple files in one call (up to 20). Each file supports offset/limit/function extraction. Partial failures reported individually without aborting",
		InputSchema:     batchReadToolSchema(),
		Fn:              tools.ToolBatchRead(workspaceDir),
		Deferred:        true,
		ConcurrencySafe: true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "search_and_read",
		Description:     "Grep for a pattern then auto-read matching files with surrounding context. Combines grep+read into one step. Returns file content around each match",
		InputSchema:     searchAndReadToolSchema(),
		Fn:              tools.ToolSearchAndRead(workspaceDir),
		Deferred:        true,
		ConcurrencySafe: true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "inspect",
		Description:     "Deep code inspection: file outline + imports + git history in one call. depth=shallow (outline+imports), deep (+git log+stats), symbol (+definition+references+blame). Auto-promotes to symbol depth when symbol param is set",
		InputSchema:     inspectToolSchema(),
		Fn:              tools.ToolInspect(workspaceDir),
		Deferred:        true,
		ConcurrencySafe: true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "apply_patch",
		Description: "Apply a unified diff patch (git diff format). Handles multi-file, multi-hunk patches atomically via git apply. Use dry_run=true to verify before applying",
		InputSchema: applyPatchToolSchema(),
		Fn:          tools.ToolApplyPatch(workspaceDir),
		Deferred:    true,
	})
}

// RegisterHiddenTools registers pilot-only hidden tools.
func RegisterHiddenTools(registry toolctx.ToolRegistrar, agentLog *agentlog.Writer) {
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "agent_logs",
		Description:     "현재 세션의 이전 에이전트 런 상세 로그를 조회합니다. 문제 진단, 이전 실행 결과 확인, 도구 실행 시간 분석에 사용합니다",
		InputSchema:     agentLogsToolSchema(),
		Fn:              tools.ToolAgentLogs(agentLog),
		Hidden:          true,
		ConcurrencySafe: true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:            "gateway_logs",
		Description:     "게이트웨이 프로세스 로그를 조회합니다. 레벨/패키지/패턴 필터링 지원. 서버 오류 진단, 요청 추적, 성능 분석에 사용합니다",
		InputSchema:     gatewayLogsToolSchema(),
		Fn:              tools.ToolGatewayLogs(),
		Hidden:          true,
		ConcurrencySafe: true,
	})
}
