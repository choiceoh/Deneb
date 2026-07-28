package runtimeops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/process"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// Note: exec returns full output. Head/tail truncation and disk spillover are
// handled once by the tool registry (ToolRegistry.Execute) so the spillover
// store receives the *original* output — truncating here would offload only the
// already-cut text and orphan the middle. See chat/tools.go.

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
		entry := cached.(workdirCacheEntry) //nolint:errcheck // type guaranteed by sync.Map usage
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

// execParams is the exec tool's input payload.
type execParams struct {
	Command    string            `json:"command"`
	Workdir    string            `json:"workdir"`
	Timeout    float64           `json:"timeout"`
	Background bool              `json:"background"`
	Structured bool              `json:"structured"`
	Env        map[string]string `json:"env"`
}

// ToolExec returns a tool that runs shell commands via procMgr with defaultDir as the
// working directory when no explicit workdir is provided.
func ToolExec(procMgr *process.Manager, defaultDir string) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p execParams
		if err := jsonutil.UnmarshalInto("exec params", input, &p); err != nil {
			return "", err
		}
		if p.Command == "" {
			return "", fmt.Errorf("command is required")
		}

		// Hard block: catastrophic, unrecoverable commands (root/home/system wipe,
		// raw block-device overwrite, disk format, fork bomb) are REFUSED outright
		// rather than run with a warning. Unlike the warn list below — which
		// includes legitimate-in-context operations like `rm -rf ./build` or
		// `git reset --hard` — these have no plausible automated use; the operator
		// can still run them directly on the host. Checked before workdir/exec so it
		// covers both the process-manager and fallback execution paths.
		if blocked := CheckCatastrophicCommand(p.Command); len(blocked) > 0 {
			return FormatCatastrophicRefusal(blocked), nil
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

		snapshotInPlaceTargets(ctx, p.Command, workDir)
		timeoutMs := execTimeoutMs(p.Timeout)

		if procMgr != nil {
			return execViaManager(ctx, procMgr, p, workDir, timeoutMs, destructiveWarning)
		}
		return execFallback(ctx, p, workDir, timeoutMs, destructiveWarning)
	}
}

// snapshotInPlaceTargets takes a pre-exec checkpoint: an in-place file edit run
// via exec (sed -i, '>' redirect) bypasses the rollback net the fs Write/Edit
// tools have. Snapshot the target files that already exist so this edit is
// /rollback-recoverable too. Best-effort + nil-safe (no Checkpointer wired →
// no-op); only existing regular files are snapshotted, so over-inclusive
// candidates (a misparsed sed script) harmlessly drop out, and the command
// itself is never blocked or modified.
func snapshotInPlaceTargets(ctx context.Context, command, workDir string) {
	for _, t := range InPlaceFileTargets(command) {
		abs := t
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(workDir, t)
		}
		if fi, err := os.Stat(abs); err == nil && fi.Mode().IsRegular() {
			toolport.SnapshotBeforeWrite(ctx, abs, "exec")
		}
	}
}

// execTimeoutMs converts the requested timeout (seconds) into milliseconds,
// defaulting to 60s and capping at 10 minutes.
func execTimeoutMs(timeoutSec float64) int64 {
	timeoutMs := int64(60000)
	if timeoutSec > 0 {
		timeoutMs = int64(timeoutSec * 1000)
	}
	const maxTimeoutMs = 10 * 60 * 1000
	if timeoutMs > maxTimeoutMs {
		timeoutMs = maxTimeoutMs
	}
	return timeoutMs
}

// execViaManager runs the command through the process manager (foreground or
// background) and formats the result.
func execViaManager(ctx context.Context, procMgr *process.Manager, p execParams, workDir string, timeoutMs int64, destructiveWarning string) (string, error) {
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
		return fmt.Sprintf(`{"id":%q,"status":"running","message":"background process started, use process tool with action=poll to check"}`, id), nil
	}

	result := procMgr.Execute(ctx, req)
	if p.Structured {
		return formatExecResultJSON(result), nil
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
	return out, nil
}

// execFallback is direct exec without a process manager. Same env hygiene as
// the procMgr path — the inherited gateway environment carries secrets, so it
// must never reach the child unsanitized; p.Env is applied on top (it was
// silently dropped here before).
func execFallback(ctx context.Context, p execParams, workDir string, timeoutMs int64, destructiveWarning string) (string, error) {
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	start := time.Now()
	cmd := exec.CommandContext(execCtx, "bash", "-c", p.Command) //nolint:gosec // G204 — command execution is by design
	cmd.Dir = workDir
	env := process.SanitizeEnv(os.Environ(), slog.Default())
	for k, v := range p.Env {
		if process.IsSecretEnvKey(k) {
			continue
		}
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)

	if p.Structured {
		exitCode := 0
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
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

	// Exit-code hints + destructive warning previously applied only on the
	// procMgr path; the fallback dropped both.
	outStr := string(out)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if isErr, hint := InterpretExitCode(p.Command, exitErr.ExitCode()); !isErr && hint != "" {
				outStr += " " + hint
			}
		}
		outStr = fmt.Sprintf("%s\n\nError: %s", outStr, err.Error())
	}
	if destructiveWarning != "" {
		outStr = destructiveWarning + "\n" + outStr
	}
	return outStr, nil
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
	if r.StdoutDroppedBytes > 0 {
		result["stdout_dropped_bytes"] = r.StdoutDroppedBytes
	}
	if r.StderrDroppedBytes > 0 {
		result["stderr_dropped_bytes"] = r.StderrDroppedBytes
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data)
}

func formatExecResult(r *process.ExecResult) string {
	var sb strings.Builder
	// Head-drop note FIRST: without it a ring-buffered tail reads as complete
	// output starting mid-line, and the model reasons over missing data
	// (measured: seq 1..300000 → "first line" was 150204).
	if r.StdoutDroppedBytes > 0 {
		fmt.Fprintf(&sb,
			"[stdout truncated: first %d bytes dropped — output exceeded the capture buffer; only the tail follows]\n",
			r.StdoutDroppedBytes)
	}
	if r.Stdout != "" {
		sb.WriteString(r.Stdout)
	}
	if r.Stderr != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		if r.StderrDroppedBytes > 0 {
			fmt.Fprintf(&sb,
				"[stderr truncated: first %d bytes dropped — only the tail follows]\n",
				r.StderrDroppedBytes)
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

// ToolProcess returns the process inspection and control tool.
func ToolProcess(procMgr *process.Manager) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action    string `json:"action"`
			SessionID string `json:"sessionId"`
			Input     string `json:"input"`
			Timeout   int64  `json:"timeout"` // poll: block up to this many ms for completion
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
			// The schema has always advertised a poll timeout, but it was never
			// read — poll returned an instant snapshot, leaving the model no way
			// to wait for a background job. Block (bounded, ctx-aware) until the
			// process leaves running/pending or the timeout elapses.
			if p.Action == "poll" && p.Timeout > 0 {
				const maxPollMs = 5 * 60 * 1000
				waitMs := min(p.Timeout, maxPollMs)
				deadline := time.Now().Add(time.Duration(waitMs) * time.Millisecond)
				for isProcessInFlight(t.Status) && time.Now().Before(deadline) {
					select {
					case <-ctx.Done():
						return "", ctx.Err()
					case <-time.After(200 * time.Millisecond):
					}
					if t = procMgr.Get(p.SessionID); t == nil {
						return fmt.Sprintf("Process %q not found.", p.SessionID), nil
					}
				}
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
			procMgr.Kill(p.SessionID) //nolint:errcheck // best-effort
			return fmt.Sprintf("Process %q killed.", p.SessionID), nil
		default:
			return fmt.Sprintf("Unknown process action: %q", p.Action), nil
		}
	}
}

// isProcessInFlight reports whether a tracked process may still change state
// (poll-with-timeout keeps waiting only in these states).
func isProcessInFlight(s process.RunStatus) bool {
	return s == process.StatusRunning || s == process.StatusPending || s == process.StatusApproved
}
