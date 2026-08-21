package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/mcpapi"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativeauth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func postMCP(t *testing.T, s *Server, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/mcp", bytes.NewReader([]byte(body)))
	if token != "" {
		req.Header.Set(clientauth.Header, token)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rec := httptest.NewRecorder()
	mcpHandler(s).ServeHTTP(rec, req)
	return rec
}

func mcpHandler(s *Server) *mcpapi.Handler {
	return mcpapi.New(mcpapi.Config{
		Authenticate: func(w http.ResponseWriter, r *http.Request) (*clientauth.Identity, bool) {
			return nativeauth.Authenticate(w, r, s.logger)
		},
		Dispatcher: s.dispatcher,
		Version:    s.version,
		Logger:     s.logger,
	})
}

func decodeMCP(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode MCP response: %v (body %q)", err, rec.Body.String())
	}
	return out
}

func TestMCPInitializeReturnsNegotiatedVersionAndToolList(t *testing.T) {
	token := withClientToken(t)
	s := newTestServer(t)

	rec := postMCP(t, s, token,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","clientInfo":{"name":"claude-code"}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("initialize status = %d, body %s", rec.Code, rec.Body.String())
	}
	out := decodeMCP(t, rec)
	result, _ := out["result"].(map[string]any)
	if result == nil {
		t.Fatalf("no result: %v", out)
	}
	if result["protocolVersion"] != "2025-03-26" {
		t.Errorf("protocolVersion = %v, want echoed 2025-03-26", result["protocolVersion"])
	}
	info, _ := result["serverInfo"].(map[string]any)
	if info == nil || info["name"] != "deneb" {
		t.Errorf("serverInfo = %v", result["serverInfo"])
	}
	caps, _ := result["capabilities"].(map[string]any)
	if caps == nil || caps["tools"] == nil {
		t.Errorf("capabilities missing tools: %v", result["capabilities"])
	}

	// An unsupported requested version negotiates down to the newest revision
	// that still speaks initialize — NOT to 2026-07-28, which removed the
	// handshake the client just used.
	rec = postMCP(t, s, token, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2099-01-01"}}`)
	out = decodeMCP(t, rec)
	if got := out["result"].(map[string]any)["protocolVersion"]; got != "2025-06-18" {
		t.Errorf("negotiated version = %v, want 2025-06-18", got)
	}

	rec = postMCP(t, s, token, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	out = decodeMCP(t, rec)
	tools, _ := out["result"].(map[string]any)["tools"].([]any)
	if want := len(mcpapi.ToolDefinitions()); len(tools) != want {
		t.Fatalf("tools = %d, want %d", len(tools), want)
	}
	first, _ := tools[0].(map[string]any)
	if first["name"] != "wiki_search" {
		t.Errorf("first tool = %v", first["name"])
	}
	schema, _ := first["inputSchema"].(map[string]any)
	if schema == nil || schema["type"] != "object" {
		t.Errorf("wiki_search schema = %v", first["inputSchema"])
	}
}

func TestMCPToolCallDispatchesToRPCAndReturnsResult(t *testing.T) {
	token := withClientToken(t)
	s := newTestServer(t)

	var seenParams string
	s.dispatcher.Register("miniapp.memory.search", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		seenParams = string(req.Params)
		return rpcutil.RespondOK(req.ID, map[string]any{
			"results": []map[string]any{{"path": "프로젝트/영산고/대표.md", "title": "영산고"}},
		})
	})

	rec := postMCP(t, s, token,
		`{"jsonrpc":"2.0","id":"c1","method":"tools/call","params":{"name":"wiki_search","arguments":{"query":"영산고","limit":3}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(seenParams, `"query":"영산고"`) {
		t.Errorf("arguments not passed through as RPC params: %s", seenParams)
	}
	out := decodeMCP(t, rec)
	result, _ := out["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content = %v", result)
	}
	block, _ := content[0].(map[string]any)
	if block["type"] != "text" || !strings.Contains(block["text"].(string), "프로젝트/영산고/대표.md") {
		t.Errorf("text block = %v", block)
	}
	if result["isError"] != nil {
		t.Errorf("unexpected isError: %v", result["isError"])
	}
}

func TestMCP_ToolCallErrorsSurfaceInResult(t *testing.T) {
	token := withClientToken(t)
	s := newTestServer(t)

	s.dispatcher.Register("miniapp.memory.get_page", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return &protocol.ResponseFrame{
			Type: protocol.FrameTypeResponse, ID: req.ID, OK: false,
			Error: &protocol.ErrorShape{Code: "NOT_FOUND", Message: "no such page"},
		}
	})

	rec := postMCP(t, s, token,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"wiki_read","arguments":{"path":"없는/문서.md"}}}`)
	out := decodeMCP(t, rec)
	result, _ := out["result"].(map[string]any)
	if result == nil || result["isError"] != true {
		t.Fatalf("want isError result, got %v", out)
	}
	content := result["content"].([]any)[0].(map[string]any)
	if !strings.Contains(content["text"].(string), "NOT_FOUND") {
		t.Errorf("error text = %v", content["text"])
	}

	// Unknown tool → JSON-RPC invalid-params error (protocol level).
	rec = postMCP(t, s, token, `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"rm_rf","arguments":{}}}`)
	out = decodeMCP(t, rec)
	errObj, _ := out["error"].(map[string]any)
	if errObj == nil || errObj["code"].(float64) != -32602 {
		t.Errorf("unknown tool error = %v", out)
	}
}

func TestMCPTransportRejectsInvalidRequestsPerJSONRPCRules(t *testing.T) {
	token := withClientToken(t)
	s := newTestServer(t)

	// Missing token → 401.
	if rec := postMCP(t, s, "", `{"jsonrpc":"2.0","id":1,"method":"ping"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("no-token status = %d", rec.Code)
	}
	// Notification (no id) → 202, no body.
	if rec := postMCP(t, s, token, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); rec.Code != http.StatusAccepted {
		t.Errorf("notification status = %d", rec.Code)
	}
	// Batch → protocol error.
	rec := postMCP(t, s, token, `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`)
	if errObj, _ := decodeMCP(t, rec)["error"].(map[string]any); errObj == nil || errObj["code"].(float64) != -32600 {
		t.Errorf("batch response = %s", rec.Body.String())
	}
	// Unknown method → -32601.
	rec = postMCP(t, s, token, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`)
	if errObj, _ := decodeMCP(t, rec)["error"].(map[string]any); errObj == nil || errObj["code"].(float64) != -32601 {
		t.Errorf("unknown method response = %s", rec.Body.String())
	}
	// GET (SSE stream) unsupported → 405.
	getReq := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/mcp", nil)
	getReq.Header.Set(clientauth.Header, token)
	getRec := httptest.NewRecorder()
	mcpHandler(s).ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d", getRec.Code)
	}
	// Browser Origin → 403 (DNS-rebinding hardening; no browser use-case).
	origReq := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/mcp",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)))
	origReq.Header.Set(clientauth.Header, token)
	origReq.Header.Set("Origin", "https://evil.example")
	origRec := httptest.NewRecorder()
	mcpHandler(s).ServeHTTP(origRec, origReq)
	if origRec.Code != http.StatusForbidden {
		t.Errorf("Origin status = %d", origRec.Code)
	}
	// ping round-trips.
	rec = postMCP(t, s, token, `{"jsonrpc":"2.0","id":"p","method":"ping"}`)
	if out := decodeMCP(t, rec); out["result"] == nil {
		t.Errorf("ping response = %s", rec.Body.String())
	}
}

// TestMCP_ToolTableIsReadOnly guards the allowlist: every mapped method must be
// a read verb — a write method appearing here is a security regression, not a
// mapping tweak. Verbs match on word boundaries (the "."/"_" segments of the
// method name), not raw substrings: substring matching false-positives on
// legitimate read methods that merely CONTAIN a verb (e.g. "unarchived",
// "bookmarks"). A segment-prefix match still catches derived forms
// ("creates", "marked") — over-flagging is fine for a security tripwire.
func TestMCP_ToolTableIsReadOnly(t *testing.T) {
	writeVerbs := []string{"write", "create", "update", "delete", "move", "merge", "send", "accept", "reject", "mark", "archive", "trash", "close", "reopen", "analyze", "ask"}
	for _, tool := range mcpapi.ToolDefinitions() {
		if !strings.HasPrefix(tool.Method, "miniapp.") {
			t.Errorf("%s maps outside the miniapp surface: %s", tool.Name, tool.Method)
		}
		segments := strings.FieldsFunc(strings.ToLower(tool.Method), func(r rune) bool {
			return r == '.' || r == '_' || r == '-'
		})
		for _, seg := range segments {
			for _, verb := range writeVerbs {
				if strings.HasPrefix(seg, verb) {
					t.Errorf("%s maps to a write-ish method %s (segment %q matches verb %q)", tool.Name, tool.Method, seg, verb)
				}
			}
		}
	}
}

// TestMCP_RejectsMalformedEnvelopes pins the JSON-RPC 2.0 envelope validation:
// a wrong/missing jsonrpc version and object/array ids are invalid requests
// (id echoed as null — an invalid id must not be echoed back).
func TestMCP_RejectsMalformedEnvelopes(t *testing.T) {
	token := withClientToken(t)
	s := newTestServer(t)

	cases := []struct {
		name string
		body string
	}{
		{"missing jsonrpc", `{"id":1,"method":"ping"}`},
		{"wrong jsonrpc", `{"jsonrpc":"1.0","id":1,"method":"ping"}`},
		{"object id", `{"jsonrpc":"2.0","id":{"a":1},"method":"ping"}`},
		{"array id", `{"jsonrpc":"2.0","id":[1],"method":"ping"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := decodeMCP(t, postMCP(t, s, token, tc.body))
			errObj, _ := out["error"].(map[string]any)
			if errObj == nil {
				t.Fatalf("expected a JSON-RPC error, got: %v", out)
			}
			if code, _ := errObj["code"].(float64); code != -32600 {
				t.Errorf("code = %v, want -32600 (invalid request)", errObj["code"])
			}
			if out["id"] != nil {
				t.Errorf("id = %v, want null (invalid/unusable request id)", out["id"])
			}
		})
	}
}

// TestMCPInternalID pins the internal dispatch-id derivation: type-prefixed
// (numeric 1 vs string "1" must not collide) and length-capped (a
// client-chosen id can't inflate internal ids).
func TestMCPInternalIDAvoidsCollisionAndTruncatesLongIDs(t *testing.T) {
	num := mcpapi.InternalID(json.RawMessage(`1`))
	str := mcpapi.InternalID(json.RawMessage(`"1"`))
	if num == str {
		t.Errorf("numeric 1 and string \"1\" collide: %q", num)
	}
	long := mcpapi.InternalID(json.RawMessage(`"` + strings.Repeat("x", 500) + `"`))
	if n := len([]rune(long)); n > 66 { // prefix + 64-rune cap
		t.Errorf("derived id length = %d, want capped", n)
	}
}
