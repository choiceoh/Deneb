package ffi

import "fmt"

// FFI return codes from core-rs C ABI functions.
const (
	rcNullPointer    = -1
	rcInvalidUTF8    = -2
	rcValidationFail = -3
	rcInputTooLarge  = -4
	rcJSONError      = -5
	rcBufferTooSmall = -6
	rcRustPanic      = -99
)

// ffiError maps negative FFI return codes to Go errors.
// Shared across all CGo wrapper files.
func ffiError(fn string, rc int) error {
	switch rc {
	case rcNullPointer:
		return fmt.Errorf("ffi: %s: null pointer", fn)
	case rcInvalidUTF8:
		return fmt.Errorf("ffi: %s: invalid UTF-8", fn)
	case rcValidationFail:
		return fmt.Errorf("ffi: %s: validation failed", fn)
	case rcInputTooLarge:
		return fmt.Errorf("ffi: %s: input too large", fn)
	case rcJSONError:
		return fmt.Errorf("ffi: %s: JSON error", fn)
	case rcBufferTooSmall:
		return fmt.Errorf("ffi: %s: output buffer too small", fn)
	case rcRustPanic:
		return fmt.Errorf("ffi: %s: rust panic", fn)
	default:
		return fmt.Errorf("ffi: %s: unknown error (rc=%d)", fn, rc)
	}
}
