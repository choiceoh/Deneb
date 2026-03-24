//go:build no_ffi || !cgo

package ffi

import (
	"encoding/json"
	"errors"
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

// ConstantTimeEq is a pure-Go fallback using XOR accumulation.
func ConstantTimeEq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// DetectMIME is a pure-Go fallback for common MIME types.
func DetectMIME(data []byte) string {
	if len(data) < 4 {
		return "application/octet-stream"
	}
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png"
	}
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	if string(data[:3]) == "GIF" {
		return "image/gif"
	}
	if string(data[:4]) == "%PDF" {
		return "application/pdf"
	}
	if data[0] == 0x50 && data[1] == 0x4B && data[2] == 0x03 && data[3] == 0x04 {
		return "application/zip"
	}
	s := strings.TrimSpace(string(data[:1]))
	if s == "{" || s == "[" {
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

// blockedHosts are hostnames that should not be accessed.
var blockedHosts = map[string]bool{
	"localhost":                true,
	"127.0.0.1":               true,
	"0.0.0.0":                 true,
	"[::1]":                   true,
	"metadata.google.internal": true,
	"169.254.169.254":         true,
}

// IsSafeURL is a pure-Go fallback for SSRF URL validation.
func IsSafeURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
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
	// Block private IP ranges.
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

// ValidateErrorCode is a pure-Go fallback for error code validation.
func ValidateErrorCode(code string) bool {
	return knownErrorCodes[code]
}
