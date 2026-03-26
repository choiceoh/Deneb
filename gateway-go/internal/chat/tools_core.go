package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/process"
)

// RegisterCoreTools populates the tool registry with all core agent tools.
// Tools that require external subsystems (e.g., process manager) are wired here.
// Tools not yet implemented return a descriptive "not available" message.
func RegisterCoreTools(registry *ToolRegistry, procMgr *process.Manager, workspaceDir string) {
	// -- File system tools (implemented in tools_fs.go) --
	registry.RegisterTool(ToolDef{
		Name:        "read",
		Description: "Read file contents",
		InputSchema: readToolSchema(),
		Fn:          toolRead(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "write",
		Description: "Create or overwrite files",
		InputSchema: writeToolSchema(),
		Fn:          toolWrite(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "edit",
		Description: "Make precise edits to files",
		InputSchema: editToolSchema(),
		Fn:          toolEdit(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "grep",
		Description: "Search file contents for patterns",
		InputSchema: grepToolSchema(),
		Fn:          toolGrep(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "find",
		Description: "Find files by glob pattern",
		InputSchema: findToolSchema(),
		Fn:          toolFind(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "ls",
		Description: "List directory contents",
		InputSchema: lsToolSchema(),
		Fn:          toolLs(workspaceDir),
	})

	// -- Exec/process tools --
	registry.RegisterTool(ToolDef{
		Name:        "exec",
		Description: "Run shell commands",
		InputSchema: execToolSchema(),
		Fn:          toolExec(procMgr, workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "process",
		Description: "Manage background exec sessions",
		InputSchema: processToolSchema(),
		Fn:          toolProcess(procMgr),
	})

	// -- Web tools (stub) --
	registry.RegisterTool(ToolDef{
		Name:        "web_fetch",
		Description: "Fetch and extract readable content from a URL",
		InputSchema: webFetchToolSchema(),
		Fn:          toolWebFetch(),
	})

	// -- Memory tools --
	registry.RegisterTool(ToolDef{
		Name:        "memory_search",
		Description: "Search memory files (MEMORY.md, memory/*.md) by keyword",
		InputSchema: memorySearchToolSchema(),
		Fn:          toolMemorySearch(workspaceDir),
	})
	registry.RegisterTool(ToolDef{
		Name:        "memory_get",
		Description: "Read specific lines from a memory file",
		InputSchema: memoryGetToolSchema(),
		Fn:          toolMemoryGet(workspaceDir),
	})

	// -- Message tool (proactive channel sends via context-injected ReplyFunc) --
	registry.RegisterTool(ToolDef{
		Name:        "message",
		Description: "Send messages and channel actions (send, reply, react)",
		InputSchema: messageToolSchema(),
		Fn:          toolMessage(),
	})

	// -- Apply patch tool --
	registry.RegisterTool(ToolDef{
		Name:        "apply_patch",
		Description: "Apply multi-file patches (unified diff format)",
		InputSchema: applyPatchToolSchema(),
		Fn:          toolApplyPatch(workspaceDir),
	})

	// -- Stubbed tools (return descriptive unavailable message) --
	stubs := []struct {
		name string
		desc string
	}{
		{"web_search", "Search the web"},
		// memory tools are registered separately with full implementation.
		// {"memory_search", "Semantic memory search"},
		// {"memory_get", "Read memory files"},
		// message tool is registered separately with full implementation.
		// {"message", "Send messages and channel actions"},
		{"cron", "Manage cron jobs and wake events"},
		{"gateway", "Gateway control (restart, config, update)"},
		{"sessions_list", "List other sessions"},
		{"sessions_history", "Fetch history for another session"},
		{"sessions_send", "Send a message to another session"},
		{"sessions_spawn", "Spawn an isolated sub-agent session"},
		{"subagents", "List, steer, or kill sub-agent runs"},
		{"session_status", "Show session status and usage"},
		{"image", "Analyze images with a vision model"},
		{"nodes", "Discover and control paired nodes"},
	}
	for _, s := range stubs {
		name := s.name
		registry.RegisterTool(ToolDef{
			Name:        name,
			Description: s.desc,
			InputSchema: map[string]any{"type": "object"},
			Fn: func(_ context.Context, _ json.RawMessage) (string, error) {
				return fmt.Sprintf("Tool %q is not yet available in the Go gateway.", name), nil
			},
		})
	}
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
				"description": "Timeout in seconds",
			},
			"background": map[string]any{
				"type":        "boolean",
				"description": "Run in background immediately",
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
				"description": "Process action: list, poll, log, write, kill",
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

// --- Web fetch tool (basic implementation) ---

func webFetchToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "HTTP or HTTPS URL to fetch",
			},
			"maxChars": map[string]any{
				"type":        "number",
				"description": "Maximum characters to return",
			},
		},
		"required": []string{"url"},
	}
}

func toolWebFetch() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			URL      string `json:"url"`
			MaxChars int    `json:"maxChars"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid web_fetch params: %w", err)
		}
		if p.URL == "" {
			return "", fmt.Errorf("url is required")
		}

		// Validate URL scheme.
		if !strings.HasPrefix(p.URL, "http://") && !strings.HasPrefix(p.URL, "https://") {
			return "", fmt.Errorf("only http:// and https:// URLs are supported")
		}

		// Default max chars.
		maxChars := 50000
		if p.MaxChars > 0 {
			maxChars = p.MaxChars
		}

		// Fetch with timeout.
		fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, p.URL, nil)
		if err != nil {
			return "", fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("User-Agent", "Deneb-Gateway/1.0")
		req.Header.Set("Accept", "text/html,text/plain,application/json,*/*")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("fetch failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			return fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Status), nil
		}

		// Read body with size limit (2x maxChars as raw bytes may be larger).
		limitBytes := int64(maxChars * 2)
		if limitBytes > 5*1024*1024 {
			limitBytes = 5 * 1024 * 1024 // Cap at 5 MB
		}
		bodyReader := io.LimitReader(resp.Body, limitBytes)
		body, err := io.ReadAll(bodyReader)
		if err != nil {
			return "", fmt.Errorf("read body: %w", err)
		}

		content := string(body)

		// Basic HTML tag stripping for readability.
		contentType := resp.Header.Get("Content-Type")
		if strings.Contains(contentType, "text/html") {
			content = stripHTMLTags(content)
		}

		// Truncate to maxChars.
		if len(content) > maxChars {
			content = content[:maxChars] + "\n\n[...truncated at " + fmt.Sprintf("%d", maxChars) + " chars]"
		}

		return content, nil
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

// stripHTMLTags does a basic removal of HTML tags for text extraction.
func stripHTMLTags(html string) string {
	var sb strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			sb.WriteRune(r)
		}
	}
	// Collapse excessive whitespace.
	result := sb.String()
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(result)
}
