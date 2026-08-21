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

// newStatelessTestClient points the client at a fake server that speaks only
// MCP 2026-07-28: it answers server/discover and rejects initialize outright,
// so any test passing here proves the client never fell back.
func newStatelessTestClient(t *testing.T) *Client {
	t.Helper()
	c, err := New(context.Background(),
		[]string{os.Args[0], "-test.run=^TestHelperProcess$", "--", "mcp-helper", "stateless"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestStatelessServerIsDiscoveredWithoutHandshake(t *testing.T) {
	c := newStatelessTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := c.protocol(); got != protocolVersion2026 {
		t.Fatalf("negotiated protocol = %q, want %q", got, protocolVersion2026)
	}
	// Identity comes from the discover result's `_meta`, not from initialize.
	if name, version := c.ServerInfo(); name != "fake-mcp-2" || version != "2.0.0" {
		t.Errorf("ServerInfo = %q/%q, want fake-mcp-2/2.0.0", name, version)
	}
}

// Every stateless request must carry `_meta`; the fake server rejects any that
// does not, so a passing call is the assertion.
func TestStatelessRequestsCarryPerRequestMetadata(t *testing.T) {
	c := newStatelessTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
	out, err := c.CallTool(ctx, "echo", json.RawMessage(`{"msg":"안녕"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !strings.Contains(out, "안녕") {
		t.Errorf("CallTool = %q, want the arguments echoed", out)
	}
}

// 2026-07-28 removed `ping`; the client must probe liveness with a method the
// server actually implements. The fake server errors on ping, so a passing
// Ping proves the substitution happened.
func TestStatelessPingUsesDiscoverInsteadOfPing(t *testing.T) {
	c := newStatelessTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

// An MRTR interim result asks for a capability this client does not declare.
// It must surface as a named error rather than an empty tool output.
func TestMultiRoundTripResultSurfacesAsError(t *testing.T) {
	c := newStatelessTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := c.CallTool(ctx, "needs-input", json.RawMessage(`{}`))
	if err == nil {
		t.Fatalf("CallTool = %q, want an error for an input_required result", out)
	}
	if !strings.Contains(err.Error(), "sampling/createMessage") {
		t.Errorf("error = %v, want it to name the requested capability", err)
	}
}

// A handshake-era server must still be driven through initialize — the probe
// is detection, not a requirement.
func TestHandshakeEraServerStillNegotiatesViaInitialize(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := c.protocol(); got != "" {
		t.Errorf("negotiated protocol = %q, want the handshake era (empty)", got)
	}
	if name, _ := c.ServerInfo(); name != "fake-mcp" {
		t.Errorf("ServerInfo name = %q, want fake-mcp", name)
	}
	// The failed discover probe is an expected answer, not a fault: it must
	// not leave a scary last-error on a perfectly healthy server.
	if stats := c.Stats(); stats.LastError != "" {
		t.Errorf("LastError = %q, want the discovery probe to stay quiet", stats.LastError)
	}
}

// A handshake-era server may enforce "initialize first" by dropping the
// transport rather than answering "method not found". On stdio that is a dead
// child, leaving nothing to fall back onto — and since every respawn would
// re-run the probe, such a server would be permanently unusable, not just slow
// to start. Detection therefore must not put anything on the wire ahead of
// initialize. This test spawns exactly that server and requires it to work.
func TestStrictLifecycleServerIsNeverProbedBeforeInitialize(t *testing.T) {
	c, err := New(context.Background(),
		[]string{os.Args[0], "-test.run=^TestHelperProcess$", "--", "mcp-helper", "strict-legacy"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(c.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start against a strict-lifecycle server: %v", err)
	}
	if got := c.protocol(); got != "" {
		t.Errorf("negotiated protocol = %q, want the handshake era (empty)", got)
	}
	if name, _ := c.ServerInfo(); name != "strict-mcp" {
		t.Errorf("ServerInfo name = %q, want strict-mcp", name)
	}
	// The child must still be alive and serving: a probe that killed it would
	// leave this failing even though Start had already returned.
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Errorf("tools = %+v, want the single echo tool", tools)
	}
	if stats := c.Stats(); stats.LastError != "" {
		t.Errorf("LastError = %q, want a clean handshake to leave none", stats.LastError)
	}
}

// Only "complete" (or its absence, from a pre-2026-07-28 server) promises a
// finished result. Anything else — an extension result type, a future
// revision's interim shape — must not reach the model looking like success.
func TestUnrecognizedResultTypeIsNotRenderedAsSuccess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     string
		wantErr bool
		want    string
	}{
		{name: "absent is complete", raw: `{"content":[{"type":"text","text":"결과"}]}`, want: "결과"},
		{name: "explicit complete", raw: `{"resultType":"complete","content":[{"type":"text","text":"결과"}]}`, want: "결과"},
		{name: "input required", raw: `{"resultType":"input_required","inputRequests":{"a":{"method":"sampling/createMessage"}}}`, wantErr: true},
		{name: "unknown extension type", raw: `{"resultType":"io.example/deferred","content":[]}`, wantErr: true},
		// The dangerous shape: an unknown type carrying content that would
		// otherwise render as a perfectly ordinary answer.
		{name: "unknown type with plausible content", raw: `{"resultType":"partial","content":[{"type":"text","text":"절반만"}]}`, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := renderToolResult("probe", json.RawMessage(tc.raw))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("renderToolResult = %q, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("renderToolResult: %v", err)
			}
			if got != tc.want {
				t.Errorf("renderToolResult = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWithStatelessMetaBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		params   map[string]any
		protocol string
		wantMeta bool
	}{
		{name: "handshake era leaves nil params nil", params: nil, protocol: ""},
		{name: "handshake era leaves params untouched", params: map[string]any{"name": "x"}, protocol: ""},
		{name: "older revision is not stateless", params: map[string]any{}, protocol: handshakeProtocolVersion},
		{name: "stateless fills nil params", params: nil, protocol: protocolVersion2026, wantMeta: true},
		{name: "stateless augments params", params: map[string]any{"name": "x"}, protocol: protocolVersion2026, wantMeta: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := withStatelessMeta(tc.params, tc.protocol)
			if !tc.wantMeta {
				if got != nil {
					if _, present := got["_meta"]; present {
						t.Fatalf("handshake-era params gained _meta: %v", got)
					}
				}
				return
			}
			meta, _ := got["_meta"].(map[string]any)
			if meta == nil {
				t.Fatalf("params = %v, want _meta", got)
			}
			if meta[metaProtocolVersion] != protocolVersion2026 {
				t.Errorf("_meta protocolVersion = %v", meta[metaProtocolVersion])
			}
			if _, present := meta[metaClientCapabilities]; !present {
				t.Errorf("_meta missing clientCapabilities (required per-request): %v", meta)
			}
			if _, present := meta[metaClientInfo]; !present {
				t.Errorf("_meta missing clientInfo: %v", meta)
			}
			// Pre-existing keys survive.
			if want, ok := tc.params["name"]; ok && got["name"] != want {
				t.Errorf("params lost %q: %v", "name", got)
			}
		})
	}
}

func TestContainsVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		versions []string
		want     bool
	}{
		{name: "nil list", versions: nil},
		{name: "empty list", versions: []string{}},
		{name: "only older revisions", versions: []string{"2025-06-18", "2024-11-05"}},
		{name: "present", versions: []string{"2025-06-18", protocolVersion2026}, want: true},
		{name: "sole entry", versions: []string{protocolVersion2026}, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := containsVersion(tc.versions, protocolVersion2026); got != tc.want {
				t.Errorf("containsVersion(%v) = %v, want %v", tc.versions, got, tc.want)
			}
		})
	}
}

// runStatelessHelper is the MCP 2026-07-28 half of TestHelperProcess: a server
// that implements server/discover, requires per-request `_meta`, and answers
// every handshake-era method with "method not found" — so a client that has
// not adopted the revision fails loudly here instead of quietly degrading.
func runStatelessHelper() {
	replyErr := func(id json.RawMessage, code int, message string) {
		data, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": id,
			"error": map[string]any{"code": code, "message": message},
		})
		fmt.Println(string(data))
	}
	// complete stamps the fields the revision requires on every result.
	complete := func(id json.RawMessage, result map[string]any) {
		result["resultType"] = "complete"
		result["_meta"] = map[string]any{
			metaServerInfo: map[string]any{"name": "fake-mcp-2", "version": "2.0.0"},
		}
		reply(id, result)
	}

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64*1024), maxLineBytes)
	for sc.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
				Cursor    string          `json:"cursor"`
				Meta      map[string]any  `json:"_meta"`
			} `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil {
			continue
		}
		if len(req.ID) == 0 || string(req.ID) == "null" {
			continue // notification
		}
		// Enforce the revision's central requirement: no request is legal
		// without its own protocol version in `_meta`.
		if req.Params.Meta[metaProtocolVersion] != protocolVersion2026 {
			replyErr(req.ID, -32020, "missing per-request protocol version in _meta")
			continue
		}

		switch req.Method {
		case "server/discover":
			complete(req.ID, map[string]any{
				"supportedVersions": []string{protocolVersion2026},
				"capabilities":      map[string]any{"tools": map[string]any{"listChanged": false}},
				"ttlMs":             300000,
				"cacheScope":        "private",
			})
		case "tools/list":
			complete(req.ID, map[string]any{"tools": []map[string]any{
				{"name": "echo", "description": "echoes its arguments", "inputSchema": map[string]any{"type": "object"}},
				{"name": "needs-input", "description": "asks for sampling", "inputSchema": map[string]any{"type": "object"}},
			}})
		case "tools/call":
			switch req.Params.Name {
			case "echo":
				complete(req.ID, map[string]any{
					"content": []map[string]any{{"type": "text", "text": string(req.Params.Arguments)}},
				})
			case "needs-input":
				// MRTR: an interim result asking the client for an LLM
				// completion this gateway cannot provide.
				reply(req.ID, map[string]any{
					"resultType":   "input_required",
					"requestState": "opaque-state",
					"inputRequests": map[string]any{
						"a": map[string]any{"method": "sampling/createMessage"},
					},
				})
			default:
				replyErr(req.ID, -32602, "unknown tool: "+req.Params.Name)
			}
		default:
			// Covers initialize and ping: both are gone in this revision, and
			// a client that still calls them must see the failure.
			replyErr(req.ID, -32601, "method not found: "+req.Method)
		}
	}
	os.Exit(0)
}

// runStrictLegacyHelper is a handshake-era server that enforces the lifecycle
// the way the strictest real ones do: anything before initialize and it exits,
// taking the stdio transport with it. A client that probes for MCP 2.0 ahead
// of the handshake gets a dead child and no way back.
func runStrictLegacyHelper() {
	initialized := false
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64*1024), maxLineBytes)
	for sc.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil {
			continue
		}
		if !initialized && req.Method != "initialize" {
			os.Exit(1) // lifecycle violation — drop the transport
		}
		if len(req.ID) == 0 || string(req.ID) == "null" {
			continue // notification (notifications/initialized)
		}
		switch req.Method {
		case "initialize":
			initialized = true
			reply(req.ID, map[string]any{
				"protocolVersion": handshakeProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "strict-mcp", "version": "1.0.0"},
			})
		case "tools/list":
			reply(req.ID, map[string]any{"tools": []map[string]any{
				{"name": "echo", "description": "echoes", "inputSchema": map[string]any{"type": "object"}},
			}})
		default:
			data, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": req.ID,
				"error": map[string]any{"code": -32601, "message": "method not found"},
			})
			fmt.Println(string(data))
		}
	}
	os.Exit(0)
}
