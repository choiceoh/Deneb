package mcpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc"
)

func configuredMCPHandler() *Handler {
	return New(Config{
		Authenticate: func(http.ResponseWriter, *http.Request) (*clientauth.Identity, bool) {
			return &clientauth.Identity{User: &clientauth.User{ID: 1}}, true
		},
		Dispatcher: &rpc.Dispatcher{},
		Version:    "test-version",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func TestInternalIDBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "zero",
			raw:  "0",
			want: "n0",
		},
		{
			name: "negative",
			raw:  "-1",
			want: "n-1",
		},
		{
			name: "positive",
			raw:  "42",
			want: "n42",
		},
		{
			name: "decimal",
			raw:  "1.25",
			want: "n1.25",
		},
		{
			name: "exponent",
			raw:  "1e9",
			want: "n1e9",
		},
		{
			name: "uppercase exponent",
			raw:  "2E3",
			want: "n2E3",
		},
		{
			name: "negative exponent",
			raw:  "3e-2",
			want: "n3e-2",
		},
		{
			name: "string zero",
			raw:  "\"0\"",
			want: "s0",
		},
		{
			name: "string one",
			raw:  "\"1\"",
			want: "s1",
		},
		{
			name: "empty string",
			raw:  "\"\"",
			want: "s",
		},
		{
			name: "ascii",
			raw:  "\"alpha\"",
			want: "salpha",
		},
		{
			name: "hyphen",
			raw:  "\"alpha-beta\"",
			want: "salpha-beta",
		},
		{
			name: "underscore",
			raw:  "\"alpha_beta\"",
			want: "salpha_beta",
		},
		{
			name: "colon",
			raw:  "\"alpha:beta\"",
			want: "salpha:beta",
		},
		{
			name: "slash",
			raw:  "\"alpha/beta\"",
			want: "salpha/beta",
		},
		{
			name: "space",
			raw:  "\"alpha beta\"",
			want: "salpha beta",
		},
		{
			name: "leading space",
			raw:  "\" alpha\"",
			want: "s alpha",
		},
		{
			name: "trailing space",
			raw:  "\"alpha \"",
			want: "salpha ",
		},
		{
			name: "korean",
			raw:  "\"주간보고\"",
			want: "s주간보고",
		},
		{
			name: "japanese",
			raw:  "\"こんにちは\"",
			want: "sこんにちは",
		},
		{
			name: "emoji",
			raw:  "\"🚀\"",
			want: "s🚀",
		},
		{
			name: "combining",
			raw:  "\"é\"",
			want: "sé",
		},
		{
			name: "punctuation",
			raw:  "\"!@#$%^&*()\"",
			want: "s!@#$%^&*()",
		},
		{
			name: "dots",
			raw:  "\"a.b.c\"",
			want: "sa.b.c",
		},
		{
			name: "plus",
			raw:  "\"a+b\"",
			want: "sa+b",
		},
		{
			name: "equals",
			raw:  "\"a=b\"",
			want: "sa=b",
		},
		{
			name: "numeric-looking string",
			raw:  "\"001\"",
			want: "s001",
		},
		{
			name: "negative-looking string",
			raw:  "\"-1\"",
			want: "s-1",
		},
		{
			name: "null token",
			raw:  "null",
			want: "nnull",
		},
		{
			name: "boolean token",
			raw:  "true",
			want: "ntrue",
		},
		{
			name: "escaped quote",
			raw:  "\"a\\\"b\"",
			want: "sa\\\"b",
		},
		{
			name: "escaped slash",
			raw:  "\"a\\/b\"",
			want: "sa\\/b",
		},
		{
			name: "escaped newline",
			raw:  "\"a\\nb\"",
			want: "sa\\nb",
		},
		{
			name: "escaped tab",
			raw:  "\"a\\tb\"",
			want: "sa\\tb",
		},
		{
			name: "escaped unicode",
			raw:  "\"\\uac00\"",
			want: "s\\uac00",
		},
		{
			name: "max int",
			raw:  "9223372036854775807",
			want: "n9223372036854775807",
		},
		{
			name: "min int",
			raw:  "-9223372036854775808",
			want: "n-9223372036854775808",
		},
		{
			name: "large decimal",
			raw:  "123456789012345678901234567890",
			want: "n123456789012345678901234567890",
		},
		{
			name: "fraction",
			raw:  "0.0000000001",
			want: "n0.0000000001",
		},
		{
			name: "negative fraction",
			raw:  "-0.0000000001",
			want: "n-0.0000000001",
		},
		{
			name: "string brackets",
			raw:  "\"[id]\"",
			want: "s[id]",
		},
		{
			name: "string braces",
			raw:  "\"{id}\"",
			want: "s{id}",
		},
		{
			name: "string null",
			raw:  "\"null\"",
			want: "snull",
		},
		{
			name: "string true",
			raw:  "\"true\"",
			want: "strue",
		},
		{
			name: "sixty four ascii",
			raw:  `"` + strings.Repeat("a", 64) + `"`,
			want: "s" + strings.Repeat("a", 64),
		},
		{
			name: "sixty five ascii",
			raw:  `"` + strings.Repeat("b", 65) + `"`,
			want: "s" + strings.Repeat("b", 64),
		},
		{
			name: "sixty five korean",
			raw:  `"` + strings.Repeat("나", 65) + `"`,
			want: "s" + strings.Repeat("나", 64),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := InternalID(json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("InternalID(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestNegotiateMCPVersionBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		params string
		want   string
	}{
		{
			name:   "absent",
			params: "",
			want:   "2025-06-18",
		},
		{
			name:   "null",
			params: "null",
			want:   "2025-06-18",
		},
		{
			name:   "empty object",
			params: "{}",
			want:   "2025-06-18",
		},
		{
			name:   "newest",
			params: "{\"protocolVersion\":\"2025-06-18\"}",
			want:   "2025-06-18",
		},
		{
			name:   "middle",
			params: "{\"protocolVersion\":\"2025-03-26\"}",
			want:   "2025-03-26",
		},
		{
			name:   "oldest",
			params: "{\"protocolVersion\":\"2024-11-05\"}",
			want:   "2024-11-05",
		},
		{
			name:   "future",
			params: "{\"protocolVersion\":\"2099-01-01\"}",
			want:   "2025-06-18",
		},
		{
			name:   "old unknown",
			params: "{\"protocolVersion\":\"2020-01-01\"}",
			want:   "2025-06-18",
		},
		{
			name:   "empty version",
			params: "{\"protocolVersion\":\"\"}",
			want:   "2025-06-18",
		},
		{
			name:   "space version",
			params: "{\"protocolVersion\":\" 2025-06-18 \"}",
			want:   "2025-06-18",
		},
		{
			name:   "case changed",
			params: "{\"protocolVersion\":\"2025-06-18A\"}",
			want:   "2025-06-18",
		},
		{
			name:   "numeric version",
			params: "{\"protocolVersion\":20250618}",
			want:   "2025-06-18",
		},
		{
			name:   "boolean version",
			params: "{\"protocolVersion\":true}",
			want:   "2025-06-18",
		},
		{
			name:   "array version",
			params: "{\"protocolVersion\":[]}",
			want:   "2025-06-18",
		},
		{
			name:   "object version",
			params: "{\"protocolVersion\":{}}",
			want:   "2025-06-18",
		},
		{
			name:   "malformed open",
			params: "{",
			want:   "2025-06-18",
		},
		{
			name:   "malformed string",
			params: "{\"protocolVersion\":",
			want:   "2025-06-18",
		},
		{
			name:   "array params",
			params: "[]",
			want:   "2025-06-18",
		},
		{
			name:   "string params",
			params: "\"2025-03-26\"",
			want:   "2025-06-18",
		},
		{
			name:   "extra fields",
			params: "{\"x\":1,\"protocolVersion\":\"2025-03-26\",\"y\":2}",
			want:   "2025-03-26",
		},
		{
			name:   "duplicate supported last",
			params: "{\"protocolVersion\":\"bad\",\"protocolVersion\":\"2024-11-05\"}",
			want:   "2024-11-05",
		},
		{
			name:   "duplicate unsupported last",
			params: "{\"protocolVersion\":\"2024-11-05\",\"protocolVersion\":\"bad\"}",
			want:   "2025-06-18",
		},
		{
			name:   "unicode field",
			params: "{\"프로토콜\":\"2025-03-26\"}",
			want:   "2025-06-18",
		},
		{
			name:   "leading whitespace",
			params: "  {\"protocolVersion\":\"2024-11-05\"}",
			want:   "2024-11-05",
		},
		{
			name:   "trailing whitespace",
			params: "{\"protocolVersion\":\"2025-03-26\"}  ",
			want:   "2025-03-26",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := negotiateMCPVersion(json.RawMessage(tc.params)); got != tc.want {
				t.Fatalf("negotiateMCPVersion(%q) = %q, want %q", tc.params, got, tc.want)
			}
		})
	}
}

func TestMCPHandlerRequestBoundaryMatrix(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		body       string
		origin     string
		mode       string
		wantStatus int
		wantBody   string
		wantAllow  string
	}{
		{
			name:       "get unsupported",
			method:     "GET",
			body:       "",
			origin:     "",
			mode:       "ok",
			wantStatus: 405,
			wantBody:   "stateless MCP server",
			wantAllow:  "POST",
		},
		{
			name:       "delete unsupported",
			method:     "DELETE",
			body:       "",
			origin:     "",
			mode:       "ok",
			wantStatus: 405,
			wantBody:   "stateless MCP server",
			wantAllow:  "POST",
		},
		{
			name:       "put unsupported",
			method:     "PUT",
			body:       "",
			origin:     "",
			mode:       "ok",
			wantStatus: 405,
			wantBody:   "method not allowed",
			wantAllow:  "POST",
		},
		{
			name:       "patch unsupported",
			method:     "PATCH",
			body:       "",
			origin:     "",
			mode:       "ok",
			wantStatus: 405,
			wantBody:   "method not allowed",
			wantAllow:  "POST",
		},
		{
			name:       "head unsupported",
			method:     "HEAD",
			body:       "",
			origin:     "",
			mode:       "ok",
			wantStatus: 405,
			wantBody:   "",
			wantAllow:  "POST",
		},
		{
			name:       "options unsupported",
			method:     "OPTIONS",
			body:       "",
			origin:     "",
			mode:       "ok",
			wantStatus: 405,
			wantBody:   "method not allowed",
			wantAllow:  "POST",
		},
		{
			name:       "browser http origin",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}",
			origin:     "http://example.com",
			mode:       "ok",
			wantStatus: 403,
			wantBody:   "browser origins not allowed",
			wantAllow:  "",
		},
		{
			name:       "browser null origin",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}",
			origin:     "null",
			mode:       "ok",
			wantStatus: 403,
			wantBody:   "browser origins not allowed",
			wantAllow:  "",
		},
		{
			name:       "missing auth callback",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}",
			origin:     "",
			mode:       "nil-auth",
			wantStatus: 503,
			wantBody:   "not configured",
			wantAllow:  "",
		},
		{
			name:       "missing dispatcher",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}",
			origin:     "",
			mode:       "nil-dispatcher",
			wantStatus: 503,
			wantBody:   "not configured",
			wantAllow:  "",
		},
		{
			name:       "authentication denied",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}",
			origin:     "",
			mode:       "deny",
			wantStatus: 401,
			wantBody:   "denied",
			wantAllow:  "",
		},
		{
			name:       "empty body",
			method:     "POST",
			body:       "",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "-32700",
			wantAllow:  "",
		},
		{
			name:       "whitespace body",
			method:     "POST",
			body:       " \n\t ",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "-32700",
			wantAllow:  "",
		},
		{
			name:       "malformed object",
			method:     "POST",
			body:       "{",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "-32700",
			wantAllow:  "",
		},
		{
			name:       "plain text",
			method:     "POST",
			body:       "hello",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "-32700",
			wantAllow:  "",
		},
		{
			name:       "json string",
			method:     "POST",
			body:       "\"hello\"",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "-32700",
			wantAllow:  "",
		},
		{
			name:       "json number",
			method:     "POST",
			body:       "1",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "-32700",
			wantAllow:  "",
		},
		{
			name:       "json boolean",
			method:     "POST",
			body:       "true",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "-32700",
			wantAllow:  "",
		},
		{
			name:       "batch empty",
			method:     "POST",
			body:       "[]",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "batching is not supported",
			wantAllow:  "",
		},
		{
			name:       "batch whitespace",
			method:     "POST",
			body:       "  []",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "batching is not supported",
			wantAllow:  "",
		},
		{
			name:       "batch values",
			method:     "POST",
			body:       "[{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}]",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "batching is not supported",
			wantAllow:  "",
		},
		{
			name:       "missing jsonrpc",
			method:     "POST",
			body:       "{\"id\":1,\"method\":\"ping\"}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "jsonrpc must be",
			wantAllow:  "",
		},
		{
			name:       "wrong jsonrpc",
			method:     "POST",
			body:       "{\"jsonrpc\":\"1.0\",\"id\":1,\"method\":\"ping\"}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "jsonrpc must be",
			wantAllow:  "",
		},
		{
			name:       "numeric jsonrpc",
			method:     "POST",
			body:       "{\"jsonrpc\":2,\"id\":1,\"method\":\"ping\"}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "-32700",
			wantAllow:  "",
		},
		{
			name:       "object id",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":{},\"method\":\"ping\"}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "id must be",
			wantAllow:  "",
		},
		{
			name:       "array id",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":[],\"method\":\"ping\"}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "id must be",
			wantAllow:  "",
		},
		{
			name:       "missing method",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":1}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "missing method",
			wantAllow:  "",
		},
		{
			name:       "empty method",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"\"}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "missing method",
			wantAllow:  "",
		},
		{
			name:       "notification absent id",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"method\":\"ping\"}",
			origin:     "",
			mode:       "ok",
			wantStatus: 202,
			wantBody:   "",
			wantAllow:  "",
		},
		{
			name:       "notification null id",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":null,\"method\":\"ping\"}",
			origin:     "",
			mode:       "ok",
			wantStatus: 202,
			wantBody:   "",
			wantAllow:  "",
		},
		{
			name:       "ping numeric id",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "\"result\":{}",
			wantAllow:  "",
		},
		{
			name:       "ping string id",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":\"abc\",\"method\":\"ping\"}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "\"id\":\"abc\"",
			wantAllow:  "",
		},
		{
			name:       "initialize newest",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2025-06-18\"}}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "\"protocolVersion\":\"2025-06-18\"",
			wantAllow:  "",
		},
		{
			name:       "initialize middle",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2025-03-26\"}}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "\"protocolVersion\":\"2025-03-26\"",
			wantAllow:  "",
		},
		{
			name:       "initialize fallback",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"future\"}}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "\"protocolVersion\":\"2025-06-18\"",
			wantAllow:  "",
		},
		{
			name:       "initialize version",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":4,\"method\":\"initialize\"}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "\"version\":\"test-version\"",
			wantAllow:  "",
		},
		{
			name:       "tools list",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":5,\"method\":\"tools/list\"}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "\"wiki_search\"",
			wantAllow:  "",
		},
		{
			name:       "unknown method",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":6,\"method\":\"resources/list\"}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "method not found",
			wantAllow:  "",
		},
		{
			name:       "tool call missing params",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":7,\"method\":\"tools/call\"}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "unknown tool",
			wantAllow:  "",
		},
		{
			name:       "tool call malformed params",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":8,\"method\":\"tools/call\",\"params\":\"bad\"}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "invalid params",
			wantAllow:  "",
		},
		{
			name:       "tool call unknown",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":9,\"method\":\"tools/call\",\"params\":{\"name\":\"delete_everything\"}}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "unknown tool",
			wantAllow:  "",
		},
		{
			name:       "duplicate method last",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":10,\"method\":\"bad\",\"method\":\"ping\"}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "\"result\":{}",
			wantAllow:  "",
		},
		{
			name:       "extra properties",
			method:     "POST",
			body:       "{\"jsonrpc\":\"2.0\",\"id\":11,\"method\":\"ping\",\"extra\":{\"a\":1}}",
			origin:     "",
			mode:       "ok",
			wantStatus: 200,
			wantBody:   "\"id\":11",
			wantAllow:  "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var handler *Handler
			switch tc.mode {
			case "nil-auth":
				handler = New(Config{Dispatcher: &rpc.Dispatcher{}})
			case "nil-dispatcher":
				handler = New(Config{Authenticate: configuredMCPHandler().authenticate})
			case "deny":
				handler = New(Config{
					Authenticate: func(w http.ResponseWriter, _ *http.Request) (*clientauth.Identity, bool) {
						http.Error(w, "denied", http.StatusUnauthorized)
						return nil, false
					},
					Dispatcher: &rpc.Dispatcher{},
				})
			default:
				handler = configuredMCPHandler()
			}
			req := httptest.NewRequest(tc.method, "/mcp", strings.NewReader(tc.body))
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%q", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Errorf("body %q does not contain %q", rec.Body.String(), tc.wantBody)
			}
			if got := rec.Header().Get("Allow"); got != tc.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tc.wantAllow)
			}
		})
	}
}

func TestMCPBodyLimitBoundary(t *testing.T) {
	handler := configuredMCPHandler()
	for _, size := range []int{maxMCPBodyBytes + 1, maxMCPBodyBytes * 2} {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(bytes.Repeat([]byte{'x'}, size)))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
			}
			if !strings.Contains(strings.ToLower(rec.Body.String()), "request body too large") {
				t.Fatalf("body = %q", rec.Body.String())
			}
		})
	}
}

func TestMCPPublishedMetadataCopiesPreserveOriginal(t *testing.T) {
	versionsA := ProtocolVersions()
	versionsB := ProtocolVersions()
	versionsA[0] = "mutated"
	if versionsB[0] == "mutated" || ProtocolVersions()[0] == "mutated" {
		t.Fatal("ProtocolVersions leaked its backing slice")
	}
	toolsA := ToolDefinitions()
	toolsB := ToolDefinitions()
	original := toolsB[0].Name
	toolsA[0].Name = "mutated"
	if toolsB[0].Name != original || ToolDefinitions()[0].Name != original {
		t.Fatal("ToolDefinitions leaked its backing slice")
	}
}

func TestMCPToolCatalogEnforcesSecurityContract(t *testing.T) {
	seenNames := map[string]bool{}
	seenMethods := map[string]bool{}
	for i, tool := range ToolDefinitions() {
		t.Run(fmt.Sprintf("tool_%02d_%s", i, tool.Name), func(t *testing.T) {
			if strings.TrimSpace(tool.Name) == "" {
				t.Error("tool name is empty")
			}
			if seenNames[tool.Name] {
				t.Errorf("duplicate tool name %q", tool.Name)
			}
			seenNames[tool.Name] = true
			if strings.TrimSpace(tool.Description) == "" {
				t.Errorf("tool %q has no description", tool.Name)
			}
			if !strings.HasPrefix(tool.Method, "miniapp.") {
				t.Errorf("tool %q escapes miniapp namespace", tool.Name)
			}
			for _, forbidden := range []string{"delete", "remove", "send", "write", "save", "update", "execute", "run"} {
				if strings.Contains(strings.ToLower(tool.Method), forbidden) {
					t.Errorf("tool %q exposes suspicious mutating method %q", tool.Name, tool.Method)
				}
			}
			if seenMethods[tool.Method] {
				t.Errorf("duplicate RPC mapping %q", tool.Method)
			}
			seenMethods[tool.Method] = true
			if got := tool.Schema["type"]; got != "object" {
				t.Errorf("schema type = %#v", got)
			}
			if _, ok := tool.Schema["properties"].(map[string]any); !ok {
				t.Errorf("schema properties type %T", tool.Schema["properties"])
			}
		})
	}
}

func TestMCPConcurrentPureBoundaries(t *testing.T) {
	const workers = 128
	const iterations = 100
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {

		wg.Add(1)
		go func() {
			defer wg.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				raw := json.RawMessage(fmt.Sprintf("%d", worker*iterations+iteration))
				if got := InternalID(raw); !strings.HasPrefix(got, "n") {
					errs <- fmt.Errorf("InternalID(%s) = %q", raw, got)
					return
				}
				if got := negotiateMCPVersion(json.RawMessage(`{"protocolVersion":"2025-03-26"}`)); got != "2025-03-26" {
					errs <- fmt.Errorf("negotiated %q", got)
					return
				}
				if len(ProtocolVersions()) != 4 {
					errs <- fmt.Errorf("protocol catalog changed length")
					return
				}
				if len(ToolDefinitions()) != 7 {
					errs <- fmt.Errorf("tool catalog changed length")
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
