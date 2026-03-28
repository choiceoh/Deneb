package ffi

import (
	"fmt"
	"unsafe"
)

// FFI return codes from core-rs C ABI functions.
// These MUST stay in sync with the Rust definitions in:
//   - core-rs/core/src/lib.rs          (C ABI return values)
//   - core-rs/core/src/protocol/error_codes.rs (canonical Rust error codes)
// When adding a new code here, mirror it in both Rust files and run:
//   make error-code-sync
const (
	rcNullPointer    = -1
	rcInvalidUTF8    = -2
	rcOutputTooSmall = -3
	rcInputTooLarge  = -4
	rcJSONError      = -5
	rcOverflow       = -6
	rcValidation     = -7
	rcRustPanic      = -99
)

// maxGrowBufSize is the upper limit for automatic buffer growth (16 MB).
const maxGrowBufSize = 16 * 1024 * 1024

// ffiError maps negative FFI return codes to Go errors.
// Shared across all CGo wrapper files.
func ffiError(fn string, rc int) error {
	switch rc {
	case rcNullPointer:
		return fmt.Errorf("ffi: %s: null pointer", fn)
	case rcInvalidUTF8:
		return fmt.Errorf("ffi: %s: invalid UTF-8", fn)
	case rcOutputTooSmall:
		return fmt.Errorf("ffi: %s: output buffer too small", fn)
	case rcInputTooLarge:
		return fmt.Errorf("ffi: %s: input too large", fn)
	case rcJSONError:
		return fmt.Errorf("ffi: %s: JSON error", fn)
	case rcOverflow:
		return fmt.Errorf("ffi: %s: overflow", fn)
	case rcValidation:
		return fmt.Errorf("ffi: %s: validation failed", fn)
	case rcRustPanic:
		return fmt.Errorf("ffi: %s: rust panic", fn)
	default:
		return fmt.Errorf("ffi: %s: unknown error (rc=%d)", fn, rc)
	}
}

// ffiCallWithGrow calls an FFI function that writes into an output buffer,
// automatically growing the buffer and retrying when the Rust side returns
// rcOutputTooSmall. The buffer doubles each retry up to maxGrowBufSize (16 MB).
// initialSize is capped at maxGrowBufSize so callers with large computed sizes
// don't skip directly to the ceiling on the first attempt.
//
// The call function receives the output buffer and must return the FFI return
// code: positive = bytes written, negative = error code.
func ffiCallWithGrow(fn string, initialSize int, call func(outPtr unsafe.Pointer, outLen int) int) ([]byte, error) {
	if initialSize > maxGrowBufSize {
		initialSize = maxGrowBufSize
	}
	size := initialSize
	for {
		out := make([]byte, size)
		var outPtr unsafe.Pointer
		if size > 0 {
			outPtr = unsafe.Pointer(&out[0])
		}
		rc := call(outPtr, size)
		if rc >= 0 {
			return out[:rc], nil
		}
		if rc == rcOutputTooSmall && size < maxGrowBufSize {
			size *= 2
			continue
		}
		return nil, ffiError(fn, rc)
	}
}
