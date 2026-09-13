package httputil

import (
	"net"
	"net/url"
	"strings"
)

// Hostname extracts the lowercased hostname from a URL string, with any
// trailing dot stripped. Returns "" for empty/unparseable input. Safe to
// use on bare hosts ("api.foo.com") and full URLs.
//
// Bare hosts without a scheme are parsed as authorities (e.g. "api.foo.com"
// or "api.foo.com:8080" → "api.foo.com"). IPv6 literals are returned
// without their enclosing brackets (e.g. "[::1]:8080" → "::1").
func Hostname(rawURL string) string {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return ""
	}

	// If the input has no scheme, parse it as an authority-only URL so
	// that url.Parse populates Host/Hostname instead of treating the
	// whole string as a relative path.
	toParse := raw
	if !strings.Contains(raw, "://") {
		toParse = "//" + raw
	}

	u, err := url.Parse(toParse)
	if err != nil {
		return ""
	}

	host := strings.ToLower(u.Hostname())
	return strings.TrimRight(host, ".")
}

// HostMatches reports whether rawURL's hostname is `domain` or a subdomain
// of it. Safer counterpart to `strings.Contains(rawURL, domain)`, which
// mis-classifies attacker-controlled paths and hosts such as
// "https://evil.com/api.openai.com/v1" or "https://api.openai.com.evil/v1"
// as native endpoints and routes auth/api-mode to the wrong provider.
//
// Exact-match semantics guard against substring-match attacks:
//
//	HostMatches("https://api.openai.com/v1",           "openai.com") == true
//	HostMatches("https://openai.com",                   "openai.com") == true
//	HostMatches("https://evil.com/api.openai.com/v1",   "openai.com") == false
//	HostMatches("https://api.openai.com.evil",          "openai.com") == false
//
// Empty/unparseable inputs (either argument) return false. The domain
// argument is normalized the same way as the hostname (lowercased,
// trailing dot stripped) before comparison.
func HostMatches(rawURL, domain string) bool {
	host := Hostname(rawURL)
	if host == "" {
		return false
	}
	d := strings.TrimRight(strings.ToLower(strings.TrimSpace(domain)), ".")
	if d == "" {
		return false
	}
	return host == d || strings.HasSuffix(host, "."+d)
}

// IsPrivateHost reports whether host is plausibly a machine this deployment
// owns: loopback, an RFC1918 address, or the Tailscale CGNAT range that this
// fleet reaches its other DGX nodes through. It guards operator-supplied
// scrape targets (the serving engine's /metrics endpoint) from being aimed at
// the public internet.
//
// Only the literal name "localhost" is accepted; every other name is rejected
// rather than resolved, so DNS never gets to decide what we connect to.
func IsPrivateHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || isCGNAT(ip)
}

// isCGNAT reports whether ip falls in 100.64.0.0/10 — the Tailscale address
// space, which is how this deployment reaches its second and third DGX nodes.
func isCGNAT(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xC0 == 0x40
}
