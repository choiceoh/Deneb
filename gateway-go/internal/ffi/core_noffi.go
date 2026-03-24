//go:build no_ffi || !cgo

package ffi

import (
	"encoding/json"
	"errors"
	"strings"
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
