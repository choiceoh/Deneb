// Package mcpapi — MCP (Model Context Protocol) gateway.
// Formerly server_http_mcp.go.
//
// Exposes an ALLOWLISTED, READ-ONLY subset of the miniapp.* RPC surface as MCP
// tools over the protocol's streamable-HTTP transport, so external AI tools —
// Claude Code on the operator's laptop first — can use Deneb's memory (wiki,
// project digests, diary, calendar, unified search) as first-class tools
// instead of ad-hoc ssh/curl. The go-micro v6 "every endpoint is an MCP tool"
// idea, sized to Deneb: no new dependency, no new business logic — each MCP
// tool is a declarative mapping onto an existing miniapp.* method, dispatched
// through the same registry and client-token auth as the native client.
//
//	POST /mcp   JSON-RPC 2.0 (server/discover · tools/list · tools/call)
//	  MCP-Protocol-Version: 2026-07-28     ← required; nothing older is served
//	  X-Deneb-Client-Token: <secret>       ← same single-user bearer as miniapp
//
// ONE protocol revision is served: 2026-07-28 ("MCP 2.0"). The `initialize`
// handshake era was supported briefly (#4561) and then dropped — it existed
// only for external clients, and carrying it meant a second shape for every
// result. A client on an older revision gets a 400 naming what we speak; the
// one exception is `server/discover`, which answers without any 2.0 metadata
// precisely so such a client can find that out. See protocol2026.go.
//
// Deliberate minimalism (spec-conformant subset):
//   - stateless: no Mcp-Session-Id, every request self-contained — which is
//     what 2026-07-28 made mandatory and this surface always did;
//   - single JSON responses (no SSE stream; GET/DELETE → 405) — legal for
//     servers that never push;
//   - tools capability only (no resources/prompts);
//   - notifications (no id) are accepted with 202 and dropped;
//   - JSON-RPC batches rejected (removed from the spec in 2025-06-18).
//
// Kill switch: DENEB_MCP_DISABLE=1 skips route registration.
package mcpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Authenticate verifies a miniapp request and returns its client identity.
type Authenticate func(http.ResponseWriter, *http.Request) (*clientauth.Identity, bool)

// Config supplies the runtime dependencies for the MCP HTTP surface.
type Config struct {
	Authenticate Authenticate
	Dispatcher   *rpc.Dispatcher
	Version      string
	Logger       *slog.Logger
}

// Handler exposes the read-only Deneb MCP tool surface over HTTP.
type Handler struct {
	authenticate Authenticate
	dispatcher   *rpc.Dispatcher
	version      string
	logger       *slog.Logger
}

// New constructs an MCP handler from gateway-owned dependencies.
func New(cfg Config) *Handler {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		authenticate: cfg.Authenticate,
		dispatcher:   cfg.Dispatcher,
		version:      cfg.Version,
		logger:       logger,
	}
}

// maxMCPBodyBytes caps an MCP request body — tool arguments are tiny JSON.
const maxMCPBodyBytes = 1 << 20 // 1 MiB

// mcpProtocolVersions are the spec revisions this gateway accepts. One entry:
// `server/discover` advertises it, and every rejection of an older client
// carries it as the `supported` list so the client learns what to speak.
var mcpProtocolVersions = []string{protocolVersion2026}

// mcpToolDef declaratively maps one MCP tool onto a miniapp.* RPC method. The
// tool's arguments object is passed through as the RPC params verbatim, so
// property names below MUST match the handler's params struct tags.
type ToolDefinition struct {
	Name        string
	Description string
	Method      string // miniapp.* method the tool dispatches to (read-only!)
	Schema      jsonObject
}

// mcpTools is the v1 read-only surface. Adding a WRITE tool here is a security
// decision, not a mapping edit — keep this list read-only unless the operator
// explicitly asks otherwise.
var mcpTools = []ToolDefinition{
	{
		Name:        "wiki_search",
		Description: "데네브 위키(업무 지식베이스) 전문 검색. 프로젝트·거래처·인물·업무 지식을 키워드로 찾는다. 결과의 path로 wiki_read 하면 전문을 읽을 수 있다.",
		Method:      "miniapp.memory.search",
		Schema: mcpObjectSchema(map[string]any{
			"query": map[string]any{"type": "string", "description": "검색어 (한국어)"},
			"limit": map[string]any{"type": "integer", "description": "최대 결과 수 (기본 10)"},
		}, []string{"query"}),
	},
	{
		Name:        "wiki_read",
		Description: "위키 문서 한 편을 경로로 읽는다 (예: 프로젝트/기아-화성/대표.md). 프로젝트 대표페이지에는 '현재 상태' 요약이 있다.",
		Method:      "miniapp.memory.get_page",
		Schema: mcpObjectSchema(map[string]any{
			"path": map[string]any{"type": "string", "description": "위키 문서 경로"},
		}, []string{"path"}),
	},
	{
		Name:        "wiki_list",
		Description: "위키 문서 목록. category를 주면 그 폴더 하위만(예: 프로젝트/기아-화성), 생략하면 전체. 폴더 구조: 프로젝트/<이름>/{대표.md, 로그.md, 기자재/, 메일분석/}.",
		Method:      "miniapp.memory.list_in_category",
		Schema: mcpObjectSchema(map[string]any{
			"category": map[string]any{"type": "string", "description": "카테고리/폴더 경로 (생략 = 전체)"},
			"limit":    map[string]any{"type": "integer", "description": "최대 결과 수 (기본 50)"},
		}, nil),
	},
	{
		Name:        "project_digests",
		Description: "진행 중인 프로젝트별 현재 상태 요약. 각 프로젝트의 최신 진행상황 헤드라인과 불릿을 돌려준다.",
		Method:      "miniapp.project.digests",
		Schema:      mcpObjectSchema(map[string]any{}, nil),
	},
	{
		Name:        "diary_recent",
		Description: "데네브의 최근 일지(작업 기록) 항목.",
		Method:      "miniapp.memory.diary_recent",
		Schema: mcpObjectSchema(map[string]any{
			"limit": map[string]any{"type": "integer", "description": "최대 항목 수 (기본 20)"},
		}, nil),
	},
	{
		Name:        "calendar_upcoming",
		Description: "다가오는 일정 목록.",
		Method:      "miniapp.calendar.list_upcoming",
		Schema: mcpObjectSchema(map[string]any{
			"hoursAhead": map[string]any{"type": "integer", "description": "몇 시간 앞까지 볼지 (기본값 있음)"},
			"limit":      map[string]any{"type": "integer", "description": "최대 결과 수"},
		}, nil),
	},
	{
		Name:        "search_all",
		Description: "데네브 통합 검색 — 위키·메일·일정·인물 등 모든 소스를 한 번에 검색한다.",
		Method:      "miniapp.search.all",
		Schema: mcpObjectSchema(map[string]any{
			"query": map[string]any{"type": "string", "description": "검색어"},
			"limit": map[string]any{"type": "integer", "description": "최대 결과 수 (기본 10)"},
		}, []string{"query"}),
	},
}

func mcpObjectSchema(props map[string]any, required []string) map[string]any {
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// mcpMessage is an incoming JSON-RPC 2.0 message.
type mcpMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// handleMCP is the streamable-HTTP MCP endpoint.
func (s *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		// continue below
	case http.MethodGet, http.MethodDelete:
		// GET opens the optional server-push SSE stream; DELETE ends a session.
		// This gateway is stateless and never pushes — both are unsupported.
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "SSE stream/session not supported (stateless MCP server)", http.StatusMethodNotAllowed)
		return
	default:
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// DNS-rebinding hardening from the MCP spec: this surface has no browser
	// use-case, so any Origin header (browsers always send one cross-origin)
	// is rejected outright. CLI/desktop MCP clients send none.
	if r.Header.Get("Origin") != "" {
		http.Error(w, "browser origins not allowed", http.StatusForbidden)
		return
	}

	if s.authenticate == nil || s.dispatcher == nil {
		http.Error(w, "MCP handler is not configured", http.StatusServiceUnavailable)
		return
	}
	identity, ok := s.authenticate(w, r)
	if !ok {
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMCPBodyBytes))
	if err != nil {
		status := http.StatusBadRequest
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, "read body: "+err.Error(), status)
		return
	}

	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "[") {
		s.writeMCPError(w, nil, -32600, "JSON-RPC batching is not supported (MCP 2025-06-18)")
		return
	}
	var msg mcpMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		s.writeMCPError(w, nil, -32700, "parse error: "+err.Error())
		return
	}
	if msg.JSONRPC != "2.0" {
		s.writeMCPError(w, nil, -32600, `jsonrpc must be "2.0"`)
		return
	}
	// JSON-RPC ids are string, number, or null — an object/array id is an
	// invalid request (and must not be echoed back; the reply id is null,
	// per the spec's invalid-request rule).
	if id := strings.TrimSpace(string(msg.ID)); strings.HasPrefix(id, "{") || strings.HasPrefix(id, "[") {
		s.writeMCPError(w, nil, -32600, "id must be a string, number, or null")
		return
	}
	if msg.Method == "" {
		s.writeMCPError(w, msg.ID, -32600, "missing method")
		return
	}

	// Notifications (no id) are fire-and-forget: acknowledge and drop.
	if len(msg.ID) == 0 || string(msg.ID) == "null" {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// server/discover is the one door left open to a client that has not
	// declared 2.0 — including one that cannot. It returns compile-time
	// constants only, so there is nothing to protect, and refusing it would
	// leave an older client with no machine-readable way to learn what this
	// endpoint speaks. A discover request that DOES carry version markers is
	// validated like any other, so a half-formed 2.0 request still fails.
	if msg.Method == "server/discover" && !declaresProtocol(r, msg) {
		s.writeMCPResult(w, msg.ID, s.decorateResult(s.discoverResult()))
		return
	}
	if verr := requireProtocolVersion(r, msg); verr != nil {
		s.writeMCPErrorFrame(w, msg.ID, verr)
		return
	}
	if herr := validateMethodHeader(r, msg); herr != nil {
		s.writeMCPErrorFrame(w, msg.ID, herr)
		return
	}

	switch msg.Method {
	case "server/discover":
		s.writeMCPResult(w, msg.ID, s.decorateResult(s.discoverResult()))
	case "tools/list":
		// mcpTools is a package-level slice, so the order is deterministic —
		// which is what lets a 2.0 client cache the catalog under ttlMs and
		// keeps LLM prompt-cache prefixes stable across listings.
		tools := make([]map[string]any, 0, len(mcpTools))
		for _, t := range mcpTools {
			tools = append(tools, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.Schema,
			})
		}
		result := withListCacheHints(map[string]any{"tools": tools})
		s.writeMCPResult(w, msg.ID, s.decorateResult(result))
	case "tools/call":
		s.handleMCPToolCall(w, r, identity, msg)
	default:
		// Covers initialize, ping and logging/setLevel, all removed in this
		// revision, alongside genuinely unknown methods.
		s.writeMethodNotFound(w, msg.ID, msg.Method)
	}
}

// handleMCPToolCall bridges one MCP tool invocation onto its miniapp.* RPC.
func (s *Handler) handleMCPToolCall(w http.ResponseWriter, r *http.Request, identity *clientauth.Identity, msg mcpMessage) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	}
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			s.writeMCPError(w, msg.ID, -32602, "invalid params: "+err.Error())
			return
		}
	}
	// Header validation is transport-level: it runs before the tool lookup so
	// a mismatched header never reaches dispatch, whether or not the name is
	// one we serve.
	if herr := validateNameHeader(r, p.Name); herr != nil {
		s.writeMCPErrorFrame(w, msg.ID, herr)
		return
	}
	var tool *ToolDefinition
	for i := range mcpTools {
		if mcpTools[i].Name == p.Name {
			tool = &mcpTools[i]
			break
		}
	}
	if tool == nil {
		s.writeMCPError(w, msg.ID, -32602, "unknown tool: "+p.Name)
		return
	}

	frame := &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "mcp-" + InternalID(msg.ID),
		Method: tool.Method,
		Params: p.Arguments,
	}
	ctx := clientauth.WithContext(r.Context(), identity)
	resp := s.dispatcher.Dispatch(ctx, frame)

	// Tool-level failures ride INSIDE the result with isError (MCP spec) so the
	// calling model can see and react to them; protocol-level errors above use
	// JSON-RPC error objects.
	if resp == nil || !resp.OK {
		text := "tool call failed"
		if resp != nil && resp.Error != nil {
			text = fmt.Sprintf("%s: %s", resp.Error.Code, resp.Error.Message)
		}
		s.writeMCPResult(w, msg.ID, s.decorateResult(map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": true,
		}))
		return
	}
	text := "{}"
	if len(resp.Payload) > 0 {
		if pretty, err := json.MarshalIndent(resp.Payload, "", "  "); err == nil {
			text = string(pretty)
		} else {
			text = string(resp.Payload)
		}
	}
	s.writeMCPResult(w, msg.ID, s.decorateResult(map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	}))
}

// InternalID derives a bounded, type-prefixed dispatch-frame id from a JSON-RPC id.
// Type-prefixed so numeric 1 and string "1" never collide, and length-capped
// so a client-chosen id can't inflate internal ids/log lines. The response is
// matched synchronously (the JSON-RPC reply echoes msg.ID itself), so the
// derived id is tracing-only — a cap-induced tie is harmless.
func InternalID(id rawJSON) string {
	raw := string(id)
	prefix := "n" // number (id validation upstream leaves string/number only)
	if strings.HasPrefix(raw, `"`) {
		prefix = "s"
		raw = strings.Trim(raw, `"`)
	}
	const maxIDLen = 64
	if r := []rune(raw); len(r) > maxIDLen {
		raw = string(r[:maxIDLen]) // rune-safe: string ids may be non-ASCII
	}
	return prefix + raw
}

func (s *Handler) writeMCPResult(w http.ResponseWriter, id json.RawMessage, result any) {
	s.writeMCPFrame(w, map[string]any{
		"jsonrpc": "2.0",
		"id":      normalizeMCPID(id),
		"result":  result,
	})
}

func (s *Handler) writeMCPError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	s.writeMCPErrorFrame(w, id, &mcpError{Code: code, Message: message})
}

// writeMCPErrorFrame emits a JSON-RPC error with the HTTP status its code
// carries. Zero status means 200 — the handshake era's answer for every
// protocol error, and still ours for the codes 2026-07-28 left unpinned.
func (s *Handler) writeMCPErrorFrame(w http.ResponseWriter, id json.RawMessage, e *mcpError) {
	body := map[string]any{"code": e.Code, "message": e.Message}
	if e.Data != nil {
		body["data"] = e.Data
	}
	s.writeMCPFrameStatus(w, e.Status, map[string]any{
		"jsonrpc": "2.0",
		"id":      normalizeMCPID(id),
		"error":   body,
	})
}

// writeMethodNotFound answers an unimplemented method. 2026-07-28 pins the
// HTTP status to 404 so a client can tell "this endpoint lacks that method"
// from a legacy HTTP+SSE server's 404 for the endpoint itself.
func (s *Handler) writeMethodNotFound(w http.ResponseWriter, id json.RawMessage, method string) {
	s.writeMCPErrorFrame(w, id, &mcpError{
		Status:  http.StatusNotFound,
		Code:    -32601,
		Message: "method not found: " + method,
	})
}

func (s *Handler) writeMCPFrame(w http.ResponseWriter, frame map[string]any) {
	s.writeMCPFrameStatus(w, http.StatusOK, frame)
}

func (s *Handler) writeMCPFrameStatus(w http.ResponseWriter, status int, frame map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "deneb-gateway")
	if status != 0 && status != http.StatusOK {
		w.WriteHeader(status)
	}
	if err := json.NewEncoder(w).Encode(frame); err != nil {
		s.logger.Warn("mcp: response write failed", "error", err)
	}
}

// normalizeMCPID echoes the raw JSON-RPC id (string or number) back, mapping
// absent ids to explicit null so error frames stay spec-shaped.
func normalizeMCPID(id json.RawMessage) any {
	if len(id) == 0 {
		return nil
	}
	return id
}

// ProtocolVersions returns the supported MCP protocol revisions, newest first.
func ProtocolVersions() []string {
	return append([]string(nil), mcpProtocolVersions...)
}

// ToolDefinitions returns a copy of the read-only MCP tool allowlist.
func ToolDefinitions() []ToolDefinition {
	return append([]ToolDefinition(nil), mcpTools...)
}
