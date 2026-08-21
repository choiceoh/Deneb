package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// helperArgv returns a command line that re-executes this test binary as a
// fake MCP server (TestHelperProcess). Activation rides an argv flag rather
// than an env var because childEnv() deliberately withholds custom variables
// from the spawned process.
func helperArgv(t *testing.T) []string {
	t.Helper()
	return []string{os.Args[0], "-test.run=^TestHelperProcess$", "--", "mcp-helper"}
}

func newTestClient(t *testing.T) *Client {
	t.Helper()
	c, err := New(context.Background(), helperArgv(t), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestStartListCall(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// tools/list follows pagination (fake server returns 2 pages).
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 || tools[0].Name != "echo" || tools[1].Name != "boom" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
	if tools[0].InputSchema["type"] != "object" {
		t.Fatalf("echo schema not preserved: %+v", tools[0].InputSchema)
	}

	// tools/call happy path — text content blocks are concatenated. The
	// helper also reports whether the client answered the string-id
	// server-initiated request it sent right after initialize.
	out, err := c.CallTool(ctx, "echo", json.RawMessage(`{"msg":"안녕"}`))
	if err != nil {
		t.Fatalf("CallTool echo: %v", err)
	}
	if !strings.Contains(out, `"msg":"안녕"`) {
		t.Fatalf("echo output missing args: %q", out)
	}
	if !strings.Contains(out, "[image content omitted]") {
		t.Fatalf("non-text block not surfaced: %q", out)
	}
	if !strings.Contains(out, "srvreply=true") {
		t.Fatalf("string-id server request was not answered: %q", out)
	}

	// isError=true surfaces as a Go error carrying the content text.
	if _, err := c.CallTool(ctx, "boom", nil); err == nil || !strings.Contains(err.Error(), "kaboom") {
		t.Fatalf("CallTool boom: want kaboom error, got %v", err)
	}

	// isError text is bounded (huge payloads must not ride into the model turn).
	if _, err := c.CallTool(ctx, "bigboom", nil); err == nil ||
		len(err.Error()) > maxErrorTextBytes+100 || !strings.Contains(err.Error(), "[truncated]") {
		errLen := -1
		if err != nil {
			errLen = len(err.Error())
		}
		t.Fatalf("CallTool bigboom: want bounded truncated error, got len=%d", errLen)
	}

	// structuredContent-only results fall back to the structured JSON.
	out, err = c.CallTool(ctx, "structured", nil)
	if err != nil || !strings.Contains(out, `"total":42`) {
		t.Fatalf("structured fallback: out=%q err=%v", out, err)
	}

	// Placeholder-only content (image block) must NOT mask structuredContent:
	// substance-based fallback keeps the machine-readable payload.
	out, err = c.CallTool(ctx, "structured-image", nil)
	if err != nil || !strings.Contains(out, `"total":42`) || !strings.Contains(out, "[image content omitted]") {
		t.Fatalf("structured+image fallback: out=%q err=%v", out, err)
	}

	// Embedded resource blocks contribute their text (labeled), not an
	// "omitted" placeholder.
	out, err = c.CallTool(ctx, "with-resource", nil)
	if err != nil || !strings.Contains(out, "full transcript body") || !strings.Contains(out, "plaud://rec/1") {
		t.Fatalf("resource rendering: out=%q err=%v", out, err)
	}

	// Unknown method errors from the server propagate.
	if _, err := c.roundTrip(ctx, "nonsense/method", nil); err == nil {
		t.Fatal("nonsense method: want error, got nil")
	}

	// Stats reflect the traffic above.
	st := c.Stats()
	if !st.Ready || st.Spawns != 1 || st.Calls == 0 || st.CallErrors == 0 || st.ServerName != "fake-mcp" {
		t.Fatalf("Stats: %+v", st)
	}
}

func TestServerExitFailsCallsAndBacksOff(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// "die" makes the fake server exit without responding: the in-flight
	// call must fail fast (not hang until ctx expiry).
	if _, err := c.CallTool(ctx, "die", nil); err == nil {
		t.Fatal("CallTool die: want error, got nil")
	}

	// A crash AFTER a successful init must not be mislabeled as an
	// initialization failure in Stats (initDone is reset on init success,
	// so stopLocked's mid-init branch cannot fire here).
	if st := c.Stats(); strings.Contains(st.LastError, "during initialization") {
		t.Fatalf("runtime crash mislabeled as init failure: %q", st.LastError)
	}

	// Immediate follow-up is inside the respawn backoff window.
	if _, err := c.CallTool(ctx, "echo", nil); err == nil || !strings.Contains(err.Error(), "backoff") {
		t.Fatalf("want backoff error, got %v", err)
	}
}

func TestClosedClientRejectsCalls(t *testing.T) {
	c := newTestClient(t)
	c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Start(ctx); err == nil {
		t.Fatal("Start after Close: want error, got nil")
	}
}

func TestCloseProcessKeepsClientUsable(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	c.CloseProcess()
	// Within the respawn backoff window the client must report backoff —
	// NOT "client closed" — proving it stays usable for later consumers.
	if _, err := c.CallTool(ctx, "echo", nil); err == nil || !strings.Contains(err.Error(), "backoff") {
		t.Fatalf("want backoff error after CloseProcess, got %v", err)
	}
}

// TestInitErrorCarriesStderrHint proves the diagnosis ring: when the child
// prints an auth hint on stderr and dies before completing the handshake,
// the initialization error itself quotes that stderr — the operator's fix
// (the OAuth URL) rides the error, not just the log.
func TestInitErrorCarriesStderrHint(t *testing.T) {
	c, err := New(context.Background(),
		[]string{os.Args[0], "-test.run=^TestHelperProcess$", "--", "mcp-helper", "auth-hint"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(c.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	startErr := c.Start(ctx)
	if startErr == nil {
		t.Fatal("Start: want init error, got nil")
	}
	if !strings.Contains(startErr.Error(), "auth.example") {
		t.Fatalf("init error does not quote stderr hint: %v", startErr)
	}
	st := c.Stats()
	if len(st.RecentStderr) == 0 || !strings.Contains(strings.Join(st.RecentStderr, " "), "auth.example") {
		t.Fatalf("Stats().RecentStderr missing hint: %+v", st.RecentStderr)
	}
	if st.Spawns != 1 || st.LastError == "" {
		t.Fatalf("Stats bookkeeping off: %+v", st)
	}
}

// TestGracefulStopSendsTERM proves the tiered teardown: CloseProcess must
// deliver SIGTERM (which the helper acknowledges by writing a sentinel file)
// rather than going straight to SIGKILL.
func TestGracefulStopSendsTERM(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "got-term")
	c, err := New(context.Background(),
		[]string{os.Args[0], "-test.run=^TestHelperProcess$", "--", "mcp-helper", "term-file=" + sentinel}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(c.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	c.CloseProcess()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(sentinel); err == nil {
			return // SIGTERM observed by the child before any SIGKILL
		}
		if time.Now().After(deadline) {
			t.Fatal("child never observed SIGTERM — teardown not graceful")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestInitWaitIsCancelable proves two properties of the ready-future init:
// an impatient caller returns on ITS OWN ctx without waiting for the slow
// handshake, and the handshake keeps running in the background so a patient
// caller succeeds afterwards.
func TestInitWaitIsCancelable(t *testing.T) {
	c, err := New(context.Background(),
		[]string{os.Args[0], "-test.run=^TestHelperProcess$", "--", "mcp-helper", "slow-init"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(c.Close)

	short, cancelShort := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelShort()
	begin := time.Now()
	if _, err := c.CallTool(short, "echo", nil); err == nil {
		t.Fatal("impatient call: want ctx error, got nil")
	}
	if elapsed := time.Since(begin); elapsed > 1200*time.Millisecond {
		t.Fatalf("impatient call blocked %v — init wait not cancelable", elapsed)
	}

	long, cancelLong := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelLong()
	if err := c.Start(long); err != nil {
		t.Fatalf("patient Start after background init: %v", err)
	}
	if err := c.Ping(long); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if name, _ := c.ServerInfo(); name != "fake-mcp" {
		t.Fatalf("ServerInfo name = %q, want fake-mcp", name)
	}
}

func TestChildEnvAllowlist(t *testing.T) {
	t.Setenv("DENEB_FAKE_SECRET", "supersecret")
	t.Setenv("LC_ALL", "C.UTF-8")
	env := childEnv()
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "DENEB_FAKE_SECRET") {
		t.Fatal("secret-bearing variable leaked into child env")
	}
	if !strings.Contains(joined, "PATH=") {
		t.Fatal("PATH missing from child env")
	}
	if !strings.Contains(joined, "LC_ALL=C.UTF-8") {
		t.Fatal("LC_* prefix family not passed through")
	}
}

// TestHelperProcess is not a real test: when re-executed with the
// "mcp-helper" argv flag it acts as a fake MCP server over stdio
// (newline-delimited JSON-RPC).
func TestHelperProcess(t *testing.T) {
	isHelper, slowInit, authHint, stateless, strictLegacy := false, false, false, false, false
	termFile := ""
	for _, arg := range os.Args {
		switch {
		case arg == "mcp-helper":
			isHelper = true
		case arg == "stateless":
			stateless = true
		case arg == "strict-legacy":
			strictLegacy = true
		case arg == "slow-init":
			slowInit = true
		case arg == "auth-hint":
			authHint = true
		case strings.HasPrefix(arg, "term-file="):
			termFile = strings.TrimPrefix(arg, "term-file=")
		}
	}
	if !isHelper {
		return
	}
	if stateless {
		// MCP 2026-07-28 server — see runStatelessHelper in stateless_test.go.
		runStatelessHelper()
		return
	}
	if strictLegacy {
		// Handshake-era server that enforces its lifecycle by dying — see
		// runStrictLegacyHelper in stateless_test.go.
		runStrictLegacyHelper()
		return
	}
	if authHint {
		// Simulate a wrapper whose OAuth is pending: hint on stderr, then die
		// without ever answering initialize. The sleep lets the client's
		// stderr goroutine consume the line before stdout EOF lands.
		fmt.Fprintln(os.Stderr, "please visit https://auth.example/device to authorize")
		time.Sleep(500 * time.Millisecond)
		os.Exit(3)
	}
	if termFile != "" {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM)
		go func() {
			<-ch
			_ = os.WriteFile(termFile, []byte("term"), 0o600)
			os.Exit(0)
		}()
	}
	// Resilience probe: a garbage line before any response must be skipped
	// by the client's read loop.
	fmt.Println("this is not json")

	gotSrvReply := false
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64*1024), maxLineBytes)
	for sc.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Error  *rpcError       `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil {
			continue
		}
		hasID := len(req.ID) > 0 && string(req.ID) != "null"
		if req.Method == "" && hasID {
			// Client's reply to our server-initiated request below.
			if string(req.ID) == `"srv-1"` && req.Error != nil {
				gotSrvReply = true
			}
			continue
		}
		if !hasID {
			continue // notification
		}
		switch req.Method {
		case "ping":
			reply(req.ID, map[string]any{})
		case "initialize":
			if slowInit {
				time.Sleep(1500 * time.Millisecond)
			}
			reply(req.ID, map[string]any{
				"protocolVersion": handshakeProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "fake-mcp", "version": "0.0.1"},
			})
			// Server-initiated request with a STRING id: the client must
			// answer method-not-found with the id echoed verbatim.
			fmt.Println(`{"jsonrpc":"2.0","id":"srv-1","method":"roots/list"}`)
		case "tools/list":
			var p struct {
				Cursor string `json:"cursor"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.Cursor == "" {
				reply(req.ID, map[string]any{
					"tools": []map[string]any{{
						"name":        "echo",
						"description": "echoes its arguments",
						"inputSchema": map[string]any{"type": "object"},
					}},
					"nextCursor": "page2",
				})
			} else {
				reply(req.ID, map[string]any{
					"tools": []map[string]any{{
						"name":        "boom",
						"description": "always fails",
						"inputSchema": map[string]any{"type": "object"},
					}},
				})
			}
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			switch p.Name {
			case "echo":
				reply(req.ID, map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": fmt.Sprintf("%s srvreply=%v", p.Arguments, gotSrvReply)},
						{"type": "image", "data": "…"},
					},
				})
			case "boom":
				reply(req.ID, map[string]any{
					"content": []map[string]any{{"type": "text", "text": "kaboom"}},
					"isError": true,
				})
			case "bigboom":
				reply(req.ID, map[string]any{
					"content": []map[string]any{{"type": "text", "text": strings.Repeat("에러", 4000)}},
					"isError": true,
				})
			case "structured":
				// structuredContent only (2025-06-18) — client must fall back
				// to it when the content array renders to nothing.
				reply(req.ID, map[string]any{
					"content":           []map[string]any{},
					"structuredContent": map[string]any{"total": 42},
				})
			case "structured-image":
				// Placeholder-only content + structuredContent: the payload
				// must survive alongside the placeholder.
				reply(req.ID, map[string]any{
					"content":           []map[string]any{{"type": "image", "data": "…"}},
					"structuredContent": map[string]any{"total": 42},
				})
			case "with-resource":
				reply(req.ID, map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": "summary"},
						{"type": "resource", "resource": map[string]any{
							"uri": "plaud://rec/1", "text": "full transcript body",
						}},
					},
				})
			case "die":
				os.Exit(1)
			}
		default:
			data, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": req.ID,
				"error": map[string]any{"code": -32601, "message": "method not found"},
			})
			fmt.Println(string(data))
		}
	}
	if termFile != "" {
		// Teardown closes stdin BEFORE signaling (MCP stdio shutdown order),
		// so the scan loop above ends first. Exiting here would race the
		// SIGTERM handler — stay alive so the sentinel proves the signal
		// (the client's KILL escalation is the backstop if TERM never comes).
		time.Sleep(30 * time.Second)
	}
	os.Exit(0)
}

func reply(id json.RawMessage, result map[string]any) {
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	fmt.Println(string(data))
}
