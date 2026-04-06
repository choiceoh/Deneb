//go:build no_ffi || !cgo

package ffi

import (
	"encoding/json"
	"errors"

	"github.com/choiceoh/deneb/gateway-go/internal/coresecurity"
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

// ConstantTimeEq delegates to coresecurity.ConstantTimeEq.
func ConstantTimeEq(a, b []byte) bool {
	return coresecurity.ConstantTimeEq(a, b)
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

// ValidateSessionKey delegates to coresecurity.ValidateSessionKey.
func ValidateSessionKey(key string) error {
	return coresecurity.ValidateSessionKey(key)
}

// SanitizeHTML delegates to coresecurity.SanitizeHTML.
func SanitizeHTML(input string) string {
	return coresecurity.SanitizeHTML(input)
}

// IsSafeURL delegates to coresecurity.IsSafeURL.
func IsSafeURL(rawURL string) bool {
	return coresecurity.IsSafeURL(rawURL)
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

// getLastPanicMsg is a no-op in pure-Go builds (no Rust panic to retrieve).
func getLastPanicMsg() string {
	return ""
}
