package mcpapi

// MCP 2026-07-28 ("MCP 2.0") — the only revision this endpoint speaks.
//
// The handshake era (2024-11-05 … 2025-11-25) pinned a connection's protocol
// version once, in `initialize`. 2026-07-28 removes that handshake: every
// request carries its own version, client identity, and capabilities in
// `params._meta`, mirrored into HTTP headers so intermediaries can route
// without parsing the body.
//
// This gateway served both eras briefly (#4561) and then dropped the older one
// deliberately: the handshake path existed only for external clients, cost a
// second code path through every result, and nothing we control needed it.
// A client on an older revision now gets a 400 that names what we speak rather
// than a silently degraded session.
//
// What this gateway implements from the revision:
//   - `server/discover` (servers MUST implement it) — version/capability probe;
//   - per-request version resolution from `MCP-Protocol-Version` + `_meta`;
//   - `Mcp-Method` / `Mcp-Name` header-vs-body validation (HeaderMismatch);
//   - `resultType` and `_meta.serverInfo` on every result;
//   - `ttlMs` / `cacheScope` cache hints on `tools/list`;
//   - `404` for unknown methods (the revision mandates the status, not just
//     the JSON-RPC code).
//
// Deliberately not implemented — nothing on this read-only surface needs them:
// MRTR input requests (no sampling/elicitation), `subscriptions/listen` (the
// tool list is a compile-time constant, so `listChanged` is false), and the
// tasks extension (every tool answers synchronously).

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// protocolVersion2026 is the stateless revision — "MCP 2.0" in SDK numbering.
const protocolVersion2026 = "2026-07-28"

// Reserved `_meta` keys from the revision. The `io.modelcontextprotocol/`
// prefix is reserved for the spec, so these names are stable.
const (
	metaProtocolVersion = "io.modelcontextprotocol/protocolVersion"
	metaServerInfo      = "io.modelcontextprotocol/serverInfo"
)

// JSON-RPC error codes the revision allocates to itself out of the
// server-error range (-32020…-32099).
const (
	codeHeaderMismatch             = -32020
	codeUnsupportedProtocolVersion = -32022
)

// resultTypeComplete marks an ordinary (non-MRTR) result. This surface never
// needs client input mid-call, so it is the only value we emit.
const resultTypeComplete = "complete"

// Cache hints for list-shaped results. The tool catalog is a compile-time
// constant, so the only thing that can invalidate it is a gateway redeploy —
// frequent enough (srv4 auto-deploy timer) that a five-minute hint is honest
// while still killing client polling.
const (
	listCacheTTLMs = 300_000
	// cacheScope private: the surface sits behind a single-user client token,
	// so a shared intermediary must not serve one auth context's response to
	// another — even though today every token sees the same catalog.
	listCacheScope = "private"
)

// serverInstructions is the natural-language guidance handed to models, shared
// by `initialize` (handshake era) and `server/discover` (stateless era).
const serverInstructions = "데네브(개인 업무 비서)의 읽기 전용 기억 표면입니다. 프로젝트 현황은 project_digests, 지식 검색은 wiki_search/search_all, 문서 열람은 wiki_read를 사용하세요."

// Standard request headers the revision mirrors from the JSON-RPC body.
const (
	headerProtocolVersion = "MCP-Protocol-Version"
	headerMethod          = "Mcp-Method"
	headerName            = "Mcp-Name"
)

// Base64 sentinel wrapping a header value that cannot be carried as plain
// ASCII. Deneb's tool names are ASCII, but a conforming client MAY encode any
// value, so the comparison has to decode first.
const (
	headerB64Prefix = "=?base64?"
	headerB64Suffix = "?="
)

// mcpError is a JSON-RPC error plus the HTTP status the revision mandates for
// it. The handshake era answered every protocol error with 200; 2026-07-28
// pins specific statuses (400 for header/version faults, 404 for unknown
// methods) so intermediaries can act without parsing the body.
type mcpError struct {
	Status  int
	Code    int
	Message string
	Data    any
}

// requestMetaVersion pulls `params._meta["io.modelcontextprotocol/protocolVersion"]`
// out of a request body. A malformed or absent `_meta` reads as "" — the
// caller decides whether that is legal for the resolved era.
func requestMetaVersion(params json.RawMessage) string {
	if len(params) == 0 {
		return ""
	}
	var p struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ""
	}
	raw, ok := p.Meta[metaProtocolVersion]
	if !ok {
		return ""
	}
	var version string
	if err := json.Unmarshal(raw, &version); err != nil {
		return ""
	}
	return version
}

// declaresProtocol reports whether a request carries any 2.0 version marker at
// all. A request with none is not a half-formed 2.0 request — it is a client
// from the handshake era, or one probing to find out what we are.
func declaresProtocol(r *http.Request, msg mcpMessage) bool {
	return strings.TrimSpace(r.Header.Get(headerProtocolVersion)) != "" ||
		strings.TrimSpace(requestMetaVersion(msg.Params)) != ""
}

// requireProtocolVersion admits a request only if it declares 2026-07-28 in
// BOTH carriers the revision requires — the `MCP-Protocol-Version` header and
// `params._meta` — and they agree.
//
// Every rejection names what we speak, one way or another: an unsupported
// version gets the supported list in `data`, and a missing declaration gets a
// message pointing at `server/discover`, which stays reachable without any of
// this (see Handler.ServeHTTP).
func requireProtocolVersion(r *http.Request, msg mcpMessage) *mcpError {
	header := strings.TrimSpace(r.Header.Get(headerProtocolVersion))
	meta := strings.TrimSpace(requestMetaVersion(msg.Params))

	if header != "" && meta != "" && header != meta {
		return headerMismatch(fmt.Sprintf(
			"%s header %q does not match body _meta %q", headerProtocolVersion, header, meta,
		))
	}
	declared := meta
	if declared == "" {
		declared = header
	}
	if declared != "" && declared != protocolVersion2026 {
		return unsupportedProtocolVersion(declared)
	}
	if header == "" {
		return headerMismatch(fmt.Sprintf(
			"missing required header: %s — this endpoint speaks only %s (call server/discover)",
			headerProtocolVersion, protocolVersion2026,
		))
	}
	if meta == "" {
		return headerMismatch(fmt.Sprintf(
			"missing required request field: params._meta[%q]", metaProtocolVersion,
		))
	}
	return nil
}

// unsupportedProtocolVersion tells a client on another revision exactly what
// this endpoint speaks, so it can renegotiate instead of guessing.
func unsupportedProtocolVersion(requested string) *mcpError {
	return &mcpError{
		Status:  http.StatusBadRequest,
		Code:    codeUnsupportedProtocolVersion,
		Message: "unsupported MCP protocol version: " + requested,
		Data: map[string]any{
			"requested": requested,
			"supported": ProtocolVersions(),
		},
	}
}

// validateMethodHeader enforces the `Mcp-Method` mirror. Required on every
// stateless request so a gateway can authorize on the header alone; a
// disagreement between header and body is the exact split-brain the revision
// added the check to close.
func validateMethodHeader(r *http.Request, msg mcpMessage) *mcpError {
	got := r.Header.Get(headerMethod)
	if got == "" {
		return headerMismatch("missing required header: " + headerMethod)
	}
	if got != msg.Method {
		return headerMismatch(fmt.Sprintf(
			"%s header %q does not match body method %q", headerMethod, got, msg.Method,
		))
	}
	return nil
}

// validateNameHeader enforces the `Mcp-Name` mirror, required for `tools/call`
// (and, on servers that offer them, `resources/read` and `prompts/get`).
func validateNameHeader(r *http.Request, name string) *mcpError {
	raw := r.Header.Get(headerName)
	if raw == "" {
		return headerMismatch("missing required header: " + headerName)
	}
	got, err := decodeHeaderValue(raw)
	if err != nil {
		return headerMismatch(fmt.Sprintf("invalid %s header: %v", headerName, err))
	}
	if got != name {
		return headerMismatch(fmt.Sprintf(
			"%s header %q does not match body params.name %q", headerName, got, name,
		))
	}
	return nil
}

// decodeHeaderValue unwraps the revision's Base64 sentinel, returning a plain
// value unchanged. Clients MUST also encode a literal that merely looks like
// the sentinel, so an undecodable payload is an error rather than a fallback
// to the raw string.
func decodeHeaderValue(v string) (string, error) {
	const minSentinel = len(headerB64Prefix) + len(headerB64Suffix)
	if len(v) < minSentinel || !strings.HasPrefix(v, headerB64Prefix) || !strings.HasSuffix(v, headerB64Suffix) {
		return v, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(v[len(headerB64Prefix) : len(v)-len(headerB64Suffix)])
	if err != nil {
		return "", errors.New("malformed base64 sentinel")
	}
	return string(decoded), nil
}

func headerMismatch(message string) *mcpError {
	return &mcpError{Status: http.StatusBadRequest, Code: codeHeaderMismatch, Message: message}
}

// serverCapabilities is what this gateway offers: tools only, and a catalog
// fixed at compile time (hence listChanged false, hence no need for the
// subscriptions/listen stream).
func serverCapabilities() jsonObject {
	return jsonObject{"tools": jsonObject{"listChanged": false}}
}

// serverInfo identifies this gateway. The handshake era put it in the
// initialize result; the stateless era puts it in every result's `_meta`.
func (s *Handler) serverInfo() jsonObject {
	return jsonObject{"name": "deneb", "title": "Deneb Gateway", "version": s.version}
}

// discoverResult answers `server/discover` — the revision's replacement for
// the initialize handshake as a version/capability probe. Servers MUST
// implement it, and clients MAY call it before anything else.
func (s *Handler) discoverResult() jsonObject {
	return jsonObject{
		"supportedVersions": ProtocolVersions(),
		"capabilities":      serverCapabilities(),
		"instructions":      serverInstructions,
		"ttlMs":             listCacheTTLMs,
		"cacheScope":        listCacheScope,
	}
}

// decorateResult adds the fields every result must carry.
func (s *Handler) decorateResult(result jsonObject) jsonObject {
	result["resultType"] = resultTypeComplete
	result["_meta"] = jsonObject{metaServerInfo: s.serverInfo()}
	return result
}

// withListCacheHints adds the CacheableResult fields the revision requires on
// list-shaped results, letting a client reuse the catalog across connections
// instead of re-listing on every one.
func withListCacheHints(result jsonObject) jsonObject {
	result["ttlMs"] = listCacheTTLMs
	result["cacheScope"] = listCacheScope
	return result
}
