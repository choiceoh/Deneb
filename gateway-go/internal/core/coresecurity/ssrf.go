package coresecurity

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

// blockedSchemes are URL schemes that should never be followed.
var blockedSchemes = map[string]struct{}{
	"file": {}, "ftp": {}, "gopher": {}, "dict": {},
	"data": {}, "ldap": {}, "ldaps": {}, "tftp": {}, "telnet": {},
}

// blockedHosts are hostnames that should not be accessed (SSRF protection).
var blockedHosts = map[string]struct{}{
	"localhost": {},
	"127.0.0.1": {},
	"0.0.0.0":   {},
	"[::1]":     {},
	"::1":       {},
	"::0":       {},
	"0000:0000:0000:0000:0000:0000:0000:0001": {},
	"metadata.google.internal":                {},
	"169.254.169.254":                         {},
}

// IsSafeURL validates a URL for SSRF safety. Blocks private/loopback IPs,
// cloud metadata endpoints, dangerous schemes, and numeric IPv4 bypass
// techniques (octal, hex, decimal).
func IsSafeURL(rawURL string) bool {
	// Explicit UNC path blocking (defense-in-depth).
	if strings.HasPrefix(rawURL, "\\\\") ||
		(strings.HasPrefix(rawURL, "//") && !strings.Contains(rawURL, "://")) {
		return false
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if _, ok := blockedSchemes[scheme]; ok {
		return false
	}
	if scheme != "http" && scheme != "https" {
		return false
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return false
	}
	if _, ok := blockedHosts[host]; ok {
		return false
	}

	// Normalize IPv6: strip brackets and zone IDs.
	hostNoBrackets := strings.TrimLeft(strings.TrimRight(host, "]"), "[")
	hostNormalized := stripIPv6ZoneID(hostNoBrackets)

	// Re-check after normalization (catches [::1] → ::1, zone-id variants).
	if _, ok := blockedHosts[hostNormalized]; ok {
		return false
	}

	// Block IPv4-mapped IPv6 loopback/private.
	if strings.HasPrefix(hostNormalized, "::ffff:127.") ||
		strings.HasPrefix(hostNormalized, "::ffff:10.") ||
		strings.HasPrefix(hostNormalized, "::ffff:192.168.") ||
		strings.HasPrefix(hostNormalized, "::ffff:169.254.") {
		return false
	}

	// Block IPv6 private: fc00::/7 (ULA) and fe80::/10 (link-local).
	if strings.HasPrefix(hostNormalized, "fc") ||
		strings.HasPrefix(hostNormalized, "fd") ||
		strings.HasPrefix(hostNormalized, "fe80") {
		return false
	}

	// Parse as IP to check private/reserved ranges.
	ip := net.ParseIP(hostNormalized)
	if ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return false
		}
		// Block CGNAT range 100.64.0.0/10.
		if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return false
		}
		return true
	}

	// Host is not a standard IP — check string-prefix private ranges.
	if strings.HasPrefix(hostNormalized, "10.") || strings.HasPrefix(hostNormalized, "192.168.") {
		return false
	}
	if strings.HasPrefix(hostNormalized, "172.") {
		parts := strings.SplitN(hostNormalized, ".", 3)
		if len(parts) >= 2 {
			if n, err := strconv.Atoi(parts[1]); err == nil && n >= 16 && n <= 31 {
				return false
			}
		}
	}
	if strings.HasPrefix(hostNormalized, "169.254.") {
		return false
	}
	if strings.HasPrefix(hostNormalized, "100.") {
		parts := strings.SplitN(hostNormalized, ".", 3)
		if len(parts) >= 2 {
			if n, err := strconv.Atoi(parts[1]); err == nil && n >= 64 && n <= 127 {
				return false
			}
		}
	}

	// Block numeric IPv4 bypass techniques (octal, hex, decimal integer).
	if isNumericPrivateIPv4(hostNormalized) {
		return false
	}

	return true
}

// stripIPv6ZoneID removes zone ID from an IPv6 address.
// Zone IDs appear as %25 (URL-encoded) or % followed by interface name.
func stripIPv6ZoneID(host string) string {
	if idx := strings.Index(host, "%25"); idx >= 0 {
		return host[:idx]
	}
	if idx := strings.IndexByte(host, '%'); idx >= 0 {
		return host[:idx]
	}
	return host
}

// isNumericPrivateIPv4 detects numeric IPv4 representations that resolve to
// private/loopback addresses. Handles: octal octets (0177.0.0.1),
// hex integer (0x7f000001), and single decimal integer (2130706433).
// Ported from Rust core-rs/core/src/security/mod.rs:273-316.
func isNumericPrivateIPv4(host string) bool {
	// Single decimal integer (e.g., 2130706433 = 127.0.0.1).
	if num, err := strconv.ParseUint(host, 10, 32); err == nil {
		return isPrivateIPv4U32(uint32(num))
	}

	// Hex integer (e.g., 0x7f000001).
	if len(host) > 2 && (host[:2] == "0x" || host[:2] == "0X") {
		if num, err := strconv.ParseUint(host[2:], 16, 32); err == nil {
			return isPrivateIPv4U32(uint32(num))
		}
	}

	// Octal/mixed-radix dotted notation (e.g., 0177.0.0.01).
	parts := strings.Split(host, ".")
	if len(parts) == 4 {
		var octets [4]byte
		allParsed := true
		for i, part := range parts {
			if part == "" {
				allParsed = false
				break
			}
			v, ok := parseOctetMixedRadix(part)
			if !ok {
				allParsed = false
				break
			}
			octets[i] = v
		}
		if allParsed {
			// Only flag if notation has octal/hex octets (differs from plain decimal).
			hasNonDecimal := false
			for _, p := range parts {
				if len(p) > 2 && (p[:2] == "0x" || p[:2] == "0X") {
					hasNonDecimal = true
					break
				}
				if len(p) > 1 && p[0] == '0' && isAllDigits(p) {
					hasNonDecimal = true
					break
				}
			}
			if hasNonDecimal {
				ip := uint32(octets[0])<<24 | uint32(octets[1])<<16 | uint32(octets[2])<<8 | uint32(octets[3])
				return isPrivateIPv4U32(ip)
			}
		}
	}

	return false
}

// parseOctetMixedRadix parses a single octet that may be decimal, octal
// (0-prefix), or hex (0x-prefix). Matches Rust parse_octet_mixed_radix.
func parseOctetMixedRadix(s string) (byte, bool) {
	if len(s) == 0 {
		return 0, false
	}
	// Hex: 0x or 0X prefix.
	if len(s) > 2 && (s[:2] == "0x" || s[:2] == "0X") {
		v, err := strconv.ParseUint(s[2:], 16, 8)
		if err != nil {
			return 0, false
		}
		return byte(v), true
	}
	// Octal: leading 0 with all digits.
	if len(s) > 1 && s[0] == '0' && isAllDigits(s) {
		v, err := strconv.ParseUint(s, 8, 8)
		if err != nil {
			return 0, false
		}
		return byte(v), true
	}
	// Decimal.
	v, err := strconv.ParseUint(s, 10, 8)
	if err != nil {
		return 0, false
	}
	return byte(v), true
}

// isPrivateIPv4U32 checks if a 32-bit IPv4 address falls in private/loopback/
// link-local ranges. Matches Rust is_private_ipv4_u32.
func isPrivateIPv4U32(ip uint32) bool {
	a := byte(ip >> 24)
	b := byte(ip >> 16)

	switch {
	case a == 127: // 127.0.0.0/8 (loopback)
		return true
	case ip == 0: // 0.0.0.0
		return true
	case a == 10: // 10.0.0.0/8
		return true
	case a == 172 && b >= 16 && b <= 31: // 172.16.0.0/12
		return true
	case a == 192 && b == 168: // 192.168.0.0/16
		return true
	case a == 169 && b == 254: // 169.254.0.0/16 (link-local / cloud metadata)
		return true
	case a == 100 && b >= 64 && b <= 127: // 100.64.0.0/10 (CGNAT)
		return true
	}
	return false
}

func isAllDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}
