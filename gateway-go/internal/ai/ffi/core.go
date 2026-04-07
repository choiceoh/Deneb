package ffi

import (
	"github.com/choiceoh/deneb/gateway-go/internal/core/coremedia"
	"github.com/choiceoh/deneb/gateway-go/internal/core/coresecurity"
)

// ConstantTimeEq delegates to coresecurity.ConstantTimeEq (pure Go, crypto/subtle).
func ConstantTimeEq(a, b []byte) bool {
	return coresecurity.ConstantTimeEq(a, b)
}

// DetectMIME delegates to coremedia.DetectMIME (pure Go, zero alloc).
func DetectMIME(data []byte) string {
	return coremedia.DetectMIME(data)
}

// ValidateSessionKey delegates to coresecurity.ValidateSessionKey (pure Go).
func ValidateSessionKey(key string) error {
	return coresecurity.ValidateSessionKey(key)
}

// SanitizeHTML delegates to coresecurity.SanitizeHTML (pure Go).
func SanitizeHTML(input string) string {
	return coresecurity.SanitizeHTML(input)
}

// IsSafeURL delegates to coresecurity.IsSafeURL (pure Go).
func IsSafeURL(rawURL string) bool {
	return coresecurity.IsSafeURL(rawURL)
}
