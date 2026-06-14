// client.go — caller identification. wormhole fronts many clients (the Deneb
// gateway on the hot path, Claude Code, the OpenAI/Anthropic SDKs, scripts), and
// some of them want the response shaped a little differently. This is the
// foundation for that: read WHO is calling from the request, classify it, and
// carry that down to where the response is produced (streamResponse) so
// per-client behavior can hang off it. Identification only here — the actual
// per-client shaping is a documented seam in streamResponse.
package main

import (
	"net/http"
	"strings"
)

// clientKind is a coarse classification of the caller, used to branch per-client
// behavior and to break the /metrics request count down by who is calling.
type clientKind string

const (
	clientDeneb        clientKind = "deneb"         // the Deneb gateway (internal hot path)
	clientClaudeCode   clientKind = "claude-code"   // Anthropic's coding CLI
	clientOpenAISDK    clientKind = "openai-sdk"    // openai-python / -node etc.
	clientAnthropicSDK clientKind = "anthropic-sdk" // anthropic-python / -node etc.
	clientCurl         clientKind = "curl"          // curl / wget / ad-hoc
	clientUnknown      clientKind = "unknown"
)

// clientInfo is what wormhole knows about the caller of one request: a coarse
// kind (to branch on), a compact human label (for logs/metrics), and the raw
// User-Agent (kept for the seam, in case finer matching is needed later).
type clientInfo struct {
	kind      clientKind
	name      string
	userAgent string
}

// identifyClient reads the calling client from the request. An explicit
// X-Wormhole-Client header wins (a client — or the operator — declaring itself);
// otherwise the User-Agent is classified. Identification never fails: an unknown
// client is served like any other, it just gets the default (no-op) shaping.
func identifyClient(r *http.Request) clientInfo {
	ua := strings.TrimSpace(r.UserAgent())
	if c := strings.TrimSpace(r.Header.Get("X-Wormhole-Client")); c != "" {
		return clientInfo{kind: classifyClient(c), name: c, userAgent: ua}
	}
	return clientInfo{kind: classifyClient(ua), name: clientLabel(ua), userAgent: ua}
}

// classifyClient maps a free-form client string (the explicit header or the
// User-Agent) onto a coarse kind by well-known substrings.
func classifyClient(s string) clientKind {
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "deneb"):
		return clientDeneb
	case strings.Contains(l, "claude-code") || strings.Contains(l, "claudecode"):
		return clientClaudeCode
	case strings.Contains(l, "anthropic"):
		return clientAnthropicSDK
	case strings.Contains(l, "openai"):
		return clientOpenAISDK
	case strings.Contains(l, "curl") || strings.Contains(l, "wget"):
		return clientCurl
	default:
		return clientUnknown
	}
}

// clientLabel turns a User-Agent into a compact label for logs/metrics: the
// product token before the first space, with any /version suffix stripped
// ("OpenAI/Python 1.2.0" → "OpenAI"). Empty UA → "unknown".
func clientLabel(ua string) string {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return "unknown"
	}
	if i := strings.IndexByte(ua, ' '); i > 0 {
		ua = ua[:i]
	}
	if i := strings.IndexByte(ua, '/'); i > 0 {
		ua = ua[:i]
	}
	return ua
}
