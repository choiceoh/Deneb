package ffi

import "fmt"

// ErrFFITimeout is returned when an FFI call exceeds its context deadline.
// Kept for API compatibility with callers that check this error.
var ErrFFITimeout = fmt.Errorf("ffi: call timed out")
