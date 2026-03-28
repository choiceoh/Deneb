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
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/media"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// CoreToolDeps holds all dependencies for core agent tools.
// Fields may be nil — each tool gracefully degrades when its dependency is unavailable.
type CoreToolDeps struct {
	ProcessMgr   *process.Manager
	WorkspaceDir string
	CronSched    *cron.Scheduler
	Sessions     *session.Manager
	LLMClient    *llm.Client
	Transcript   TranscriptStore

	// SessionSendFn is a callback that sends a message to a target session,
	// triggering an agent run. Set after Handler creation to avoid circular deps.
	SessionSendFn func(sessionKey, message string) error

	// VegaBackend is the optional Vega search backend.
	// The vega tool gracefully degrades when this is nil.
	VegaBackend vega.Backend

	// AgentLog is the optional agent detail log writer.
	// The agent_logs tool gracefully degrades when this is nil.
	AgentLog *agentlog.Writer

	// MemoryStore is the optional aurora-memory structured store.
	// The health_check tool uses this to probe memory subsystem health.
	MemoryStore *memory.Store
}

// RegisterCoreTools populates the tool registry with all core agent tools.
// Tools that require external subsystems (e.g., process manager) are wired here.
func RegisterCoreTools(registry *ToolRegistry, deps *CoreToolDeps) {
	procMgr := deps.ProcessMgr
	workspaceDir := deps.WorkspaceDir
	cronSched := deps.CronSched
	// -- File tools (tools_fs.go, tools_fs_search.go) --
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

	// -- Code tools (tools_coding.go, tools_analyze.go, tools_test_runner.go) --
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
		Description: "Run tests/builds with structured results. Actions: run (tests with pass/fail/skip counts), build (compile check), check (lint/vet). Frameworks: go, cargo, make. Parses go test -json for structured output",
		InputSchema: testToolSchema(),
		Fn:          toolTest(workspaceDir),
	})

	// -- Git tool (tools_git.go) --
	registry.RegisterTool(ToolDef{
		Name:        "git",
		Description: "Git operations: status, commit, log, branch, stash, blame, tag, merge, rebase, cherry_pick, reset, remote, clean. Use diff tool for viewing diffs, apply_patch for applying patches",
		InputSchema: gitToolSchema(),
		Fn:          toolGit(workspaceDir),
	})

	// -- Exec/process tools --
	registry.RegisterTool(ToolDef{
		Name:        "exec",
		Description: "Run a shell command (bash -c). Default timeout 30s, max 5min. Use background=true for long tasks, then process to check",
		InputSchema: execToolSchema(),
		Fn:          toolExec(procMgr, workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "process",
		Description: "Manage background exec sessions: list running, poll/log output, kill by sessionId",
		InputSchema: processToolSchema(),
		Fn:          toolProcess(procMgr),
	})

	// -- Web tools --
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

	// -- Memory tools --
	registry.RegisterTool(ToolDef{
		Name:        "memory_search",
		Description: "Search MEMORY.md + memory/*.md by keyword. Returns matched lines with context",
		InputSchema: memorySearchToolSchema(),
		Fn:          toolMemorySearch(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "vega",
		Description: "Search project knowledge base (Vega). Hybrid BM25 + semantic search across all projects. Actions: search (default), ask",
		InputSchema: vegaToolSchema(),
		Fn:          toolVega(deps),
	})
	registry.RegisterTool(ToolDef{
		Name:        "polaris",
		Description: "Query Deneb system manual. actions: topics (doc tree), search (keyword search), read (read a doc), guides (27 AI-curated system guides in 4 categories: core, tools, runtime, infra). Use guides with category key to browse",
		InputSchema: polarisToolSchema(),
		Fn:          polaris.NewHandler(workspaceDir),
	})

	// -- System tools --
	registry.RegisterTool(ToolDef{
		Name:        "health_check",
		Description: "인프라 상태 점검: embedding (Gemini), reranker (Jina), sglang (로컬 LLM), memory (aurora-memory DB). component: all (기본), embedding, reranker, sglang, memory",
		InputSchema: healthCheckToolSchema(),
		Fn:          toolHealthCheck(deps),
	})
	registry.RegisterTool(ToolDef{
		Name:        "cron",
		Description: "Schedule recurring jobs (cron expressions). Actions: status, list, add, update, remove, run, wake",
		InputSchema: cronToolSchema(),
		Fn:          toolCron(cronSched, deps),
	})
	registry.RegisterTool(ToolDef{
		Name:        "message",
		Description: "Send messages to the user's channel. Actions: send, reply, react, thread-reply. Use for proactive sends",
		InputSchema: messageToolSchema(),
		Fn:          toolMessage(),
	})
	registry.RegisterTool(ToolDef{
		Name:        "gateway",
		Description: "Gateway self-management: config read/write, restart (SIGUSR1), git pull + rebuild",
		InputSchema: gatewayToolSchema(),
		Fn:          toolGateway(workspaceDir),
	})

	// -- Session tools --
	registry.RegisterTool(ToolDef{
		Name:        "sessions_list",
		Description: "List active sessions with kind/status. Filter by kinds: main, group, cron, hook",
		InputSchema: sessionsListToolSchema(),
		Fn:          toolSessionsList(deps.Sessions),
	})
	registry.RegisterTool(ToolDef{
		Name:        "sessions_history",
		Description: "Fetch message history from another session (default: last 20 messages)",
		InputSchema: sessionsHistoryToolSchema(),
		Fn:          toolSessionsHistory(deps.Transcript),
	})
	registry.RegisterTool(ToolDef{
		Name:        "sessions_search",
		Description: "Search all past session transcripts by keyword",
		InputSchema: sessionsSearchToolSchema(),
		Fn:          toolSessionsSearch(deps.Transcript),
	})
	registry.RegisterTool(ToolDef{
		Name:        "sessions_send",
		Description: "Send a message to another session (defaults to \"main\" if sessionKey omitted)",
		InputSchema: sessionsSendToolSchema(),
		Fn:          toolSessionsSend(deps),
	})
	registry.RegisterTool(ToolDef{
		Name:        "sessions_spawn",
		Description: "Create an isolated sub-agent session for parallel work. Use subagents to monitor",
		InputSchema: sessionsSpawnToolSchema(),
		Fn:          toolSessionsSpawn(deps),
	})
	registry.RegisterTool(ToolDef{
		Name:        "subagents",
		Description: "Monitor and control sub-agents: list status, steer with messages, or kill. Defaults to list",
		InputSchema: subagentsToolSchema(),
		Fn:          toolSubagents(deps),
	})

	// -- Media tools --
	registry.RegisterTool(ToolDef{
		Name:        "image",
		Description: "Analyze images with a vision model (up to 20 local files or URLs). Accepts optional prompt",
		InputSchema: imageToolSchema(),
		Fn:          toolImage(deps.LLMClient),
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

	// -- Data tools --
	registry.RegisterTool(ToolDef{
		Name:        "kv",
		Description: "Persistent key-value store (survives restarts). Actions: get, set, delete, list. Dot-separated keys for namespaces",
		InputSchema: kvToolSchema(),
		Fn:          toolKV(),
	})
	registry.RegisterTool(ToolDef{
		Name:        "gmail",
		Description: "Gmail (native OAuth2): inbox summary, search, read, send, reply, labels with contact aliases. Auth: ~/.deneb/credentials/gmail_client.json + gmail_token.json",
		InputSchema: gmailToolSchema(),
		Fn:          toolGmail(),
	})

	// -- Hidden tools (pilot-only) --
	registry.RegisterTool(ToolDef{
		Name:        "agent_logs",
		Description: "현재 세션의 이전 에이전트 런 상세 로그를 조회합니다. 문제 진단, 이전 실행 결과 확인, 도구 실행 시간 분석에 사용합니다",
		InputSchema: agentLogsToolSchema(),
		Fn:          toolAgentLogs(deps.AgentLog),
		Hidden:      true,
	})
	registry.RegisterTool(ToolDef{
		Name:        "gateway_logs",
		Description: "게이트웨이 프로세스 로그를 조회합니다. 레벨/패키지/패턴 필터링 지원. 서버 오류 진단, 요청 추적, 성능 분석에 사용합니다",
		InputSchema: gatewayLogsToolSchema(),
		Fn:          toolGatewayLogs(),
		Hidden:      true,
	})

	// -- Pilot (registered last: uses the registry itself) --
	registry.RegisterTool(ToolDef{
		Name:        "pilot",
		Description: "Fast local AI — gathers tool outputs and analyzes them in one call (free, no API cost). Best for: summarizing file/command output, reviewing diffs, analyzing test failures, comparing multiple sources, processing grep results. Shortcuts: file, files, exec, grep, find, url, http, diff, test, tree, git_log, health, kv_key, memory, gmail, youtube, polaris, image, vega, agent_logs, gateway_logs. Options: chain, max_length, output_format, post_process",
		InputSchema: pilotToolSchema(),
		Fn:          toolPilot(registry, workspaceDir),
	})

	// -- Post-processing pipeline --
	RegisterDefaultPostProcessors(registry)
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
