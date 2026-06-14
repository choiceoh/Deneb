// codeaction.go — the code_action tool (CodeAct paradigm).
//
// Instead of one JSON tool-call per turn, the model writes a short Python
// program that orchestrates several Deneb tools and processes the data locally,
// returning only what it prints. This collapses multi-tool, batch, and
// cross-source-join work (scan many emails, join calendar × deals × contacts,
// count/filter/aggregate, batch-add calendar events, update the wiki) into a
// single turn — fewer turns, less re-prefill (the dominant local-model latency
// cost).
//
// Security model (see also codeaction_runtime.py):
//   - The model's Python runs in a throwaway subprocess with a MINIMAL env, so
//     the gateway's secret env vars are never inherited.
//   - A PEP 578 audit hook blocks network (except this bridge), subprocess,
//     fs writes outside the scratch dir, secret-path reads, and ctypes.
//   - Deneb tool access goes through an ephemeral loopback HTTP bridge guarded
//     by a one-time token; the bridge enforces an action allowlist server-side:
//     read tools plus recoverable INTERNAL writes (local calendar, git-versioned
//     wiki, workspace-clamped files). Outbound/irreversible actions (email
//     send/reply) are excluded — the model does those as visible top-level tool
//     calls, where StreamHooks.OnBeforeToolCall can gate them.
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

	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
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

// CodeActionDeps wires the code_action tool. Invoker is the read-only tool
// surface (the chat registry) the bridge forwards to. Contacts/Calendar/Wiki
// are the typed sources used to answer deneb.<tool>(..., as_json=True) with
// structured data; any nil disables the structured path for that tool (callers
// fall back to formatted text).
type CodeActionDeps struct {
	Invoker  ToolInvoker
	Contacts *contacts.Store
	Calendar *toolctx.CalendarDeps
	Wiki     *wiki.Store
}

// CodeActionDescription is the deferred-listing description. The first sentence
// is the WHEN trigger (the deferred summary truncates to it).
const CodeActionDescription = "Run Python in a single turn to orchestrate tools or batch/aggregate data — scan many emails, join calendar×contacts×wiki, count/filter/compute, batch-add calendar events, update the wiki — instead of many separate tool calls. A preloaded `deneb` object exposes read tools plus recoverable internal writes (calendar/wiki/workspace files); outbound actions (email send) are NOT available here — do those as a normal tool call. Only what you print() (and any traceback) is returned. The interpreter is sandboxed: no network except the tool bridge, no subprocess, no raw file writes outside a scratch dir, no secret reads."

// codeActionAllowed is the action-granular allowlist. It permits read actions
// plus recoverable INTERNAL writes: calendar create/update/delete (the calendar
// tool lands these in the local store and refuses Google edits) and wiki
// write/log (the wiki dir is git-versioned). Outbound/irreversible actions —
// gmail send/reply/label and attachment download — are intentionally absent; the
// model does those as visible top-level tool calls. (fs read/write/edit have no
// "action" and are gated in codeActionAllow.)
var codeActionAllowed = map[string]map[string]bool{
	"gmail":    {"inbox": true, "search": true, "read": true, "thread": true, "analyze": true},
	"calendar": {"list": true, "get": true, "free_slots": true, "create": true, "update": true, "delete": true},
	"contacts": {"lookup": true, "search": true},
	"wiki":     {"search": true, "read": true, "index": true, "daily": true, "status": true, "write": true, "log": true},
}

// codeActionAllow returns nil if (tool, args.action) is a permitted call, or a
// descriptive error the model can learn from.
func codeActionAllow(tool string, args map[string]any) error {
	// fs read/write/edit have no "action"; they are workspace-clamped
	// (ResolvePath clamps out-of-workspace paths to the workspace, so neither
	// secrets like ~/.deneb nor system files like ~/.bashrc are reachable), so
	// they need no extra gating. write/edit are recoverable internal writes.
	if tool == "read" || tool == "write" || tool == "edit" {
		return nil
	}
	actions, ok := codeActionAllowed[tool]
	if !ok {
		return fmt.Errorf("tool %q is not available from code_action (allowed: gmail, calendar, contacts, wiki, read, write, edit)", tool)
	}
	action, _ := args["action"].(string)
	action = strings.TrimSpace(action)
	if action == "" {
		return fmt.Errorf("%s via code_action requires an 'action' (allowed: %s)", tool, strings.Join(sortedActionKeys(actions), ", "))
	}
	if !actions[action] {
		return fmt.Errorf("%s action %q is not allowed from code_action (allowed: %s; outbound send is not — use a normal tool call)", tool, action, strings.Join(sortedActionKeys(actions), ", "))
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
	invoker  ToolInvoker
	contacts *contacts.Store       // optional; backs structured (as_json) contacts
	calendar *toolctx.CalendarDeps // optional; backs structured calendar
	wiki     *wiki.Store           // optional; backs structured wiki
	token    string
	ctx      context.Context
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
		JSON bool           `json:"json"` // request structured output where supported
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeBridgeJSON(w, map[string]any{"ok": false, "error": "bad request: " + err.Error()})
		return
	}
	if req.Args == nil {
		req.Args = map[string]any{}
	}
	// The read-only allowlist gates both paths identically.
	if err := codeActionAllow(req.Tool, req.Args); err != nil {
		writeBridgeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	// Structured path: serialize the tool's typed data (as_json=True) so model
	// code gets a Python list/dict instead of formatted text to re-parse.
	if req.JSON {
		val, err := b.structuredResult(req.Tool, req.Args)
		if err != nil {
			writeBridgeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		data, err := json.Marshal(val)
		if err != nil {
			writeBridgeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeBridgeJSON(w, map[string]any{"ok": true, "result": string(data)})
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

// structuredResult returns typed data for tools that support as_json=True.
// Only the read-only contacts surface is wired in v1; calendar/wiki can be
// added here as their typed readers are exposed.
func (b *codeActionBridge) structuredResult(tool string, args map[string]any) (any, error) {
	switch tool {
	case "contacts":
		if b.contacts == nil {
			return nil, fmt.Errorf("contacts store is unavailable for structured output")
		}
		return contactsStructured(b.contacts, args)
	case "calendar":
		if b.calendar == nil {
			return nil, fmt.Errorf("calendar is unavailable for structured output")
		}
		return calendarStructured(b.ctx, b.calendar, args)
	case "wiki":
		if b.wiki == nil {
			return nil, fmt.Errorf("wiki is unavailable for structured output")
		}
		return wikiStructured(b.ctx, b.wiki, args)
	default:
		return nil, fmt.Errorf("structured output (as_json=True) is not available for %q — call it without as_json", tool)
	}
}

// --- structured DTOs: stable lowercase JSON contracts for as_json=True ---

type caEvent struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Start     string   `json:"start"` // RFC3339 (KST)
	End       string   `json:"end"`
	Location  string   `json:"location,omitempty"`
	AllDay    bool     `json:"all_day"`
	Status    string   `json:"status,omitempty"`
	Attendees []string `json:"attendees,omitempty"` // emails
}

type caWikiHit struct {
	Path    string  `json:"path"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`
}

type caWikiPage struct {
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	Summary  string   `json:"summary,omitempty"`
	Category string   `json:"category,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Updated  string   `json:"updated,omitempty"`
	Body     string   `json:"body"`
}

func caEventOf(e calendar.Event) caEvent {
	var att []string
	for _, a := range e.Attendees {
		if a.Email != "" {
			att = append(att, a.Email)
		}
	}
	return caEvent{
		ID:        e.ID,
		Title:     e.Summary,
		Start:     e.Start.Format(time.RFC3339),
		End:       e.End.Format(time.RFC3339),
		Location:  e.Location,
		AllDay:    e.AllDay,
		Status:    e.Status,
		Attendees: att,
	}
}

// calendarStructured answers read-only calendar actions with typed data. list
// reuses calMerged (the same Google+local merge the text tool uses, so the two
// can't diverge); get replicates the local:/google ID routing.
func calendarStructured(ctx context.Context, d *toolctx.CalendarDeps, args map[string]any) (any, error) {
	action, _ := args["action"].(string)
	switch strings.TrimSpace(action) {
	case "list", "":
		from, to, errMsg := calResolveWindow(stringArg(args, "from"), stringArg(args, "to"), intArg(args, "hours_ahead"))
		if errMsg != "" {
			return nil, fmt.Errorf("%s", errMsg)
		}
		events, _ := calMerged(ctx, d, from, to)
		out := make([]caEvent, 0, len(events))
		for _, e := range events {
			out = append(out, caEventOf(e))
		}
		return out, nil
	case "get":
		id := strings.TrimSpace(stringArg(args, "id"))
		if id == "" {
			return nil, fmt.Errorf("calendar get requires id")
		}
		ev, err := calStructGet(ctx, d, id)
		if err != nil {
			return nil, err
		}
		if ev == nil {
			return nil, fmt.Errorf("event %q not found", id)
		}
		return caEventOf(*ev), nil
	default:
		return nil, fmt.Errorf("calendar structured: action %q not supported (list, get); use as_json=False for free_slots", action)
	}
}

// calStructGet resolves one event by ID, routing local: IDs to the local store
// and others to the read-only Google client (mirrors calActionGet).
func calStructGet(ctx context.Context, d *toolctx.CalendarDeps, id string) (*calendar.Event, error) {
	if localcal.IsLocalID(id) {
		if d.Local == nil {
			return nil, fmt.Errorf("local calendar unavailable")
		}
		return d.Local.Get(id), nil
	}
	if d.Client == nil {
		return nil, fmt.Errorf("google calendar not connected")
	}
	client, err := d.Client()
	if err != nil {
		return nil, err
	}
	return client.Get(ctx, id)
}

// wikiStructured answers read-only wiki actions with typed data: search returns
// ranked hits, read returns a page's metadata + body.
func wikiStructured(ctx context.Context, store *wiki.Store, args map[string]any) (any, error) {
	action, _ := args["action"].(string)
	query := stringArg(args, "query")
	switch strings.TrimSpace(action) {
	case "search":
		limit := 10
		if n := intArg(args, "limit"); n > 0 {
			limit = n
		}
		hits, err := store.Search(ctx, query, limit)
		if err != nil {
			return nil, err
		}
		out := make([]caWikiHit, 0, len(hits))
		for _, h := range hits {
			out = append(out, caWikiHit{Path: h.Path, Snippet: h.Content, Score: h.Score})
		}
		return out, nil
	case "read":
		if strings.TrimSpace(query) == "" {
			return nil, fmt.Errorf("wiki read requires query (the page path)")
		}
		pg, err := store.ReadPage(query)
		if err != nil {
			return nil, err
		}
		return caWikiPage{
			Path:     query,
			Title:    pg.Meta.Title,
			Summary:  pg.Meta.Summary,
			Category: pg.Meta.Category,
			Tags:     pg.Meta.Tags,
			Updated:  pg.Meta.Updated,
			Body:     pg.Body,
		}, nil
	case "index":
		// Mirrors the text tool (wikiIndex uses p.Category); accept query as a
		// forgiving fallback. Returns page paths so model code can enumerate a
		// category and read/aggregate each page.
		category := strings.TrimSpace(stringArg(args, "category"))
		if category == "" {
			category = strings.TrimSpace(query)
		}
		paths, err := store.ListPages(category)
		if err != nil {
			return nil, err
		}
		if paths == nil {
			paths = []string{}
		}
		return paths, nil
	default:
		return nil, fmt.Errorf("wiki structured: action %q not supported (search, read, index); use as_json=False for daily/status", action)
	}
}

// stringArg / intArg read a bridge arg (JSON map) with the right dynamic type.
func stringArg(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

func intArg(args map[string]any, key string) int {
	if f, ok := args[key].(float64); ok {
		return int(f)
	}
	return 0
}

// contactsStructured answers a read-only contacts call with []contacts.Contact
// (json-tagged) instead of formatted text. Always returns a non-nil slice so
// the Python side decodes to a list, never None.
func contactsStructured(store *contacts.Store, args map[string]any) (any, error) {
	action, _ := args["action"].(string)
	query, _ := args["query"].(string)
	var res []contacts.Contact
	switch action {
	case "lookup":
		res = store.LookupPhone(query)
	case "search":
		limit := 20
		if m, ok := args["max"].(float64); ok && m > 0 {
			limit = int(m)
		}
		res = store.Search(query, limit)
	default:
		return nil, fmt.Errorf("contacts structured: action %q not supported (lookup, search)", action)
	}
	if res == nil {
		res = []contacts.Contact{}
	}
	return res, nil
}

func writeBridgeJSON(w http.ResponseWriter, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// ToolCodeAction returns the code_action tool. d.Invoker is the read-only tool
// surface (the chat registry) the bridge forwards to; the scratch dir is a
// fresh system temp dir per run.
func ToolCodeAction(d CodeActionDeps) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if d.Invoker == nil {
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
			Handler:           &codeActionBridge{invoker: d.Invoker, contacts: d.Contacts, calendar: d.Calendar, wiki: d.Wiki, token: token, ctx: ctx},
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
				"description": "Python 3 source. A preloaded `deneb` object exposes Deneb tools: deneb.gmail(action, query=…, message_id=…, max=…) [inbox|search|read|thread|analyze — read-only, NO send], deneb.calendar(action, **kw) [list|get|free_slots; create|update|delete on the local calendar], deneb.contacts(action, query) [lookup|search], deneb.wiki(action, query=…, **kw) [search|read|index|daily|status; write|log], and workspace files deneb.read(file_path) / deneb.write(file_path, content) / deneb.edit(file_path, old_string, new_string). Pass as_json=True for parsed objects instead of text — deneb.contacts (list of {name,phones,emails,org}), deneb.calendar list/get (events {id,title,start,end,location,all_day,attendees}), deneb.wiki search/read/index ({path,snippet,score} / {path,title,summary,body} / list of page paths) — ideal for filtering, counting, joining. Writes are internal and recoverable; outbound email send is NOT available here — do that as a normal tool call. Use print() to return data — only stdout and any traceback come back. Sandbox: no network except the bridge, no subprocess, no raw file writes outside the scratch dir (use deneb.write for workspace files).",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Wall-clock seconds (default 60, max 120).",
			},
		},
		"required": []any{"code"},
	}
}
