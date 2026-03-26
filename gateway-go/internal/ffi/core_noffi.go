//go:build no_ffi || !cgo

package ffi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// Available reports whether the Rust FFI library is linked.
const Available = false

// ValidateFrame is a pure-Go fallback for basic frame validation.
func ValidateFrame(jsonStr string) error {
	if len(jsonStr) == 0 {
		return errors.New("ffi: empty JSON input")
	}
	var raw struct {
		Type   string `json:"type"`
		ID     string `json:"id"`
		Method string `json:"method"`
		Event  string `json:"event"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return errors.New("ffi: invalid JSON")
	}
	switch raw.Type {
	case "req":
		if raw.ID == "" || raw.Method == "" {
			return errors.New("ffi: request frame missing id or method")
		}
	case "res":
		if raw.ID == "" {
			return errors.New("ffi: response frame missing id")
		}
	case "event":
		if raw.Event == "" {
			return errors.New("ffi: event frame missing event name")
		}
	default:
		return errors.New("ffi: unknown frame type")
	}
	return nil
}

// ConstantTimeEq is a pure-Go fallback using crypto/subtle.
// Unlike a naive XOR loop, subtle.ConstantTimeCompare handles length
// mismatches in constant time, preventing timing side-channel leaks.
func ConstantTimeEq(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// DetectMIME is a pure-Go fallback for common MIME types.
// Uses first-byte dispatch to minimize comparisons (zero alloc).
func DetectMIME(data []byte) string {
	if len(data) < 4 {
		return "application/octet-stream"
	}
	switch data[0] {
	case 0x89:
		if data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
			return "image/png"
		}
	case 0xFF:
		if data[1] == 0xD8 && data[2] == 0xFF {
			return "image/jpeg"
		}
	case 'G':
		if data[1] == 'I' && data[2] == 'F' {
			return "image/gif"
		}
	case '%':
		if data[1] == 'P' && data[2] == 'D' && data[3] == 'F' {
			return "application/pdf"
		}
	case 0x50: // PK
		if data[1] == 0x4B && data[2] == 0x03 && data[3] == 0x04 {
			return "application/zip"
		}
	case '{', '[':
		return "application/json"
	}
	return "application/octet-stream"
}

// ValidateSessionKey is a pure-Go fallback for session key validation.
// Single-pass: counts runes and checks for control chars simultaneously.
func ValidateSessionKey(key string) error {
	if len(key) == 0 {
		return errors.New("ffi: empty session key")
	}
	count := 0
	for _, r := range key {
		count++
		if count > 512 {
			return errors.New("ffi: session key too long")
		}
		if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			return errors.New("ffi: invalid session key")
		}
	}
	return nil
}

// SanitizeHTML is a pure-Go fallback for HTML sanitization.
func SanitizeHTML(input string) string {
	// Fast path: no special chars — return as-is (zero alloc).
	if !strings.ContainsAny(input, "<>&\"'") {
		return input
	}
	var b strings.Builder
	b.Grow(len(input) + len(input)/4)
	for _, r := range input {
		switch r {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#x27;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// blockedHosts are hostnames that should not be accessed (SSRF protection).
var blockedHosts = map[string]bool{
	"localhost":                true,
	"127.0.0.1":                true,
	"0.0.0.0":                  true,
	"[::1]":                    true,
	"::1":                      true,
	"metadata.google.internal": true,
	"169.254.169.254":          true,
}

// blockedSchemes are URL schemes that should never be followed.
var blockedSchemes = map[string]bool{
	"file": true, "ftp": true, "gopher": true, "dict": true, "data": true,
	"ldap": true, "ldaps": true, "tftp": true, "telnet": true,
}

// IsSafeURL is a pure-Go fallback for SSRF URL validation.
// Blocks private/loopback IPs, cloud metadata endpoints, and dangerous schemes.
func IsSafeURL(rawURL string) bool {
	// Explicit UNC path blocking (defense-in-depth).
	if strings.HasPrefix(rawURL, "\\\\") || (strings.HasPrefix(rawURL, "//") && !strings.Contains(rawURL, "://")) {
		return false
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if blockedSchemes[scheme] {
		return false
	}
	if scheme != "http" && scheme != "https" {
		return false
	}
	// url.Hostname() already strips userinfo and port.
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return false
	}
	if blockedHosts[host] {
		return false
	}

	// Parse as IP to check private/reserved ranges.
	ip := net.ParseIP(host)
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

	// Host is a hostname — check common private IP prefixes as strings.
	if strings.HasPrefix(host, "10.") || strings.HasPrefix(host, "192.168.") {
		return false
	}
	if strings.HasPrefix(host, "172.") {
		parts := strings.SplitN(host, ".", 3)
		if len(parts) >= 2 {
			if n, err := strconv.Atoi(parts[1]); err == nil && n >= 16 && n <= 31 {
				return false
			}
		}
	}
	// Block link-local range 169.254.x.x.
	if strings.HasPrefix(host, "169.254.") {
		return false
	}
	// Block CGNAT range 100.64-127.x.x.
	if strings.HasPrefix(host, "100.") {
		parts := strings.SplitN(host, ".", 3)
		if len(parts) >= 2 {
			if n, err := strconv.Atoi(parts[1]); err == nil && n >= 64 && n <= 127 {
				return false
			}
		}
	}
	return true
}

// knownErrorCodes contains all valid gateway error codes.
var knownErrorCodes = map[string]bool{
	"NOT_LINKED": true, "NOT_PAIRED": true, "AGENT_TIMEOUT": true,
	"INVALID_REQUEST": true, "UNAVAILABLE": true, "MISSING_PARAM": true,
	"NOT_FOUND": true, "UNAUTHORIZED": true, "VALIDATION_FAILED": true,
	"CONFLICT": true, "FORBIDDEN": true, "NODE_DISCONNECTED": true,
	"DEPENDENCY_FAILED": true, "FEATURE_DISABLED": true,
}

// ValidateParams is a pure-Go fallback that always returns an error.
// Schema validation requires the Rust FFI library (jsonschema crate).
// In no_ffi builds, callers should treat all params as unvalidated and
// rely on application-level validation instead.
func ValidateParams(method, jsonStr string) (valid bool, errorsJSON []byte, err error) {
	return false, nil, errors.New("ffi: schema validation requires Rust FFI (not available in no_ffi build)")
}

// ValidateErrorCode is a pure-Go fallback for error code validation.
func ValidateErrorCode(code string) bool {
	return knownErrorCodes[code]
}
