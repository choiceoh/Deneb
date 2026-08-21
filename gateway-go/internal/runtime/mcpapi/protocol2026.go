package mcpapi

// MCP 2026-07-28 ("MCP 2.0") — the stateless revision.
//
// The handshake era (2024-11-05 … 2025-06-18) pinned a connection's protocol
// version once, in `initialize`. 2026-07-28 removes that handshake: every
// request carries its own version, client identity, and capabilities in
// `params._meta`, mirrored into HTTP headers so intermediaries can route
// without parsing the body. This file holds everything specific to that
// revision; handler.go stays the transport and keeps the older era working
// byte-for-byte.
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

// handshakeProtocolVersion is the newest revision that still speaks
// `initialize`. A client using the handshake is by definition in that era, so
// negotiation for an unknown requested version answers with this rather than
// with the (handshake-less) newest revision overall.
const handshakeProtocolVersion = "2025-06-18"

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

// supportsProtocolVersion reports whether a requested revision is one we speak.
func supportsProtocolVersion(version string) bool {
	for _, v := range mcpProtocolVersions {
		if v == version {
			return true
		}
	}
	return false
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

// resolveProtocolVersion decides which revision a request speaks.
//
// A stateless request declares its version twice — in the
// `MCP-Protocol-Version` header and in `params._meta` — and the two MUST
// agree. A handshake-era request declares neither per-request (its version was
// pinned by `initialize`), which resolves to "" and leaves the old path alone.
func resolveProtocolVersion(r *http.Request, msg mcpMessage) (string, *mcpError) {
	header := strings.TrimSpace(r.Header.Get(headerProtocolVersion))
	meta := strings.TrimSpace(requestMetaVersion(msg.Params))

	if header != "" && meta != "" && header != meta {
		return "", headerMismatch(fmt.Sprintf(
			"%s header %q does not match body _meta %q", headerProtocolVersion, header, meta,
		))
	}
	version := meta
	if version == "" {
		version = header
	}
	if version == "" {
		return "", nil // handshake era — initialize pinned the version
	}
	if !supportsProtocolVersion(version) {
		return "", &mcpError{
			Status:  http.StatusBadRequest,
			Code:    codeUnsupportedProtocolVersion,
			Message: "unsupported MCP protocol version: " + version,
			Data: map[string]any{
				"requested": version,
				"supported": ProtocolVersions(),
			},
		}
	}
	// The stateless revision requires BOTH carriers; an older revision's
	// version may legally arrive in the header alone.
	if version == protocolVersion2026 {
		if header == "" {
			return "", headerMismatch("missing required header: " + headerProtocolVersion)
		}
		if meta == "" {
			return "", headerMismatch(fmt.Sprintf(
				"missing required request field: params._meta[%q]", metaProtocolVersion,
			))
		}
	}
	return version, nil
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

// decorateResult adds the fields every stateless result must carry. A
// handshake-era result is returned untouched: `resultType` did not exist
// there, and older clients validate results against the older schema.
func (s *Handler) decorateResult(result jsonObject, version string) jsonObject {
	if version != protocolVersion2026 {
		return result
	}
	result["resultType"] = resultTypeComplete
	result["_meta"] = jsonObject{metaServerInfo: s.serverInfo()}
	return result
}

// withListCacheHints adds the CacheableResult fields the revision requires on
// list-shaped results, letting a client reuse the catalog across connections
// instead of re-listing on every one.
func withListCacheHints(result jsonObject, version string) jsonObject {
	if version != protocolVersion2026 {
		return result
	}
	result["ttlMs"] = listCacheTTLMs
	result["cacheScope"] = listCacheScope
	return result
}
