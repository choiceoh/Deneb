package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// helperArgv returns a command line that re-executes this test binary as a
// fake MCP server (TestHelperProcess). The GO_MCP_HELPER env var is set via
// t.Setenv so it reaches the child through os.Environ.
func helperArgv(t *testing.T) []string {
	t.Helper()
	t.Setenv("GO_MCP_HELPER", "1")
	return []string{os.Args[0], "-test.run=^TestHelperProcess$"}
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

	// tools/call happy path — text content blocks are concatenated.
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

	// isError=true surfaces as a Go error carrying the content text.
	if _, err := c.CallTool(ctx, "boom", nil); err == nil || !strings.Contains(err.Error(), "kaboom") {
		t.Fatalf("CallTool boom: want kaboom error, got %v", err)
	}

	// Unknown method errors from the server propagate.
	if _, err := c.roundTrip(ctx, "nonsense/method", nil); err == nil {
		t.Fatal("nonsense method: want error, got nil")
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

// TestHelperProcess is not a real test: when re-executed with GO_MCP_HELPER=1
// it acts as a fake MCP server over stdio (newline-delimited JSON-RPC).
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_MCP_HELPER") != "1" {
		return
	}
	// Resilience probe: a garbage line before any response must be skipped
	// by the client's read loop.
	fmt.Println("this is not json")

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64*1024), maxLineBytes)
	for sc.Scan() {
		var req struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil {
			continue
		}
		if req.ID == nil {
			continue // notification
		}
		switch req.Method {
		case "initialize":
			reply(*req.ID, map[string]any{
				"protocolVersion": protocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "fake-mcp", "version": "0.0.1"},
			})
		case "tools/list":
			var p struct {
				Cursor string `json:"cursor"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.Cursor == "" {
				reply(*req.ID, map[string]any{
					"tools": []map[string]any{{
						"name":        "echo",
						"description": "echoes its arguments",
						"inputSchema": map[string]any{"type": "object"},
					}},
					"nextCursor": "page2",
				})
			} else {
				reply(*req.ID, map[string]any{
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
				reply(*req.ID, map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": string(p.Arguments)},
						{"type": "image", "data": "…"},
					},
				})
			case "boom":
				reply(*req.ID, map[string]any{
					"content": []map[string]any{{"type": "text", "text": "kaboom"}},
					"isError": true,
				})
			case "die":
				os.Exit(1)
			}
		default:
			replyErr(*req.ID, -32601, "method not found")
		}
	}
	os.Exit(0)
}

func reply(id int64, result map[string]any) {
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	fmt.Println(string(data))
}

func replyErr(id int64, code int, msg string) {
	data, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": msg},
	})
	fmt.Println(string(data))
}
