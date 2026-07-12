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
	if isUNCPath(rawURL) {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if !isSafeWebScheme(parsed.Scheme) {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false
	}
	return isSafeURLHost(normalizeURLHost(host))
}

func isUNCPath(rawURL string) bool {
	return strings.HasPrefix(rawURL, "\\\\") ||
		(strings.HasPrefix(rawURL, "//") && !strings.Contains(rawURL, "://"))
}

func isSafeWebScheme(rawScheme string) bool {
	scheme := strings.ToLower(rawScheme)
	if _, blocked := blockedSchemes[scheme]; blocked {
		return false
	}
	return scheme == "http" || scheme == "https"
}

func normalizeURLHost(host string) string {
	withoutBrackets := strings.TrimLeft(strings.TrimRight(host, "]"), "[")
	return stripIPv6ZoneID(withoutBrackets)
}

func isSafeURLHost(host string) bool {
	if _, blocked := blockedHosts[host]; blocked {
		return false
	}
	if hasPrivateIPv6Prefix(host) {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return isPublicIP(ip)
	}
	return !hasPrivateIPv4Prefix(host) && !isNumericPrivateIPv4(host)
}

func hasPrivateIPv6Prefix(host string) bool {
	for _, prefix := range []string{
		"::ffff:127.", "::ffff:10.", "::ffff:192.168.", "::ffff:169.254.",
		"fc", "fd", "fe80",
	} {
		if strings.HasPrefix(host, prefix) {
			return true
		}
	}
	return false
}

func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return false
	}
	if ipv4 := ip.To4(); ipv4 != nil && ipv4[0] == 100 && ipv4[1] >= 64 && ipv4[1] <= 127 {
		return false
	}
	return true
}

func hasPrivateIPv4Prefix(host string) bool {
	if strings.HasPrefix(host, "10.") || strings.HasPrefix(host, "192.168.") ||
		strings.HasPrefix(host, "169.254.") {
		return true
	}
	if strings.HasPrefix(host, "172.") && secondIPv4OctetInRange(host, 16, 31) {
		return true
	}
	return strings.HasPrefix(host, "100.") && secondIPv4OctetInRange(host, 64, 127)
}

func secondIPv4OctetInRange(host string, low, high int) bool {
	parts := strings.SplitN(host, ".", 3)
	if len(parts) < 2 {
		return false
	}
	value, err := strconv.Atoi(parts[1])
	return err == nil && value >= low && value <= high
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
	if s == "" {
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
	a := byte(ip >> 24) //nolint:gosec // G115 — extracting individual bytes from uint32 IPv4 address
	b := byte(ip >> 16) //nolint:gosec // G115 — extracting individual bytes from uint32 IPv4 address

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
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}
