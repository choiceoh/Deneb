package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/media"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
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

	// AutonomousSvc is set after Handler creation to avoid init-order deps.
	// The autonomous tool gracefully degrades when this is nil.
	AutonomousSvc *autonomous.Service

	// VegaBackend is the optional Vega search backend.
	// The vega tool gracefully degrades when this is nil.
	VegaBackend vega.Backend
}

// RegisterCoreTools populates the tool registry with all core agent tools.
// Tools that require external subsystems (e.g., process manager) are wired here.
func RegisterCoreTools(registry *ToolRegistry, deps *CoreToolDeps) {
	procMgr := deps.ProcessMgr
	workspaceDir := deps.WorkspaceDir
	cronSched := deps.CronSched
	// -- File system tools (implemented in tools_fs.go) --
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
		Name:        "ls",
		Description: "List directory contents with sizes. Use find for recursive search",
		InputSchema: lsToolSchema(),
		Fn:          toolLs(workspaceDir),
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

	// -- Web tool (unified search + fetch) --
	webCache := NewFetchCache()
	sglang := newSGLangExtractor()
	registry.RegisterTool(ToolDef{
		Name:        "web",
		Description: "Search the web, fetch URLs, or search+auto-fetch in one call. Modes: {url:...} fetch, {query:...} search, {query:...,fetch:N} search+fetch",
		InputSchema: webToolSchema(),
		Fn:          toolWeb(webCache, sglang),
	})

	// -- Memory tools --
	registry.RegisterTool(ToolDef{
		Name:        "memory_search",
		Description: "Search MEMORY.md + memory/*.md by keyword. Returns matched lines with context",
		InputSchema: memorySearchToolSchema(),
		Fn:          toolMemorySearch(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "memory_get",
		Description: "Read specific line range from a memory file. Use after memory_search to get full context",
		InputSchema: memoryGetToolSchema(),
		Fn:          toolMemoryGet(workspaceDir),
	})

	// -- Vega tool (project knowledge search) --
	registry.RegisterTool(ToolDef{
		Name:        "vega",
		Description: "Search project knowledge base (Vega). Hybrid BM25 + semantic search across all projects. Actions: search (default), ask",
		InputSchema: vegaToolSchema(),
		Fn:          toolVega(deps),
	})

	// -- System manual tool (queryable Deneb documentation) --
	registry.RegisterTool(ToolDef{
		Name:        "polaris",
		Description: "Query Deneb system manual. actions: topics (doc tree), search (keyword search), read (read a doc), guides (28 AI-curated system guides: aurora, vega, agent-loop, compaction, tools, system-prompt, memory, sessions, architecture, channels, telegram, skills, pilot, cron, autonomous, web, exec, gateway-tool, media, gmail, data-tools, sessions-tools, message, provider, liteparse, metrics, nodes, transcript)",
		InputSchema: systemManualToolSchema(),
		Fn:          toolSystemManual(workspaceDir),
	})

	// -- Message tool (proactive channel sends via context-injected ReplyFunc) --
	registry.RegisterTool(ToolDef{
		Name:        "message",
		Description: "Send messages to the user's channel. Actions: send, reply, react, thread-reply. Use for proactive sends",
		InputSchema: messageToolSchema(),
		Fn:          toolMessage(),
	})

	// -- Apply patch tool --
	registry.RegisterTool(ToolDef{
		Name:        "apply_patch",
		Description: "Apply multi-file unified diff patches. Tries git apply first, falls back to patch -p1",
		InputSchema: applyPatchToolSchema(),
		Fn:          toolApplyPatch(workspaceDir),
	})

	// -- Coding tools (implemented in tools_coding.go) --
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

	// -- Cron tool --
	registry.RegisterTool(ToolDef{
		Name:        "cron",
		Description: "Schedule recurring jobs (cron expressions). Actions: status, list, add, update, remove, run, wake",
		InputSchema: cronToolSchema(),
		Fn:          toolCron(cronSched, deps),
	})

	// -- Gateway tool --
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
		Name:        "sessions_restore",
		Description: "Restore a past session's conversation into the current session for continuation",
		InputSchema: sessionsRestoreToolSchema(),
		Fn:          toolSessionsRestore(deps.Transcript),
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
	registry.RegisterTool(ToolDef{
		Name:        "session_status",
		Description: "Show current session info: kind, status, model, token usage, runtime",
		InputSchema: sessionStatusToolSchema(),
		Fn:          toolSessionStatus(deps.Sessions),
	})

	// -- Image tool --
	registry.RegisterTool(ToolDef{
		Name:        "image",
		Description: "Analyze images with a vision model (up to 20 local files or URLs). Accepts optional prompt",
		InputSchema: imageToolSchema(),
		Fn:          toolImage(deps.LLMClient),
	})

	// -- YouTube transcript tool --
	registry.RegisterTool(ToolDef{
		Name:        "youtube_transcript",
		Description: "Extract transcript/subtitles and metadata from a YouTube video",
		InputSchema: youtubeTranscriptToolSchema(),
		Fn:          toolYouTubeTranscript(),
	})

	// -- Nodes tool --
	registry.RegisterTool(ToolDef{
		Name:        "nodes",
		Description: "Discover and control paired mobile nodes (status/notify/camera/run)",
		InputSchema: nodesToolSchema(),
		Fn:          toolNodes(),
	})

	// -- Send file tool (media delivery to channel) --
	registry.RegisterTool(ToolDef{
		Name:        "send_file",
		Description: "Send a file to the user (auto-detects: photo/video/audio/document). Max 50 MB",
		InputSchema: sendFileToolSchema(),
		Fn:          toolSendFile(),
	})

	// -- HTTP tool (structured API requests) --
	registry.RegisterTool(ToolDef{
		Name:        "http",
		Description: "Make HTTP API requests with headers, JSON body, and auth. Returns status + headers + body",
		InputSchema: httpToolSchema(),
		Fn:          toolHTTP(),
	})

	// -- KV tool (lightweight key-value persistence) --
	registry.RegisterTool(ToolDef{
		Name:        "kv",
		Description: "Persistent key-value store (survives restarts). Actions: get, set, delete, list. Dot-separated keys for namespaces",
		InputSchema: kvToolSchema(),
		Fn:          toolKV(),
	})

	// -- Gmail tool (structured Gmail operations via native API) --
	registry.RegisterTool(ToolDef{
		Name:        "gmail",
		Description: "Gmail (native OAuth2): inbox summary, search, read, send, reply, labels with contact aliases. Auth: ~/.deneb/credentials/gmail_client.json + gmail_token.json",
		InputSchema: gmailToolSchema(),
		Fn:          toolGmail(),
	})

	// -- Clipboard tool (temporary content sharing) --
	registry.RegisterTool(ToolDef{
		Name:        "clipboard",
		Description: "Temporary in-memory clipboard (ring buffer, 32 items max). Actions: set, get, list, clear",
		InputSchema: clipboardToolSchema(),
		Fn:          toolClipboard(),
	})

	// -- Autonomous tool (goal-driven execution management) --
	registry.RegisterTool(ToolDef{
		Name:        "autonomous",
		Description: "Manage autonomous goals and execution cycles. Autonomous cycles let Deneb act without waiting for user input — essential for callbacks like sub-agent completion notifications, scheduled checks, and deferred task follow-ups that would otherwise stay unread until the user speaks. Actions: status, goals, add_goal, update_goal, remove_goal, cycle_run, cycle_stop, enable, disable, recent_runs",
		InputSchema: autonomousToolSchema(),
		Fn:          toolAutonomous(deps),
	})

	// -- Pilot tool (fast local AI that orchestrates other tools) --
	// Registered last: uses the registry itself to execute source tools.
	registry.RegisterTool(ToolDef{
		Name:        "pilot",
		Description: "Fast local AI that runs tools + analyzes results in one call. Shortcuts: file, files, exec, grep, find, url, http, kv_key, memory, gmail, youtube, polaris, image, clipboard, ls, vega. Options: chain (follow-up tools), max_length (brief/normal/detailed), output_format (text/json/list), conditional sources (only_if/skip_if), post_process steps. Auto-enables thinking for complex tasks. Falls back to raw results if sglang is down",
		InputSchema: pilotToolSchema(),
		Fn:          toolPilot(registry, workspaceDir),
	})

	// -- Post-processing pipeline --
	RegisterDefaultPostProcessors(registry)
}

// --- Exec tool ---

func execToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Shell command to execute",
			},
			"workdir": map[string]any{
				"type":        "string",
				"description": "Working directory (defaults to workspace root)",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Timeout in seconds (default: 30, max: 300)",
				"default":     30,
				"minimum":     1,
				"maximum":     300,
			},
			"background": map[string]any{
				"type":        "boolean",
				"description": "Run in background immediately, then use process tool to check output",
				"default":     false,
			},
		},
		"required": []string{"command"},
	}
}

func toolExec(procMgr *process.Manager, defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Command    string  `json:"command"`
			Workdir    string  `json:"workdir"`
			Timeout    float64 `json:"timeout"`
			Background bool    `json:"background"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid exec params: %w", err)
		}
		if p.Command == "" {
			return "", fmt.Errorf("command is required")
		}

		workDir := p.Workdir
		if workDir == "" {
			workDir = defaultDir
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
			return formatExecResult(result), nil
		}

		// Fallback: direct exec without process manager.
		execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
		defer cancel()
		cmd := exec.CommandContext(execCtx, "bash", "-c", p.Command)
		cmd.Dir = workDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Sprintf("%s\n\nError: %s", string(out), err.Error()), nil
		}
		return string(out), nil
	}
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

func processToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Process action",
				"enum":        []string{"list", "poll", "log", "write", "kill"},
			},
			"sessionId": map[string]any{
				"type":        "string",
				"description": "Session ID for actions other than list",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Poll timeout in milliseconds",
			},
		},
		"required": []string{"action"},
	}
}

func toolProcess(procMgr *process.Manager) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action    string `json:"action"`
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid process params: %w", err)
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

func youtubeTranscriptToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "YouTube video URL (youtube.com/watch?v=... or youtu.be/...)",
			},
		},
		"required": []string{"url"},
	}
}

func toolYouTubeTranscript() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid youtube_transcript params: %w", err)
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

// --- Apply patch tool ---

func applyPatchToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"patch": map[string]any{
				"type":        "string",
				"description": "The unified diff patch to apply",
			},
		},
		"required": []string{"patch"},
	}
}

func toolApplyPatch(defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Patch string `json:"patch"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid apply_patch params: %w", err)
		}
		if p.Patch == "" {
			return "", fmt.Errorf("patch is required")
		}

		// Use `git apply` for reliable patch application.
		cmd := exec.CommandContext(ctx, "git", "apply", "--allow-empty", "-")
		cmd.Dir = defaultDir
		cmd.Stdin = strings.NewReader(p.Patch)
		out, err := cmd.CombinedOutput()
		if err != nil {
			// Fall back to `patch -p1` if git apply fails.
			cmd2 := exec.CommandContext(ctx, "patch", "-p1", "--no-backup-if-mismatch")
			cmd2.Dir = defaultDir
			cmd2.Stdin = strings.NewReader(p.Patch)
			out2, err2 := cmd2.CombinedOutput()
			if err2 != nil {
				return fmt.Sprintf("git apply failed: %s\npatch -p1 failed: %s", string(out), string(out2)), nil
			}
			return fmt.Sprintf("Patch applied (via patch -p1):\n%s", string(out2)), nil
		}
		result := "Patch applied successfully."
		if len(out) > 0 {
			result += "\n" + string(out)
		}
		return result, nil
	}
}
