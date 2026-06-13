// codeaction.go — the code_action tool (CodeAct paradigm).
//
// Instead of one JSON tool-call per turn, the model writes a short Python
// program that orchestrates several read-only Deneb tools and processes the
// data locally, returning only what it prints. This collapses multi-tool,
// batch, and cross-source-join work (scan many emails, join calendar × deals ×
// contacts, count/filter/aggregate) into a single turn — fewer turns, less
// re-prefill (the dominant local-model latency cost).
//
// Security model (see also codeaction_runtime.py):
//   - The model's Python runs in a throwaway subprocess with a MINIMAL env, so
//     the gateway's secret env vars are never inherited.
//   - A PEP 578 audit hook blocks network (except this bridge), subprocess,
//     fs writes outside the scratch dir, secret-path reads, and ctypes.
//   - Deneb data access goes through an ephemeral loopback HTTP bridge guarded
//     by a one-time token; the bridge enforces a READ-ONLY tool/action
//     allowlist server-side, so token possession grants nothing more.
//   - Wall-clock timeout + output cap bound the run.
package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

//go:embed codeaction_runtime.py
var codeActionRuntime string

// ToolInvoker is the read-only tool surface the code_action bridge dials back
// into. *chat.ToolRegistry already satisfies it (see chat/tools.go), so no new
// wiring is needed — the registry passes itself in toolreg_core.go.
type ToolInvoker interface {
	Execute(ctx context.Context, name string, input json.RawMessage) (string, error)
}

// CodeActionDescription is the deferred-listing description. The first sentence
// is the WHEN trigger (the deferred summary truncates to it).
const CodeActionDescription = "Run Python in a single turn to orchestrate several read-only tools or batch/aggregate data — scan many emails, join calendar×contacts×wiki, count/filter/compute — instead of many separate tool calls. A preloaded `deneb` object exposes read-only gmail/calendar/contacts/wiki; only what you print() (and any traceback) is returned. The interpreter is sandboxed: no network except the tool bridge, no subprocess, no file writes outside a scratch dir, no secret reads."

// codeActionReadOnly is the action-granular allowlist. Only these (tool,
// action) pairs may be dialed from model-written code. Anything that mutates
// state or sends outbound (gmail send/reply/label/attachment, calendar
// create/update/delete, wiki write/log) is intentionally absent. Arbitrary
// file read (the `read` tool) is also intentionally excluded in v1.
var codeActionReadOnly = map[string]map[string]bool{
	"gmail":    {"inbox": true, "search": true, "read": true, "thread": true, "analyze": true},
	"calendar": {"list": true, "get": true, "free_slots": true},
	"contacts": {"lookup": true, "search": true},
	"wiki":     {"search": true, "read": true, "index": true, "daily": true, "status": true},
}

// codeActionAllow returns nil if (tool, args.action) is a permitted read-only
// call, or a descriptive error the model can learn from.
func codeActionAllow(tool string, args map[string]any) error {
	actions, ok := codeActionReadOnly[tool]
	if !ok {
		return fmt.Errorf("tool %q is not available from code_action (read-only only: gmail, calendar, contacts, wiki)", tool)
	}
	action, _ := args["action"].(string)
	action = strings.TrimSpace(action)
	if action == "" {
		return fmt.Errorf("%s via code_action requires an 'action' (allowed: %s)", tool, strings.Join(sortedActionKeys(actions), ", "))
	}
	if !actions[action] {
		return fmt.Errorf("%s action %q is not allowed from code_action (read-only actions: %s)", tool, action, strings.Join(sortedActionKeys(actions), ", "))
	}
	return nil
}

func sortedActionKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// codeActionBridge is the ephemeral loopback HTTP handler that model code dials.
// It authenticates a one-time token, enforces the read-only allowlist, and
// forwards the call to the chat tool registry on the captured turn context (so
// preset / TurnContext / run-cache propagate exactly as a top-level call would).
type codeActionBridge struct {
	invoker ToolInvoker
	token   string
	ctx     context.Context
}

func (b *codeActionBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Deneb-Bridge-Token")), []byte(b.token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var req struct {
		Tool string         `json:"tool"`
		Args map[string]any `json:"args"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeBridgeJSON(w, map[string]any{"ok": false, "error": "bad request: " + err.Error()})
		return
	}
	if req.Args == nil {
		req.Args = map[string]any{}
	}
	if err := codeActionAllow(req.Tool, req.Args); err != nil {
		writeBridgeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	argsJSON, err := json.Marshal(req.Args)
	if err != nil {
		writeBridgeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	result, err := b.invoker.Execute(b.ctx, req.Tool, argsJSON)
	if err != nil {
		writeBridgeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeBridgeJSON(w, map[string]any{"ok": true, "result": result})
}

func writeBridgeJSON(w http.ResponseWriter, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// ToolCodeAction returns the code_action tool. invoker is the read-only tool
// surface (the chat registry); baseDir is unused for the scratch dir (a fresh
// system temp dir is used) but kept for symmetry with other tool constructors.
func ToolCodeAction(invoker ToolInvoker) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if invoker == nil {
			return "", fmt.Errorf("code_action is unavailable")
		}
		var p struct {
			Code    string  `json:"code"`
			Timeout float64 `json:"timeout"`
		}
		if err := jsonutil.UnmarshalInto("code_action params", input, &p); err != nil {
			return "", err
		}
		if strings.TrimSpace(p.Code) == "" {
			return "", fmt.Errorf("code is required")
		}

		pyPath, err := exec.LookPath("python3")
		if err != nil {
			return "", fmt.Errorf("code_action requires python3, which was not found on PATH: %w", err)
		}

		// Throwaway scratch sandbox (cwd for the run; the only writable tree).
		sandbox, err := os.MkdirTemp("", "deneb-codeaction-")
		if err != nil {
			return "", fmt.Errorf("code_action scratch dir: %w", err)
		}
		defer os.RemoveAll(sandbox)
		if err := os.WriteFile(filepath.Join(sandbox, "_runtime.py"), []byte(codeActionRuntime), 0o600); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(sandbox, "_main.py"), []byte(p.Code), 0o600); err != nil {
			return "", err
		}

		// Ephemeral loopback bridge, one-time token.
		var lc net.ListenConfig
		lis, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
		if err != nil {
			return "", fmt.Errorf("code_action bridge: %w", err)
		}
		token := newBridgeToken()
		srv := &http.Server{
			Handler:           &codeActionBridge{invoker: invoker, token: token, ctx: ctx},
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() { _ = srv.Serve(lis) }()
		defer func() { _ = srv.Close() }()
		tcpAddr, ok := lis.Addr().(*net.TCPAddr)
		if !ok {
			return "", fmt.Errorf("code_action bridge: unexpected listener address type %T", lis.Addr())
		}
		port := tcpAddr.Port

		// Wall-clock budget (default 60s, hard cap 120s).
		timeoutMs := int64(60000)
		if p.Timeout > 0 {
			timeoutMs = int64(p.Timeout * 1000)
		}
		const maxTimeoutMs = 120000
		if timeoutMs > maxTimeoutMs {
			timeoutMs = maxTimeoutMs
		}
		cpuSeconds := timeoutMs/1000 + 5

		execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
		defer cancel()

		// -I isolated mode (ignore PYTHON* env + user site), -B no bytecode writes.
		cmd := exec.CommandContext(execCtx, pyPath, "-I", "-B", filepath.Join(sandbox, "_runtime.py"))
		cmd.Dir = sandbox
		// Minimal env: the gateway's secret env vars are NOT inherited.
		cmd.Env = []string{
			"PATH=" + os.Getenv("PATH"),
			"HOME=" + os.Getenv("HOME"),
			"LANG=C.UTF-8",
			"DENEB_SANDBOX_DIR=" + sandbox,
			"DENEB_BRIDGE_PORT=" + strconv.Itoa(port),
			"DENEB_BRIDGE_TOKEN=" + token,
			"DENEB_CPU_SECONDS=" + strconv.FormatInt(cpuSeconds, 10),
		}
		out := &cappedBuffer{limit: 24000}
		errb := &cappedBuffer{limit: 8000}
		cmd.Stdout = out
		cmd.Stderr = errb

		runErr := cmd.Run()
		return formatCodeActionResult(out.String(), errb.String(), runErr, execCtx.Err()), nil
	}
}

func newBridgeToken() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// cappedBuffer collects up to limit bytes, then drops the rest (so a runaway
// print loop cannot OOM the gateway) while still consuming the stream.
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.limit - c.buf.Len(); room > 0 {
		if len(p) <= room {
			return c.buf.Write(p)
		}
		c.buf.Write(p[:room])
	}
	c.truncated = true
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	s := c.buf.String()
	if c.truncated {
		s += "\n…[output truncated]"
	}
	return s
}

func formatCodeActionResult(stdout, stderr string, runErr, ctxErr error) string {
	var sb strings.Builder
	stdout = strings.TrimRight(stdout, "\n")
	if stdout != "" {
		sb.WriteString(stdout)
	}
	if ctxErr != nil { // deadline exceeded
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("[code_action: timed out]")
		return sb.String()
	}
	if stderr = strings.TrimRight(stderr, "\n"); stderr != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("STDERR:\n")
		sb.WriteString(stderr)
	} else if runErr != nil {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "[code_action: %v]", runErr)
	}
	if sb.Len() == 0 {
		return "(code_action produced no output — use print() to return data)"
	}
	return sb.String()
}

// CodeActionSchema is the input schema (defined in Go, like fetch_tools, since
// code_action is registered in toolreg_core.go and not via tool_schemas.json).
func CodeActionSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"code": map[string]any{
				"type":        "string",
				"description": "Python 3 source. A preloaded `deneb` object exposes read-only tools that each return the tool's text result: deneb.gmail(action, query=…, message_id=…, max=…) [inbox|search|read|thread|analyze], deneb.calendar(action, **kw) [list|get|free_slots], deneb.contacts(action, query) [lookup|search], deneb.wiki(action, query=…, **kw) [search|read|index|daily|status]. Use print() to return data to yourself — only stdout and any traceback come back. No network (except the bridge), no subprocess, no writes outside the scratch dir.",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Wall-clock seconds (default 60, max 120).",
			},
		},
		"required": []any{"code"},
	}
}
