package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/polaris"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/web"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/media"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// CoreToolDeps holds all dependencies for core agent tools.
// It composes focused dep structs for each tool group. Use the domain-specific
// Register*Tools functions for fine-grained wiring; RegisterCoreTools is a
// convenience wrapper that delegates to all of them.
//
// Fields that support late-binding (Vega.Backend, Sessions.SendFn, Chrono.SendFn)
// can be set after initial construction — the tool closures read them at call time
// through the pointer to the embedded sub-struct.
type CoreToolDeps struct {
	WorkspaceDir string
	Process      ProcessDeps
	Sessions     SessionDeps
	Chrono       ChronoDeps
	Vega         VegaDeps
	LLMClient    *llm.Client
	DefaultModel string
	AgentLog     *agentlog.Writer
}

// RegisterCoreTools populates the tool registry with all core agent tools.
// It is a convenience wrapper that delegates to the domain-specific Register*Tools functions.
func RegisterCoreTools(registry *ToolRegistry, deps *CoreToolDeps) {
	RegisterFSTools(registry, deps.WorkspaceDir)
	RegisterProcessTools(registry, &deps.Process)
	RegisterWebTools(registry)
	RegisterSessionTools(registry, &deps.Sessions)
	RegisterChronoTools(registry, &deps.Chrono)
	RegisterVegaTools(registry, &deps.Vega)
	RegisterMediaTools(registry, deps.LLMClient)
	RegisterDataTools(registry, deps.LLMClient, deps.DefaultModel)
	RegisterHiddenTools(registry, deps.AgentLog)
	// Morning letter: needs registry for web search calls.
	registry.RegisterTool(ToolDef{
		Name:        "morning_letter",
		Description: "Collect daily morning briefing data (모닝레터). Fetches weather, exchange rates, copper price (MetalpriceAPI), calendar, and email in parallel. Returns structured JSON for you to compose the final letter",
		InputSchema: morningLetterToolSchema(),
		Fn:          toolMorningLetter(registry),
	})
	// Pilot registered last: it takes the registry itself as a dependency.
	registry.RegisterTool(ToolDef{
		Name:        "pilot",
		Description: "Local AI analysis — gathers tool outputs and analyzes in one call (free). Shortcuts: file, exec, grep, find, url, diff, test, tree, git_log, health, memory, vega, image + more",
		InputSchema: pilotToolSchema(),
		Fn:          toolPilot(registry, deps.WorkspaceDir),
	})
	RegisterDefaultPostProcessors(registry)
}

// RegisterFSTools registers file-system, code analysis, and git tools.
// These tools only require the workspace directory.
func RegisterFSTools(registry *ToolRegistry, workspaceDir string) {
	registry.RegisterTool(ToolDef{
		Name:        "read",
		Description: "Read file contents with line numbers (default: 2000 lines). Use offset/limit for large files",
		InputSchema: readToolSchema(),
		Fn:          toolRead(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "write",
		Description: "Create or overwrite a file. Auto-creates parent directories. Use edit for partial changes",
		InputSchema: writeToolSchema(),
		Fn:          toolWrite(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "edit",
		Description: "Search-and-replace in a file. old_string must be unique unless replace_all=true. Read first to find the exact string",
		InputSchema: editToolSchema(),
		Fn:          toolEdit(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "grep",
		Description: "Regex search across files (ripgrep). Use include/fileType to narrow scope. Returns file:line:match format",
		InputSchema: grepToolSchema(),
		Fn:          toolGrep(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "find",
		Description: "Find files by glob pattern (e.g. \"**/*.go\"). Use grep to search inside files instead",
		InputSchema: findToolSchema(),
		Fn:          toolFind(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "multi_edit",
		Description: "Batch search-and-replace across multiple files in one call. Up to 50 edits. Essential for refactoring, renaming symbols, updating imports",
		InputSchema: multiEditToolSchema(),
		Fn:          toolMultiEdit(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "tree",
		Description: "Display directory tree with depth control. Filters: dirs_only, pattern glob, show_hidden. Skips node_modules/.git/target etc",
		InputSchema: treeToolSchema(),
		Fn:          toolTree(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "diff",
		Description: "Git diff and file comparison. Modes: staged, unstaged, all (vs HEAD), commit (show commit), branch (compare branches), files (compare two files). Options: stat_only, context_lines, path filter",
		InputSchema: diffToolSchema(),
		Fn:          toolDiff(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "analyze",
		Description: "Code analysis: outline (file structure), symbols (find definitions), references (find usages), imports (dependency graph), signature (function signatures). Supports Go (AST) and Rust (regex)",
		InputSchema: analyzeToolSchema(),
		Fn:          toolAnalyze(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "test",
		Description: "Run tests/builds (go/cargo/make). Actions: run, build, check. Structured pass/fail/skip results",
		InputSchema: testToolSchema(),
		Fn:          toolTest(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "git",
		Description: "Git operations: status, commit, log, branch, stash, blame, tag, merge, rebase, reset, remote, clean",
		InputSchema: gitToolSchema(),
		Fn:          toolGit(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "memory_search",
		Description: "Search MEMORY.md + memory/*.md by keyword. Returns matched lines with context",
		InputSchema: memorySearchToolSchema(),
		Fn:          toolMemorySearch(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "polaris",
		Description: "Deneb system knowledge agent. Ask any question about the Deneb system — Polaris autonomously searches docs, guides, and source code to synthesize a direct answer",
		InputSchema: polarisToolSchema(),
		Fn: polaris.NewHandlerWithDeps(workspaceDir, polaris.Deps{
			LLM:    callLocalLLM,
			Health: checkSglangHealth,
			Tools:  &polaris.ReadOnlyExecutor{Inner: registry},
		}),
	})
	registry.RegisterTool(ToolDef{
		Name:        "gateway",
		Description: "Gateway self-management: config read/write, restart (SIGUSR1), git pull + rebuild",
		InputSchema: gatewayToolSchema(),
		Fn:          toolGateway(workspaceDir),
	})
}

// RegisterProcessTools registers exec and process management tools.
func RegisterProcessTools(registry *ToolRegistry, d *ProcessDeps) {
	registry.RegisterTool(ToolDef{
		Name:        "exec",
		Description: "Run a shell command (bash -c). Default timeout 30s, max 5min. Use background=true for long tasks, then process to check",
		InputSchema: execToolSchema(),
		Fn:          toolExec(d.Mgr, d.WorkspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "process",
		Description: "Manage background exec sessions: list running, poll/log output, kill by sessionId",
		InputSchema: processToolSchema(),
		Fn:          toolProcess(d.Mgr),
	})
}

// RegisterWebTools registers web fetch and HTTP tools (no external deps required).
func RegisterWebTools(registry *ToolRegistry) {
	webCache := web.NewFetchCache()
	sglang := web.NewSGLangExtractor()
	registry.RegisterTool(ToolDef{
		Name:        "web",
		Description: "Search the web, fetch URLs, or search+auto-fetch in one call. Modes: {url:...} fetch, {query:...} search, {query:...,fetch:N} search+fetch",
		InputSchema: webToolSchema(),
		Fn:          web.Tool(webCache, sglang),
	})
	registry.RegisterTool(ToolDef{
		Name:        "http",
		Description: "Make HTTP API requests with headers, JSON body, and auth. Returns status + headers + body",
		InputSchema: httpToolSchema(),
		Fn:          toolHTTP(),
	})
}

// RegisterSessionTools registers session management tools.
// d is passed by pointer so late-bound fields (SendFn) are visible at call time.
func RegisterSessionTools(registry *ToolRegistry, d *SessionDeps) {
	registry.RegisterTool(ToolDef{
		Name:        "sessions_list",
		Description: "List active sessions with kind/status. Filter by kinds: main, group, cron, hook",
		InputSchema: sessionsListToolSchema(),
		Fn:          toolSessionsList(d.Manager),
	})
	registry.RegisterTool(ToolDef{
		Name:        "sessions_history",
		Description: "Fetch message history from another session (default: last 20 messages)",
		InputSchema: sessionsHistoryToolSchema(),
		Fn:          toolSessionsHistory(d.Transcript),
	})
	registry.RegisterTool(ToolDef{
		Name:        "sessions_search",
		Description: "Search all past session transcripts by keyword",
		InputSchema: sessionsSearchToolSchema(),
		Fn:          toolSessionsSearch(d.Transcript),
	})
	registry.RegisterTool(ToolDef{
		Name:        "sessions_send",
		Description: "Send a message to another session (defaults to \"main\" if sessionKey omitted)",
		InputSchema: sessionsSendToolSchema(),
		Fn:          toolSessionsSend(d),
	})
	registry.RegisterTool(ToolDef{
		Name:        "sessions_spawn",
		Description: "Create an isolated sub-agent session for parallel work. Use subagents to monitor",
		InputSchema: sessionsSpawnToolSchema(),
		Fn:          toolSessionsSpawn(d),
	})
	registry.RegisterTool(ToolDef{
		Name:        "subagents",
		Description: "Monitor and control sub-agents: list status, steer with messages, or kill. Defaults to list",
		InputSchema: subagentsToolSchema(),
		Fn:          toolSubagents(d),
	})
}

// RegisterChronoTools registers the cron scheduling tool.
// d is passed by pointer so late-bound fields (SendFn) are visible at call time.
func RegisterChronoTools(registry *ToolRegistry, d *ChronoDeps) {
	registry.RegisterTool(ToolDef{
		Name:        "cron",
		Description: "Schedule recurring jobs (cron expressions). Actions: status, list, add, update, remove, run, wake",
		InputSchema: cronToolSchema(),
		Fn:          toolCron(d),
	})
	registry.RegisterTool(ToolDef{
		Name:        "message",
		Description: "Send messages to the user's channel. Actions: send, reply, react, thread-reply. Use for proactive sends",
		InputSchema: messageToolSchema(),
		Fn:          toolMessage(),
	})
}

// RegisterVegaTools registers vega search and health-check tools.
// d is passed by pointer so late-bound fields (Backend) are visible at call time.
func RegisterVegaTools(registry *ToolRegistry, d *VegaDeps) {
	registry.RegisterTool(ToolDef{
		Name:        "vega",
		Description: "Search project knowledge base (Vega). Hybrid BM25 + semantic search across all projects. Actions: search (default), ask",
		InputSchema: vegaToolSchema(),
		Fn:          toolVega(d),
	})
	registry.RegisterTool(ToolDef{
		Name:        "health_check",
		Description: "인프라 상태 점검: embedding (Gemini), reranker (Jina), sglang (로컬 LLM), memory (aurora-memory DB). component: all (기본), embedding, reranker, sglang, memory",
		InputSchema: healthCheckToolSchema(),
		Fn:          toolHealthCheck(d),
	})
}

// RegisterMediaTools registers image analysis and media delivery tools.
func RegisterMediaTools(registry *ToolRegistry, llmClient *llm.Client) {
	registry.RegisterTool(ToolDef{
		Name:        "image",
		Description: "Analyze images with a vision model (up to 20 local files or URLs). Accepts optional prompt",
		InputSchema: imageToolSchema(),
		Fn:          toolImage(llmClient),
	})
	registry.RegisterTool(ToolDef{
		Name:        "youtube_transcript",
		Description: "Extract transcript/subtitles and metadata from a YouTube video",
		InputSchema: youtubeTranscriptToolSchema(),
		Fn:          toolYouTubeTranscript(),
	})
	registry.RegisterTool(ToolDef{
		Name:        "send_file",
		Description: "Send a file to the user (auto-detects: photo/video/audio/document). Max 50 MB",
		InputSchema: sendFileToolSchema(),
		Fn:          toolSendFile(),
	})
}

// RegisterDataTools registers persistent storage tools.
func RegisterDataTools(registry *ToolRegistry, llmClient *llm.Client, defaultModel string) {
	registry.RegisterTool(ToolDef{
		Name:        "kv",
		Description: "Persistent key-value store (survives restarts). Actions: get, set, delete, list. Dot-separated keys for namespaces",
		InputSchema: kvToolSchema(),
		Fn:          toolKV(),
	})
	registry.RegisterTool(ToolDef{
		Name:        "gmail",
		Description: "Gmail (native OAuth2): inbox, search, read, send, reply, labels, analyze (LLM 이메일 분석). Auth: ~/.deneb/credentials/gmail_client.json + gmail_token.json",
		InputSchema: gmailToolSchema(),
		Fn:          toolGmail(llmClient, defaultModel),
	})
}

// RegisterHiddenTools registers pilot-only hidden tools.
func RegisterHiddenTools(registry *ToolRegistry, agentLog *agentlog.Writer) {
	registry.RegisterTool(ToolDef{
		Name:        "agent_logs",
		Description: "현재 세션의 이전 에이전트 런 상세 로그를 조회합니다. 문제 진단, 이전 실행 결과 확인, 도구 실행 시간 분석에 사용합니다",
		InputSchema: agentLogsToolSchema(),
		Fn:          toolAgentLogs(agentLog),
		Hidden:      true,
	})
	registry.RegisterTool(ToolDef{
		Name:        "gateway_logs",
		Description: "게이트웨이 프로세스 로그를 조회합니다. 레벨/패키지/패턴 필터링 지원. 서버 오류 진단, 요청 추적, 성능 분석에 사용합니다",
		InputSchema: gatewayLogsToolSchema(),
		Fn:          toolGatewayLogs(),
		Hidden:      true,
	})
}

// --- Exec tool ---

func toolExec(procMgr *process.Manager, defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Command    string  `json:"command"`
			Workdir    string  `json:"workdir"`
			Timeout    float64 `json:"timeout"`
			Background bool    `json:"background"`
			Structured bool    `json:"structured"`
		}
		if err := jsonutil.UnmarshalInto("exec params", input, &p); err != nil {
			return "", err
		}
		if p.Command == "" {
			return "", fmt.Errorf("command is required")
		}

		workDir := p.Workdir
		if workDir == "" {
			workDir = defaultDir
		}

		// Validate working directory exists.
		if info, err := os.Stat(workDir); err != nil || !info.IsDir() {
			return "", fmt.Errorf("working directory does not exist: %s", workDir)
		}

		timeoutMs := int64(30000)
		if p.Timeout > 0 {
			timeoutMs = int64(p.Timeout * 1000)
		}
		const maxTimeoutMs = 5 * 60 * 1000
		if timeoutMs > maxTimeoutMs {
			timeoutMs = maxTimeoutMs
		}

		if procMgr != nil {
			result := procMgr.Execute(ctx, process.ExecRequest{
				Command:    "bash",
				Args:       []string{"-c", p.Command},
				WorkingDir: workDir,
				TimeoutMs:  timeoutMs,
			})
			if p.Structured {
				return formatExecResultJSON(result), nil
			}
			return formatExecResult(result), nil
		}

		// Fallback: direct exec without process manager.
		execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
		defer cancel()
		start := time.Now()
		cmd := exec.CommandContext(execCtx, "bash", "-c", p.Command)
		cmd.Dir = workDir
		out, err := cmd.CombinedOutput()
		elapsed := time.Since(start)

		if p.Structured {
			exitCode := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					exitCode = -1
				}
			}
			result := map[string]any{
				"stdout":     string(out),
				"stderr":     "",
				"exit_code":  exitCode,
				"runtime_ms": elapsed.Milliseconds(),
				"timed_out":  execCtx.Err() != nil,
			}
			data, _ := json.MarshalIndent(result, "", "  ")
			return string(data), nil
		}

		if err != nil {
			return fmt.Sprintf("%s\n\nError: %s", string(out), err.Error()), nil
		}
		return string(out), nil
	}
}

// formatExecResultJSON returns process manager result as JSON.
func formatExecResultJSON(r *process.ExecResult) string {
	result := map[string]any{
		"stdout":     r.Stdout,
		"stderr":     r.Stderr,
		"exit_code":  r.ExitCode,
		"runtime_ms": r.RuntimeMs,
	}
	if r.Error != "" {
		result["error"] = r.Error
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data)
}

func formatExecResult(r *process.ExecResult) string {
	var sb strings.Builder
	if r.Stdout != "" {
		sb.WriteString(r.Stdout)
	}
	if r.Stderr != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("STDERR:\n")
		sb.WriteString(r.Stderr)
	}
	if r.Error != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("Error: ")
		sb.WriteString(r.Error)
	}
	if r.ExitCode != 0 {
		fmt.Fprintf(&sb, "\nExit code: %d", r.ExitCode)
	}
	if sb.Len() == 0 {
		return "(no output)"
	}
	return sb.String()
}

// --- Process tool ---

func toolProcess(procMgr *process.Manager) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action    string `json:"action"`
			SessionID string `json:"sessionId"`
		}
		if err := jsonutil.UnmarshalInto("process params", input, &p); err != nil {
			return "", err
		}
		if procMgr == nil {
			return "Process manager not available.", nil
		}

		switch p.Action {
		case "list":
			tracked := procMgr.List()
			if len(tracked) == 0 {
				return "No active processes.", nil
			}
			data, _ := json.MarshalIndent(tracked, "", "  ")
			return string(data), nil
		case "poll", "log":
			if p.SessionID == "" {
				return "", fmt.Errorf("sessionId is required for %s", p.Action)
			}
			t := procMgr.Get(p.SessionID)
			if t == nil {
				return fmt.Sprintf("Process %q not found.", p.SessionID), nil
			}
			data, _ := json.MarshalIndent(t, "", "  ")
			return string(data), nil
		case "kill":
			if p.SessionID == "" {
				return "", fmt.Errorf("sessionId is required for kill")
			}
			procMgr.Kill(p.SessionID)
			return fmt.Sprintf("Process %q killed.", p.SessionID), nil
		default:
			return fmt.Sprintf("Unknown process action: %q", p.Action), nil
		}
	}
}

// --- YouTube transcript tool ---

func toolYouTubeTranscript() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			URL string `json:"url"`
		}
		if err := jsonutil.UnmarshalInto("youtube_transcript params", input, &p); err != nil {
			return "", err
		}
		if p.URL == "" {
			return "", fmt.Errorf("url is required")
		}
		if !media.IsYouTubeURL(p.URL) {
			return "", fmt.Errorf("not a valid YouTube URL: %s", p.URL)
		}

		ytCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()

		result, err := media.ExtractYouTubeTranscript(ytCtx, p.URL)
		if err != nil {
			return "", fmt.Errorf("youtube transcript extraction failed: %w", err)
		}

		return media.FormatYouTubeResult(result), nil
	}
}
