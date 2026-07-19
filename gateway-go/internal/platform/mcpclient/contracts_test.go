package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestRenderToolResultContractAdditional(t *testing.T) {
	for _, tt := range []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{name: "empty", raw: `{}`, want: ""},
		{name: "text", raw: `{"content":[{"type":"text","text":"hello"}]}`, want: "hello"},
		{name: "multiple text", raw: `{"content":[{"type":"text","text":"one"},{"type":"text","text":"two"}]}`, want: "one\ntwo"},
		{name: "resource", raw: `{"content":[{"type":"resource","resource":{"uri":"file:///a","text":"body"}}]}`, want: "[resource file:///a]\nbody"},
		{name: "missing resource", raw: `{"content":[{"type":"resource"}]}`, want: "[resource content omitted]"},
		{name: "image", raw: `{"content":[{"type":"image"}]}`, want: "[image content omitted]"},
		{name: "structured fallback", raw: `{"structuredContent":{"a":1}}`, want: `{"a":1}`},
		{name: "placeholder plus structured", raw: `{"content":[{"type":"image"}],"structuredContent":{"a":1}}`, want: "[image content omitted]\n{\"a\":1}"},
		{name: "blank text plus structured", raw: `{"content":[{"type":"text","text":"  "}],"structuredContent":[1,2]}`, want: "[1,2]"},
		{name: "substantive text wins", raw: `{"content":[{"type":"text","text":"text"}],"structuredContent":{"ignored":true}}`, want: "text"},
		{name: "tool error", raw: `{"content":[{"type":"text","text":"bad input"}],"isError":true}`, wantErr: "mcp tool demo failed: bad input"},
		{name: "malformed", raw: `{`, wantErr: "tools/call result"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderToolResult("demo", json.RawMessage(tt.raw))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) || got != "" {
					t.Fatalf("got=%q err=%v", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got=%q/%v want=%q", got, err, tt.want)
			}
		})
	}
}

func TestRenderToolResultErrorBoundAndUTF8(t *testing.T) {
	text := strings.Repeat("가", 1000)
	raw, _ := json.Marshal(map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": true})
	_, err := renderToolResult("unicode", raw)
	if err == nil || !utf8.ValidString(err.Error()) || !strings.Contains(err.Error(), "[truncated]") || len(err.Error()) > maxErrorTextBytes+100 {
		t.Fatalf("error len=%d valid=%v err=%v", len(err.Error()), utf8.ValidString(err.Error()), err)
	}
}

func TestTruncateRuneSafeContractAdditional(t *testing.T) {
	for _, tt := range []struct {
		s    string
		max  int
		want string
	}{
		{s: "abc", max: 3, want: "abc"},
		{s: "abcd", max: 3, want: "abc… [truncated]"},
		{s: "가나다", max: 4, want: "가… [truncated]"},
		{s: "abc", max: 0, want: "… [truncated]"},
		{s: "", max: 0, want: ""},
	} {
		got := truncateRuneSafe(tt.s, tt.max)
		if got != tt.want || !utf8.ValidString(got) {
			t.Errorf("truncate(%q,%d)=%q want=%q", tt.s, tt.max, got, tt.want)
		}
	}
}

func TestNewContractAdditional(t *testing.T) {
	if _, err := New(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "empty command") {
		t.Fatalf("empty = %v", err)
	}
	if _, err := New(context.Background(), []string{}, nil); err == nil {
		t.Fatal("empty slice accepted")
	}
	c, err := New(nil, []string{"server", "--flag"}, nil) //nolint:staticcheck // contract under test: New must default a nil context
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.argv, []string{"server", "--flag"}) || c.logger == nil || c.lifeCtx == nil || c.pending == nil {
		t.Fatalf("client = %+v", c)
	}
}

func TestChildEnvAllowlistAndLocaleFamily(t *testing.T) {
	t.Setenv("PATH", "/bin")
	t.Setenv("HOME", "/home/test")
	t.Setenv("LC_TEST", "ko_KR.UTF-8")
	t.Setenv("OPENAI_API_KEY", "secret")
	t.Setenv("DENEB_TELEGRAM_TOKEN", "secret2")
	env := childEnv()
	joined := strings.Join(env, "\n")
	for _, want := range []string{"PATH=/bin", "HOME=/home/test", "LC_TEST=ko_KR.UTF-8"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q: %v", want, env)
		}
	}
	for _, secret := range []string{"OPENAI_API_KEY", "DENEB_TELEGRAM_TOKEN", "secret2"} {
		if strings.Contains(joined, secret) {
			t.Errorf("secret leaked %q: %v", secret, env)
		}
	}
}

func TestStats_ReturnsIndependentCopyOfCounters(t *testing.T) {
	c, _ := New(context.Background(), []string{"server"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Now()
	c.mu.Lock()
	c.ready, c.closed, c.serverName, c.serverVersion, c.spawns = true, false, "srv", "1.2", 3
	c.lastError, c.lastErrorAt = "last", now
	c.mu.Unlock()
	c.calls.Store(8)
	c.callErrors.Store(2)
	c.stderrMu.Lock()
	c.stderrTail = []string{"one", "two"}
	c.stderrMu.Unlock()
	got := c.Stats()
	if !got.Ready || got.Closed || got.ServerName != "srv" || got.ServerVersion != "1.2" || got.Spawns != 3 || got.Calls != 8 || got.CallErrors != 2 || got.LastError != "last" || !got.LastErrorAt.Equal(now) || !reflect.DeepEqual(got.RecentStderr, []string{"one", "two"}) {
		t.Fatalf("Stats = %+v", got)
	}
	got.RecentStderr[0] = "mutated"
	if c.Stats().RecentStderr[0] != "one" {
		t.Fatal("Stats stderr aliases client")
	}
}

func TestErrorStatsAndStderrRingContracts(t *testing.T) {
	c, _ := New(context.Background(), []string{"server"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	want := errors.New("failure")
	if got := c.noteCallError(want); !errors.Is(got, want) {
		t.Fatalf("noteCallError = %v", got)
	}
	stats := c.Stats()
	if stats.CallErrors != 1 || stats.LastError != "failure" || stats.LastErrorAt.IsZero() {
		t.Fatalf("stats = %+v", stats)
	}
	c.stderrMu.Lock()
	c.stderrGen = 2
	c.stderrMu.Unlock()
	c.recordStderr(1, "stale")
	for i := 0; i < stderrRingSize+5; i++ {
		c.recordStderr(2, fmt.Sprintf("line-%02d", i))
	}
	stats = c.Stats()
	if len(stats.RecentStderr) != stderrRingSize || stats.RecentStderr[0] != "line-05" || stats.RecentStderr[len(stats.RecentStderr)-1] != "line-24" {
		t.Fatalf("ring = %v", stats.RecentStderr)
	}
	if got := c.stderrSnippet(3); got != "line-22 | line-23 | line-24" {
		t.Fatalf("snippet = %q", got)
	}
	if got := c.stderrSnippet(100); !strings.HasPrefix(got, "line-05") {
		t.Fatalf("large snippet = %q", got)
	}
	if got := c.stderrSnippet(0); got != "" {
		t.Fatalf("zero snippet = %q", got)
	}
	long := strings.Repeat("가", 200)
	c.recordStderr(2, long)
	last := c.Stats().RecentStderr[stderrRingSize-1]
	if len(last) > stderrLineCap+20 || !utf8.ValidString(last) || !strings.Contains(last, "truncated") {
		t.Fatalf("long line = len%d %q", len(last), last)
	}
}

type rpcHarness struct {
	c         *Client
	serverIn  *os.File
	serverOut *os.File
	done      chan struct{}
}

func newRPCHarness(t *testing.T, handler func(map[string]any) map[string]any) *rpcHarness {
	t.Helper()
	serverIn, clientIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	clientOut, serverOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	c, _ := New(context.Background(), []string{"fake"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.mu.Lock()
	c.ready = true
	c.stdin = clientIn
	c.mu.Unlock()
	h := &rpcHarness{c: c, serverIn: serverIn, serverOut: serverOut, done: make(chan struct{})}
	go c.readLoop(clientOut)
	go func() {
		defer close(h.done)
		sc := bufio.NewScanner(serverIn)
		for sc.Scan() {
			var req map[string]any
			if json.Unmarshal(sc.Bytes(), &req) != nil {
				continue
			}
			resp := handler(req)
			if resp == nil {
				continue
			}
			resp["jsonrpc"] = "2.0"
			resp["id"] = req["id"]
			data, _ := json.Marshal(resp)
			_, _ = serverOut.Write(append(data, '\n'))
		}
	}()
	return h
}

func (h *rpcHarness) close() {
	h.c.mu.Lock()
	if h.c.stdin != nil {
		_ = h.c.stdin.Close()
		h.c.stdin = nil
	}
	h.c.mu.Unlock()
	_ = h.serverIn.Close()
	_ = h.serverOut.Close()
	select {
	case <-h.done:
	case <-time.After(time.Second):
	}
}

func TestPing_ListToolsAndCallToolReturnExpectedResults(t *testing.T) {
	page := 0
	h := newRPCHarness(t, func(req map[string]any) map[string]any {
		switch req["method"] {
		case "ping":
			return map[string]any{"result": map[string]any{}}
		case "tools/list":
			page++
			if page == 1 {
				return map[string]any{"result": map[string]any{"tools": []any{map[string]any{"name": "one", "description": "first", "inputSchema": map[string]any{"type": "object"}}}, "nextCursor": "next"}}
			}
			params := req["params"].(map[string]any)
			if params["cursor"] != "next" {
				t.Errorf("cursor params = %#v", params)
			}
			return map[string]any{"result": map[string]any{"tools": []any{map[string]any{"name": "two"}}}}
		case "tools/call":
			params := req["params"].(map[string]any)
			if params["name"] != "demo" {
				t.Errorf("call params = %#v", params)
			}
			return map[string]any{"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "result"}}}}
		}
		return map[string]any{"error": map[string]any{"code": -32601, "message": "unknown"}}
	})
	defer h.close()
	if err := h.c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	tools, err := h.c.ListTools(context.Background())
	if err != nil || len(tools) != 2 || tools[0].Name != "one" || tools[1].Name != "two" {
		t.Fatalf("tools = %+v/%v", tools, err)
	}
	got, err := h.c.CallTool(context.Background(), "demo", nil)
	if err != nil || got != "result" {
		t.Fatalf("CallTool = %q/%v", got, err)
	}
	stats := h.c.Stats()
	if stats.Calls != 4 || stats.CallErrors != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestListToolsRepeatedCursorStops(t *testing.T) {
	h := newRPCHarness(t, func(map[string]any) map[string]any {
		return map[string]any{"result": map[string]any{"tools": []any{map[string]any{"name": "one"}}, "nextCursor": "same"}}
	})
	defer h.close()
	if _, err := h.c.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "repeated cursor") {
		t.Fatalf("error = %v", err)
	}
}

func TestRoundTripServerErrorAndCancellation(t *testing.T) {
	h := newRPCHarness(t, func(req map[string]any) map[string]any {
		if req["method"] == "error" {
			return map[string]any{"error": map[string]any{"code": -32000, "message": "broken"}}
		}
		return nil
	})
	defer h.close()
	if _, err := h.c.roundTrip(context.Background(), "error", nil); err == nil || !strings.Contains(err.Error(), "-32000: broken") {
		t.Fatalf("server error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := h.c.roundTrip(ctx, "never", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout = %v", err)
	}
	h.c.pendingMu.Lock()
	pending := len(h.c.pending)
	h.c.pendingMu.Unlock()
	if pending != 0 || h.c.Stats().CallErrors != 2 {
		t.Fatalf("pending/stats = %d/%+v", pending, h.c.Stats())
	}
}

type bufferWriteCloser struct {
	mu sync.Mutex
	bytes.Buffer
	err error
}

func (b *bufferWriteCloser) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return 0, b.err
	}
	return b.Buffer.Write(p)
}
func (*bufferWriteCloser) Close() error { return nil }

func TestSendContractAndMissingProcess(t *testing.T) {
	c, _ := New(context.Background(), []string{"server"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := c.send(map[string]any{"method": "x"}); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("missing send = %v", err)
	}
	w := &bufferWriteCloser{}
	c.mu.Lock()
	c.stdin = w
	c.generation = 1
	c.mu.Unlock()
	if err := c.send(map[string]any{"jsonrpc": "2.0", "method": "notify"}); err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	data := w.String()
	w.mu.Unlock()
	if !strings.HasSuffix(data, "\n") || !strings.Contains(data, `"method":"notify"`) {
		t.Fatalf("wire = %q", data)
	}
	w.err = errors.New("pipe broken")
	if err := c.send(map[string]any{"method": "x"}); err == nil || !strings.Contains(err.Error(), "write") {
		t.Fatalf("write error = %v", err)
	}
}

func TestReadLoopRoutesResponsesAndIgnoresGarbage(t *testing.T) {
	c, _ := New(context.Background(), []string{"server"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ch := make(chan rpcResponse, 1)
	c.pending[7] = ch
	input := strings.Join([]string{
		"", "not-json", `{"id":"string","result":{}}`, `{"id":99,"result":{"late":true}}`, `{"id":7,"result":{"ok":true}}`, "",
	}, "\n")
	c.readLoop(strings.NewReader(input))
	select {
	case got := <-ch:
		if got.Err != nil || string(got.Result) != `{"ok":true}` {
			t.Fatalf("response = %+v", got)
		}
	default:
		t.Fatal("response not routed")
	}
	if len(c.pending) != 0 {
		t.Fatalf("pending = %#v", c.pending)
	}
}

func TestOnNotification_IgnoresOtherMarksListChanged(t *testing.T) {
	c, _ := New(context.Background(), []string{"server"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.onNotification("other")
	if c.listChangedLogged {
		t.Fatal("other marked list change")
	}
	c.onNotification("notifications/tools/list_changed")
	if !c.listChangedLogged {
		t.Fatal("list change not marked")
	}
	c.onNotification("notifications/tools/list_changed")
}

func TestServerInfoCloseAndClosedStart(t *testing.T) {
	c, _ := New(context.Background(), []string{"server"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.mu.Lock()
	c.serverName, c.serverVersion = "name", "version"
	c.mu.Unlock()
	if name, version := c.ServerInfo(); name != "name" || version != "version" {
		t.Fatalf("info = %q/%q", name, version)
	}
	c.Close()
	c.Close()
	if !c.Stats().Closed {
		t.Fatal("not closed")
	}
	if err := c.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Start closed = %v", err)
	}
}
