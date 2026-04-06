//go:build no_ffi || !cgo

package ffi

import (
	"github.com/choiceoh/deneb/gateway-go/internal/coresecurity"
)

// Available reports whether the Rust FFI library is linked.
const Available = false

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
