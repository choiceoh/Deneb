package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// maxOutputRunes caps tool output sent to the LLM to avoid wasting tokens.
// Head and tail are preserved for context; the middle is elided.
const maxOutputRunes = 30000

// TruncateForLLM keeps the first half and last half of output when it exceeds
// maxOutputRunes, inserting a clear elision marker in the middle.
func TruncateForLLM(s string) string {
	runes := []rune(s)
	if len(runes) <= maxOutputRunes {
		return s
	}
	half := maxOutputRunes / 2
	head := string(runes[:half])
	tail := string(runes[len(runes)-half:])
	omitted := len(runes) - maxOutputRunes
	return fmt.Sprintf("%s\n\n... [%d chars omitted] ...\n\n%s", head, omitted, tail)
}

type workdirCacheEntry struct {
	exists  bool
	checked time.Time
}

const workdirCacheTTL = 10 * time.Second

// workdirCache avoids redundant os.Stat calls for the same directory across
// sequential exec invocations. Safe for concurrent use.
var workdirCache sync.Map // map[string]workdirCacheEntry

func validateWorkdir(dir string) error {
	if cached, ok := workdirCache.Load(dir); ok {
		entry := cached.(workdirCacheEntry)
		if time.Since(entry.checked) <= workdirCacheTTL {
			if entry.exists {
				return nil
			}
			return fmt.Errorf("working directory does not exist: %s", dir)
		}
		workdirCache.Delete(dir)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		workdirCache.Store(dir, workdirCacheEntry{exists: false, checked: time.Now()})
		return fmt.Errorf("working directory does not exist: %s", dir)
	}
	workdirCache.Store(dir, workdirCacheEntry{exists: true, checked: time.Now()})
	return nil
}

// ToolExec returns a tool that runs shell commands via procMgr with defaultDir as the
// working directory when no explicit workdir is provided.
func ToolExec(procMgr *process.Manager, defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Command    string            `json:"command"`
			Workdir    string            `json:"workdir"`
			Timeout    float64           `json:"timeout"`
			Background bool              `json:"background"`
			Structured bool              `json:"structured"`
			Env        map[string]string `json:"env"`
		}
		if err := jsonutil.UnmarshalInto("exec params", input, &p); err != nil {
			return "", err
		}
		if p.Command == "" {
			return "", fmt.Errorf("command is required")
		}

		// Safety check: warn about destructive commands.
		// The warning is prepended to the output so the LLM sees it.
		var destructiveWarning string
		if checks := CheckDestructiveCommand(p.Command); len(checks) > 0 {
			destructiveWarning = FormatDestructiveWarnings(checks)
		}

		workDir := p.Workdir
		if workDir == "" {
			workDir = defaultDir
		}

		if err := validateWorkdir(workDir); err != nil {
			return "", err
		}

		timeoutMs := int64(30000)
		if p.Timeout > 0 {
			timeoutMs = int64(p.Timeout * 1000)
		}
		const maxTimeoutMs = 10 * 60 * 1000
		if timeoutMs > maxTimeoutMs {
			timeoutMs = maxTimeoutMs
		}

		if procMgr != nil {
			req := process.ExecRequest{
				Command:    "bash",
				Args:       []string{"-c", p.Command},
				WorkingDir: workDir,
				TimeoutMs:  timeoutMs,
				Env:        p.Env,
			}

			// Background mode: launch asynchronously and return the process ID
			// so the caller can poll via the process tool.
			if p.Background {
				id := procMgr.ExecuteBackground(ctx, req)
				return fmt.Sprintf(`{"id":"%s","status":"running","message":"background process started, use process tool with action=poll to check"}`, id), nil
			}

			result := procMgr.Execute(ctx, req)
			if p.Structured {
				return TruncateForLLM(formatExecResultJSON(result)), nil
			}
			out := formatExecResult(result)
			// Annotate non-error exit codes with command-specific context.
			// e.g. grep exit 1 = "no matches found", not an error.
			if result.ExitCode != 0 {
				if isErr, hint := InterpretExitCode(p.Command, result.ExitCode); !isErr && hint != "" {
					out += " " + hint
				}
			}
			if destructiveWarning != "" {
				out = destructiveWarning + "\n" + out
			}
			return TruncateForLLM(out), nil
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
			return TruncateForLLM(string(data)), nil
		}

		if err != nil {
			return TruncateForLLM(fmt.Sprintf("%s\n\nError: %s", string(out), err.Error())), nil
		}
		return TruncateForLLM(string(out)), nil
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

func ToolProcess(procMgr *process.Manager) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action    string `json:"action"`
			SessionID string `json:"sessionId"`
			Input     string `json:"input"`
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
		case "write":
			if p.SessionID == "" {
				return "", fmt.Errorf("sessionId is required for write")
			}
			if p.Input == "" {
				return "", fmt.Errorf("input is required for write")
			}
			if err := procMgr.WriteStdin(p.SessionID, p.Input); err != nil {
				return "", err
			}
			return fmt.Sprintf("Wrote %d bytes to process %q stdin.", len(p.Input), p.SessionID), nil
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
