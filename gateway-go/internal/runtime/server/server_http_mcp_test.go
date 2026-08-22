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

// postMCPRaw sends a body verbatim, with no protocol metadata. Transport-level
// tests use it: a malformed envelope or a missing token is rejected before the
// version gate ever runs, and a legacy-shaped request is the thing under test.
func postMCPRaw(t *testing.T, s *Server, token, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/mcp", bytes.NewReader([]byte(body)))
	if token != "" {
		req.Header.Set(clientauth.Header, token)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mcpHandler(s).ServeHTTP(rec, req)
	return rec
}

// postMCP issues one CONFORMING 2026-07-28 request. The endpoint serves no
// other revision, so the harness attaches the version markers and mirrored
// headers that every request must carry — otherwise each test would restate
// the same boilerplate.
func postMCP(t *testing.T, s *Server, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var msg map[string]any
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		t.Fatalf("test body is not JSON: %v", err)
	}
	method, _ := msg["method"].(string)
	params, _ := msg["params"].(map[string]any)
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    mcp2026,
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	msg["params"] = params
	encoded, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("re-encode test body: %v", err)
	}
	headers := map[string]string{"MCP-Protocol-Version": mcp2026, "Mcp-Method": method}
	if name, _ := params["name"].(string); name != "" {
		headers["Mcp-Name"] = name
	}
	return postMCPRaw(t, s, token, string(encoded), headers)
}

// mcp2026 is the only revision this endpoint serves.
const mcp2026 = "2026-07-28"

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

// The endpoint serves 2026-07-28 and nothing else. A handshake-era client is
// turned away with a message naming what we speak, and `server/discover` stays
// reachable without any 2.0 metadata so that client can discover it.
func TestMCPServesOnlyTheStatelessRevision(t *testing.T) {
	token := withClientToken(t)
	s := newTestServer(t)

	// A bare probe — no headers, no `_meta` — still answers.
	rec := postMCPRaw(t, s, token, `{"jsonrpc":"2.0","id":1,"method":"server/discover"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("bare discover status = %d, body %s", rec.Code, rec.Body.String())
	}
	result, _ := decodeMCP(t, rec)["result"].(map[string]any)
	versions, _ := result["supportedVersions"].([]any)
	if len(versions) != 1 || versions[0] != mcp2026 {
		t.Errorf("supportedVersions = %v, want exactly [%s]", result["supportedVersions"], mcp2026)
	}

	// The handshake itself is gone: initialize is just an unknown method now,
	// but the version gate rejects the request before it gets that far.
	rec = postMCPRaw(t, s, token,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("legacy initialize status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	errObj, _ := decodeMCP(t, rec)["error"].(map[string]any)
	if errObj == nil || errObj["code"].(float64) != -32020 {
		t.Fatalf("legacy initialize error = %v, want -32020", errObj)
	}
	if msg, _ := errObj["message"].(string); !strings.Contains(msg, mcp2026) {
		t.Errorf("rejection %q should name the revision we speak", msg)
	}

	// A client that DOES declare a version we dropped gets the supported list
	// back, which is what lets it renegotiate rather than guess.
	rec = postMCPRaw(t, s, token,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2025-11-25"}}}`,
		map[string]string{"MCP-Protocol-Version": "2025-11-25", "Mcp-Method": "tools/list"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("dropped-revision status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	errObj, _ = decodeMCP(t, rec)["error"].(map[string]any)
	if errObj == nil || errObj["code"].(float64) != -32022 {
		t.Fatalf("dropped-revision error = %v, want -32022", errObj)
	}
	data, _ := errObj["data"].(map[string]any)
	supported, _ := data["supported"].([]any)
	if len(supported) != 1 || supported[0] != mcp2026 {
		t.Errorf("supported = %v, want [%s]", data["supported"], mcp2026)
	}

	// And a conforming 2.0 request is served normally.
	rec = postMCP(t, s, token, `{"jsonrpc":"2.0","id":4,"method":"tools/list"}`)
	out := decodeMCP(t, rec)
	result, _ = out["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if want := len(mcpapi.ToolDefinitions()); len(tools) != want {
		t.Fatalf("tools = %d, want %d", len(tools), want)
	}
	if first, _ := tools[0].(map[string]any); first["name"] != "wiki_search" {
		t.Errorf("first tool = %v", first["name"])
	}
	if result["resultType"] != "complete" {
		t.Errorf("resultType = %v, want complete", result["resultType"])
	}
	if result["ttlMs"] == nil || result["cacheScope"] != "private" {
		t.Errorf("missing cache hints: %v", result)
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

	// Missing token → 401, before anything else is looked at.
	if rec := postMCPRaw(t, s, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no-token status = %d", rec.Code)
	}
	// Notification (no id) → 202, no body. Header rules are undefined for
	// notification POSTs in this revision, so it is accepted bare.
	if rec := postMCPRaw(t, s, token, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil); rec.Code != http.StatusAccepted {
		t.Errorf("notification status = %d", rec.Code)
	}
	// Batch → protocol error, caught before the version gate.
	rec := postMCPRaw(t, s, token, `[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`, nil)
	if errObj, _ := decodeMCP(t, rec)["error"].(map[string]any); errObj == nil || errObj["code"].(float64) != -32600 {
		t.Errorf("batch response = %s", rec.Body.String())
	}
	// Unknown method → -32601 AND 404: the revision pins the status so a client
	// can tell a missing method from a legacy server missing the endpoint.
	rec = postMCP(t, s, token, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown method status = %d, want 404", rec.Code)
	}
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
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	origReq.Header.Set(clientauth.Header, token)
	origReq.Header.Set("Origin", "https://evil.example")
	origRec := httptest.NewRecorder()
	mcpHandler(s).ServeHTTP(origRec, origReq)
	if origRec.Code != http.StatusForbidden {
		t.Errorf("Origin status = %d", origRec.Code)
	}
	// ping was removed in this revision — a POST that returns is liveness
	// enough — so it is now an unknown method like any other.
	rec = postMCP(t, s, token, `{"jsonrpc":"2.0","id":"p","method":"ping"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("ping status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
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
		{"missing jsonrpc", `{"id":1,"method":"tools/list"}`},
		{"wrong jsonrpc", `{"jsonrpc":"1.0","id":1,"method":"tools/list"}`},
		{"object id", `{"jsonrpc":"2.0","id":{"a":1},"method":"tools/list"}`},
		{"array id", `{"jsonrpc":"2.0","id":[1],"method":"tools/list"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := decodeMCP(t, postMCPRaw(t, s, token, tc.body, nil))
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
