package ffi

import "fmt"

// ffiError maps negative FFI return codes to Go errors.
// Shared across all CGo wrapper files.
func ffiError(fn string, rc int) error {
	switch rc {
	case -1:
		return fmt.Errorf("ffi: %s: null pointer", fn)
	case -2:
		return fmt.Errorf("ffi: %s: invalid UTF-8", fn)
	case -3:
		return fmt.Errorf("ffi: %s: validation failed", fn)
	case -4:
		return fmt.Errorf("ffi: %s: input too large", fn)
	case -5:
		return fmt.Errorf("ffi: %s: JSON error", fn)
	case -6:
		return fmt.Errorf("ffi: %s: output buffer too small", fn)
	case -99:
		return fmt.Errorf("ffi: %s: rust panic", fn)
	default:
		return fmt.Errorf("ffi: %s: unknown error (rc=%d)", fn, rc)
	}
}
