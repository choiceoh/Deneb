package auth

import (
	"net"
	"net/url"
	"strings"
)

// OriginCheckResult represents the outcome of a browser origin validation.
type OriginCheckResult struct {
	OK        bool   `json:"ok"`
	MatchedBy string `json:"matchedBy,omitempty"` // "allowlist", "host-header-fallback", "local-loopback"
	Reason    string `json:"reason,omitempty"`
}

// CheckBrowserOrigin validates the Origin header against allowed origins.
// Mirrors checkBrowserOrigin in src/gateway/auth/origin-check.ts.
func CheckBrowserOrigin(requestHost, origin string, allowedOrigins []string, allowHostHeaderFallback, isLocalClient bool) OriginCheckResult {
	parsed := parseOrigin(origin)
	if parsed == nil {
		return OriginCheckResult{OK: false, Reason: "origin missing or invalid"}
	}

	// Check allowlist.
	allowlist := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		o = strings.TrimSpace(strings.ToLower(o))
		if o != "" {
			allowlist[o] = true
		}
	}
	if allowlist["*"] || allowlist[parsed.origin] {
		return OriginCheckResult{OK: true, MatchedBy: "allowlist"}
	}

	// Host-header fallback.
	normalizedHost := normalizeHostHeader(requestHost)
	if allowHostHeaderFallback && normalizedHost != "" && parsed.host == normalizedHost {
		return OriginCheckResult{OK: true, MatchedBy: "host-header-fallback"}
	}

	// Local loopback fallback (only for genuinely local socket clients).
	if isLocalClient && isLoopbackHost(parsed.hostname) {
		return OriginCheckResult{OK: true, MatchedBy: "local-loopback"}
	}

	return OriginCheckResult{OK: false, Reason: "origin not allowed"}
}

type parsedOrigin struct {
	origin   string
	host     string
	hostname string
}

func parseOrigin(raw string) *parsedOrigin {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" {
		return nil
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Host)
	hostname := strings.ToLower(u.Hostname())
	return &parsedOrigin{
		origin:   scheme + "://" + host,
		host:     host,
		hostname: hostname,
	}
}

func normalizeHostHeader(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	// Strip default ports.
	if strings.HasSuffix(host, ":80") || strings.HasSuffix(host, ":443") {
		h, _, err := net.SplitHostPort(host)
		if err == nil {
			return h
		}
	}
	return host
}

func isLoopbackHost(hostname string) bool {
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" || hostname == "[::1]" {
		return true
	}
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}
