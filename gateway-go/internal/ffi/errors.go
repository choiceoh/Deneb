package ffi

import (
	"context"
	"fmt"
	"sync"
	"unsafe"
)

// ErrFFITimeout is returned when an FFI call exceeds its context deadline.
var ErrFFITimeout = fmt.Errorf("ffi: call timed out")

// FFI return codes are defined in ffi_error_codes_gen.go (auto-generated from
// proto/gateway.proto FfiErrorCode enum). To add or modify a code, edit
// gateway.proto and run: make error-codes-gen

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

// ffiOutPool1MB pools 1 MB byte slices for FFI output buffers used by
// compaction and context engine calls that repeat at short intervals.
var ffiOutPool1MB = sync.Pool{
	New: func() any { return make([]byte, 1024*1024) },
}

// initialBufSize computes a starting buffer size for FFI output.
// multiplier scales the input length, floor sets the minimum size.
// The result is capped at maxGrowBufSize.
func initialBufSize(inputLen, multiplier, floor int) int {
	size := inputLen * multiplier
	if size < floor {
		size = floor
	}
	if size > maxGrowBufSize {
		size = maxGrowBufSize
	}
	return size
}

// ffiCallWithPool is like ffiCallWithGrow but reuses buffers from a sync.Pool
// for the initial attempt. The result is copied to a right-sized slice so the
// pooled buffer can be returned safely. Falls back to ffiCallWithGrow if the
// pooled buffer is too small.
func ffiCallWithPool(fn string, pool *sync.Pool, call func(outPtr unsafe.Pointer, outLen int) int) ([]byte, error) {
	buf := pool.Get().([]byte)
	var outPtr unsafe.Pointer
	if len(buf) > 0 {
		outPtr = unsafe.Pointer(&buf[0])
	}
	rc := call(outPtr, len(buf))
	if rc >= 0 {
		result := make([]byte, rc)
		copy(result, buf[:rc])
		pool.Put(buf)
		return result, nil
	}
	// Read panic message immediately after C returns, while still on the
	// same OS thread. Thread-local storage becomes unreliable after any
	// Go scheduling point.
	panicMsg := ""
	if rc == rcRustPanic {
		panicMsg = getLastPanicMsg()
	}
	pool.Put(buf) // return before fallback to avoid holding two large buffers
	if rc == rcOutputTooSmall {
		return ffiCallWithGrow(fn, len(buf)*2, call)
	}
	if panicMsg != "" {
		return nil, fmt.Errorf("ffi: %s: rust panic: %s", fn, panicMsg)
	}
	return nil, ffiError(fn, rc)
}

// ffiCallWithGrowCtx is like ffiCallWithGrow but respects context cancellation.
// The FFI call runs in a separate goroutine; if ctx is canceled before the call
// returns, ErrFFITimeout is returned immediately. The underlying FFI call still
// runs to completion (cannot interrupt C code) but its result is discarded.
func ffiCallWithGrowCtx(ctx context.Context, fn string, initialSize int, call func(outPtr unsafe.Pointer, outLen int) int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrFFITimeout, fn, err)
	}
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		d, e := ffiCallWithGrow(fn, initialSize, call)
		ch <- result{d, e}
	}()
	select {
	case r := <-ch:
		return r.data, r.err
	case <-ctx.Done():
		return nil, fmt.Errorf("%w: %s: %v", ErrFFITimeout, fn, ctx.Err())
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
		// Read panic message immediately after C returns, while still on
		// the same OS thread. Thread-local storage becomes unreliable
		// after any Go scheduling point.
		if rc == rcRustPanic {
			if msg := getLastPanicMsg(); msg != "" {
				return nil, fmt.Errorf("ffi: %s: rust panic: %s", fn, msg)
			}
		}
		if rc == rcOutputTooSmall && size < maxGrowBufSize {
			size *= 2
			continue
		}
		return nil, ffiError(fn, rc)
	}
}
