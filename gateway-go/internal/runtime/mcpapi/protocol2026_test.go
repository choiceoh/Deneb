package mcpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// statelessMeta is the `_meta` block a conforming 2026-07-28 client attaches
// to every request.
const statelessMeta = `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28",` +
	`"io.modelcontextprotocol/clientInfo":{"name":"probe","version":"1.0"},` +
	`"io.modelcontextprotocol/clientCapabilities":{}}`

// postStateless issues one 2026-07-28 request. headers overrides or removes
// (empty value) the standard mirrors, so a test can express exactly one
// deviation from a conforming request.
func postStateless(t *testing.T, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var msg struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		t.Fatalf("test body is not JSON: %v", err)
	}
	std := map[string]string{
		headerProtocolVersion: protocolVersion2026,
		headerMethod:          msg.Method,
	}
	if msg.Params.Name != "" {
		std[headerName] = msg.Params.Name
	}
	for k, v := range headers {
		std[k] = v
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range std {
		if v == "" {
			continue // empty means "omit this header"
		}
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	configuredMCPHandler().ServeHTTP(rec, req)
	return rec
}

func decodeFrame(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return out
}

func resultOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	out := decodeFrame(t, rec)
	result, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", out)
	}
	return result
}

// errorOf asserts the frame is a JSON-RPC error with the given HTTP status and
// code, and returns the error object.
func errorOf(t *testing.T, rec *httptest.ResponseRecorder, wantStatus, wantCode int) map[string]any {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, wantStatus, rec.Body.String())
	}
	out := decodeFrame(t, rec)
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error in %v", out)
	}
	if code, _ := errObj["code"].(float64); int(code) != wantCode {
		t.Fatalf("error code = %v, want %d (%v)", errObj["code"], wantCode, errObj)
	}
	return errObj
}

func TestServerDiscoverAdvertisesStatelessRevision(t *testing.T) {
	t.Parallel()
	rec := postStateless(t, `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{`+statelessMeta+`}}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	result := resultOf(t, rec)

	versions, _ := result["supportedVersions"].([]any)
	if len(versions) == 0 || versions[0] != protocolVersion2026 {
		t.Errorf("supportedVersions = %v, want %s first", result["supportedVersions"], protocolVersion2026)
	}
	if result["resultType"] != resultTypeComplete {
		t.Errorf("resultType = %v, want %q", result["resultType"], resultTypeComplete)
	}
	if result["cacheScope"] != listCacheScope {
		t.Errorf("cacheScope = %v, want %q", result["cacheScope"], listCacheScope)
	}
	if ttl, _ := result["ttlMs"].(float64); int(ttl) != listCacheTTLMs {
		t.Errorf("ttlMs = %v, want %d", result["ttlMs"], listCacheTTLMs)
	}
	caps, _ := result["capabilities"].(map[string]any)
	if caps == nil || caps["tools"] == nil {
		t.Errorf("capabilities = %v, want tools", result["capabilities"])
	}
	meta, _ := result["_meta"].(map[string]any)
	info, _ := meta[metaServerInfo].(map[string]any)
	if info == nil || info["name"] != "deneb" {
		t.Errorf("_meta serverInfo = %v", result["_meta"])
	}
}

// server/discover is the probe a client uses before it knows what the server
// speaks, so it must answer even without the stateless request metadata.
func TestServerDiscoverAnswersBareProbe(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`))
	rec := httptest.NewRecorder()
	configuredMCPHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	// Answered in 2026-07-28 shape regardless: the method exists only there.
	if result := resultOf(t, rec); result["resultType"] != resultTypeComplete {
		t.Errorf("resultType = %v, want %q", result["resultType"], resultTypeComplete)
	}
}

func TestStatelessToolsListCarriesCacheHintsAndResultType(t *testing.T) {
	t.Parallel()
	rec := postStateless(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{`+statelessMeta+`}}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	result := resultOf(t, rec)

	if tools, _ := result["tools"].([]any); len(tools) != len(ToolDefinitions()) {
		t.Errorf("tools = %d, want %d", len(tools), len(ToolDefinitions()))
	}
	if result["resultType"] != resultTypeComplete {
		t.Errorf("resultType = %v", result["resultType"])
	}
	if ttl, _ := result["ttlMs"].(float64); int(ttl) != listCacheTTLMs {
		t.Errorf("ttlMs = %v, want %d", result["ttlMs"], listCacheTTLMs)
	}
	if result["cacheScope"] != listCacheScope {
		t.Errorf("cacheScope = %v, want %q", result["cacheScope"], listCacheScope)
	}
}

// A handshake-era client must see byte-identical results to before the
// stateless revision landed: no resultType, no cache hints, no _meta.
func TestHandshakeEraResultsStayUndecorated(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set(headerProtocolVersion, handshakeProtocolVersion)
	rec := httptest.NewRecorder()
	configuredMCPHandler().ServeHTTP(rec, req)

	result := resultOf(t, rec)
	for _, field := range []string{"resultType", "ttlMs", "cacheScope", "_meta"} {
		if _, present := result[field]; present {
			t.Errorf("handshake-era result leaked %q: %v", field, result)
		}
	}
}

func TestStatelessHeaderValidationBoundaryMatrix(t *testing.T) {
	t.Parallel()
	const toolsList = `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{` + statelessMeta + `}}`
	const toolsCall = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wiki_search",` +
		`"arguments":{"query":"x"},` + statelessMeta + `}}`

	tests := []struct {
		name     string
		body     string
		headers  map[string]string
		wantCode int
		wantMsg  string
	}{
		{
			name:     "protocol version header missing",
			body:     toolsList,
			headers:  map[string]string{headerProtocolVersion: ""},
			wantCode: codeHeaderMismatch,
			wantMsg:  "missing required header: MCP-Protocol-Version",
		},
		{
			name:     "protocol version header disagrees with meta",
			body:     toolsList,
			headers:  map[string]string{headerProtocolVersion: "2025-06-18"},
			wantCode: codeHeaderMismatch,
			wantMsg:  "does not match body _meta",
		},
		{
			name: "protocol version meta missing",
			body: `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
			headers: map[string]string{
				headerProtocolVersion: protocolVersion2026,
				headerMethod:          "tools/list",
			},
			wantCode: codeHeaderMismatch,
			wantMsg:  "missing required request field",
		},
		{
			name:     "method header missing",
			body:     toolsList,
			headers:  map[string]string{headerMethod: ""},
			wantCode: codeHeaderMismatch,
			wantMsg:  "missing required header: Mcp-Method",
		},
		{
			name:     "method header disagrees with body",
			body:     toolsList,
			headers:  map[string]string{headerMethod: "tools/call"},
			wantCode: codeHeaderMismatch,
			wantMsg:  "does not match body method",
		},
		{
			name:     "name header missing on tools/call",
			body:     toolsCall,
			headers:  map[string]string{headerName: ""},
			wantCode: codeHeaderMismatch,
			wantMsg:  "missing required header: Mcp-Name",
		},
		{
			name:     "name header disagrees with body",
			body:     toolsCall,
			headers:  map[string]string{headerName: "search_all"},
			wantCode: codeHeaderMismatch,
			wantMsg:  "does not match body params.name",
		},
		{
			name:     "name header malformed base64 sentinel",
			body:     toolsCall,
			headers:  map[string]string{headerName: "=?base64?not-base64!?="},
			wantCode: codeHeaderMismatch,
			wantMsg:  "invalid Mcp-Name header",
		},
		{
			name: "unsupported protocol version",
			body: `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":` +
				`{"io.modelcontextprotocol/protocolVersion":"2025-09-01"}}}`,
			headers: map[string]string{
				headerProtocolVersion: "2025-09-01",
				headerMethod:          "tools/list",
			},
			wantCode: codeUnsupportedProtocolVersion,
			wantMsg:  "unsupported MCP protocol version",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := postStateless(t, tc.body, tc.headers)
			errObj := errorOf(t, rec, http.StatusBadRequest, tc.wantCode)
			if msg, _ := errObj["message"].(string); !strings.Contains(msg, tc.wantMsg) {
				t.Errorf("message = %q, want it to contain %q", msg, tc.wantMsg)
			}
		})
	}
}

// The unsupported-version error must tell the client what to retry with —
// that data field is how a 2.0 client renegotiates instead of giving up.
func TestUnsupportedVersionErrorListsSupportedRevisions(t *testing.T) {
	t.Parallel()
	rec := postStateless(t,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":`+
			`{"io.modelcontextprotocol/protocolVersion":"2099-01-01"}}}`,
		map[string]string{headerProtocolVersion: "2099-01-01", headerMethod: "tools/list"})

	errObj := errorOf(t, rec, http.StatusBadRequest, codeUnsupportedProtocolVersion)
	data, _ := errObj["data"].(map[string]any)
	if data == nil || data["requested"] != "2099-01-01" {
		t.Fatalf("data = %v, want requested 2099-01-01", errObj["data"])
	}
	supported, _ := data["supported"].([]any)
	if len(supported) != len(ProtocolVersions()) {
		t.Errorf("supported = %v, want %v", data["supported"], ProtocolVersions())
	}
}

// A Base64-sentinel Mcp-Name that decodes to the body value passes validation
// and reaches dispatch — proven by the tool error coming from dispatch rather
// than a header mismatch.
func TestBase64SentinelNameHeaderIsDecodedBeforeComparison(t *testing.T) {
	t.Parallel()
	encoded := headerB64Prefix + base64.StdEncoding.EncodeToString([]byte("wiki_search")) + headerB64Suffix
	rec := postStateless(t,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"wiki_search",`+
			`"arguments":{"query":"x"},`+statelessMeta+`}}`,
		map[string]string{headerName: encoded})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	// The bare test dispatcher has no miniapp methods, so the call surfaces as
	// an in-result tool error — the point is that it got that far.
	result := resultOf(t, rec)
	if result["isError"] != true {
		t.Errorf("want a dispatched tool error, got %v", result)
	}
	if result["resultType"] != resultTypeComplete {
		t.Errorf("tool error result missing resultType: %v", result)
	}
}

// Methods the stateless revision removed. 404 (not 200) is mandated so a
// client can tell a modern server missing a method from a legacy endpoint.
func TestStatelessRemovedMethodsAnswer404(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"initialize", "ping", "logging/setLevel", "resources/list"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			rec := postStateless(t,
				`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":{`+statelessMeta+`}}`, nil)
			errObj := errorOf(t, rec, http.StatusNotFound, -32601)
			if msg, _ := errObj["message"].(string); !strings.Contains(msg, method) {
				t.Errorf("message = %q, want it to name %q", msg, method)
			}
		})
	}
}

// The same methods keep working for handshake-era clients — adopting 2.0 must
// not break the clients still on 1.x.
func TestHandshakeEraKeepsInitializeAndPing(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"initialize", "ping"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/mcp",
				strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"`+method+`"}`))
			rec := httptest.NewRecorder()
			configuredMCPHandler().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
			}
			if _, ok := decodeFrame(t, rec)["result"]; !ok {
				t.Errorf("%s did not answer with a result: %s", method, rec.Body.String())
			}
		})
	}
}

func TestHandshakeEraUnknownMethodStillAnswers200(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"resources/list"}`))
	rec := httptest.NewRecorder()
	configuredMCPHandler().ServeHTTP(rec, req)

	errorOf(t, rec, http.StatusOK, -32601)
}

func TestDecodeHeaderValueBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "plain ascii", raw: "wiki_search", want: "wiki_search"},
		{name: "empty", raw: "", want: ""},
		{
			name: "sentinel korean",
			raw:  headerB64Prefix + base64.StdEncoding.EncodeToString([]byte("위키검색")) + headerB64Suffix,
			want: "위키검색",
		},
		{name: "sentinel empty payload", raw: "=?base64??=", want: ""},
		{name: "malformed payload", raw: "=?base64?@@@?=", wantErr: true},
		// Shorter than prefix+suffix: prefix and suffix would overlap, so this
		// is not a sentinel and must pass through rather than slice out of range.
		{name: "overlapping prefix and suffix", raw: "=?base64?=", want: "=?base64?="},
		{name: "prefix only", raw: "=?base64?", want: "=?base64?"},
		{name: "suffix only", raw: "?=", want: "?="},
		{name: "uppercase prefix is not the sentinel", raw: "=?BASE64?aGk=?=", want: "=?BASE64?aGk=?="},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeHeaderValue(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("decodeHeaderValue(%q) = %q, want error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeHeaderValue(%q): %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("decodeHeaderValue(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestRequestMetaVersionBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		params string
		want   string
	}{
		{name: "absent params", params: "", want: ""},
		{name: "empty object", params: `{}`, want: ""},
		{name: "meta without version", params: `{"_meta":{}}`, want: ""},
		{name: "version present", params: `{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}`, want: "2026-07-28"},
		{name: "version wrong type", params: `{"_meta":{"io.modelcontextprotocol/protocolVersion":7}}`, want: ""},
		{name: "meta wrong type", params: `{"_meta":"nope"}`, want: ""},
		{name: "malformed json", params: `{"_meta":`, want: ""},
		{name: "params is an array", params: `[1,2]`, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := requestMetaVersion(json.RawMessage(tc.params)); got != tc.want {
				t.Errorf("requestMetaVersion(%q) = %q, want %q", tc.params, got, tc.want)
			}
		})
	}
}
