//go:build !no_ffi && cgo

package ffi

/*
// Static linking avoids LD_LIBRARY_PATH issues at runtime.
#cgo LDFLAGS: ${SRCDIR}/../../../core-rs/target/release/libdeneb_core.a -lm -ldl -lpthread
#cgo darwin LDFLAGS: -framework Security
#cgo CFLAGS: -I${SRCDIR}

// Deneb core FFI functions (from core-rs/src/lib.rs).
extern int deneb_validate_frame(const unsigned char *json_ptr, unsigned long json_len);
extern int deneb_constant_time_eq(
	const unsigned char *a_ptr, unsigned long a_len,
	const unsigned char *b_ptr, unsigned long b_len);
extern int deneb_detect_mime(
	const unsigned char *data_ptr, unsigned long data_len,
	unsigned char *out_ptr, unsigned long out_len);
extern int deneb_validate_session_key(const unsigned char *key_ptr, unsigned long key_len);
extern int deneb_sanitize_html(
	const unsigned char *input_ptr, unsigned long input_len,
	unsigned char *out_ptr, unsigned long out_len);
extern int deneb_is_safe_url(const unsigned char *url_ptr, unsigned long url_len);
extern int deneb_validate_error_code(const unsigned char *code_ptr, unsigned long code_len);
*/
import "C"
import (
	"errors"
	"unsafe"
)

// Available reports whether the Rust FFI library is linked.
const Available = true

// ValidateFrame validates a gateway frame JSON string using the Rust
// protocol validator. Returns nil if valid.
func ValidateFrame(json string) error {
	if len(json) == 0 {
		return errors.New("ffi: empty JSON input")
	}
	ptr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(json)))
	rc := C.deneb_validate_frame(ptr, C.ulong(len(json)))
	switch rc {
	case 0:
		return nil
	case -1:
		return errors.New("ffi: null pointer")
	case -2:
		return errors.New("ffi: invalid UTF-8")
	case -3:
		return errors.New("ffi: frame validation failed")
	default:
		return errors.New("ffi: unknown error")
	}
}

// ConstantTimeEq performs a constant-time comparison of two byte slices.
// Uses the Rust implementation to prevent timing side-channel attacks.
func ConstantTimeEq(a, b []byte) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	var aPtr, bPtr *C.uchar
	if len(a) > 0 {
		aPtr = (*C.uchar)(unsafe.Pointer(&a[0]))
	}
	if len(b) > 0 {
		bPtr = (*C.uchar)(unsafe.Pointer(&b[0]))
	}
	rc := C.deneb_constant_time_eq(aPtr, C.ulong(len(a)), bPtr, C.ulong(len(b)))
	return rc == 0
}

// DetectMIME identifies the MIME type of the given data by inspecting
// magic bytes. Returns "application/octet-stream" for unknown formats.
func DetectMIME(data []byte) string {
	if len(data) == 0 {
		return "application/octet-stream"
	}
	var out [128]byte
	dataPtr := (*C.uchar)(unsafe.Pointer(&data[0]))
	outPtr := (*C.uchar)(unsafe.Pointer(&out[0]))
	n := C.deneb_detect_mime(dataPtr, C.ulong(len(data)), outPtr, C.ulong(len(out)))
	if n <= 0 {
		return "application/octet-stream"
	}
	return string(out[:n])
}

// ValidateSessionKey checks if a session key is valid (non-empty, max 512 chars,
// no control characters). Returns nil if valid.
func ValidateSessionKey(key string) error {
	if len(key) == 0 {
		return errors.New("ffi: empty session key")
	}
	ptr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(key)))
	rc := C.deneb_validate_session_key(ptr, C.ulong(len(key)))
	switch rc {
	case 0:
		return nil
	case -1:
		return errors.New("ffi: null pointer")
	case -2:
		return errors.New("ffi: invalid UTF-8")
	case -3:
		return errors.New("ffi: invalid session key")
	default:
		return errors.New("ffi: unknown error")
	}
}

// maxSanitizeInputBytes is the maximum input size for SanitizeHTML (1 MB).
// Prevents OOM from pathologically large inputs multiplied by 6x expansion.
const maxSanitizeInputBytes = 1 * 1024 * 1024

// SanitizeHTML escapes HTML-significant characters in the input.
// Inputs exceeding 1 MB are returned unmodified to prevent OOM.
func SanitizeHTML(input string) string {
	if len(input) == 0 {
		return ""
	}
	if len(input) > maxSanitizeInputBytes {
		return input // safety limit: return original for oversized input
	}
	// Output can be up to 6x input size (each char could become &#x27;)
	outSize := len(input) * 6
	out := make([]byte, outSize)
	ptr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(input)))
	outPtr := (*C.uchar)(unsafe.Pointer(&out[0]))
	n := C.deneb_sanitize_html(ptr, C.ulong(len(input)), outPtr, C.ulong(outSize))
	if n <= 0 {
		return input // fallback: return original on error
	}
	return string(out[:n])
}

// IsSafeURL checks if a URL is safe for outbound requests (not targeting
// internal/private networks). Returns true if safe.
func IsSafeURL(url string) bool {
	if len(url) == 0 {
		return false
	}
	ptr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(url)))
	rc := C.deneb_is_safe_url(ptr, C.ulong(len(url)))
	return rc == 0
}

// ValidateErrorCode checks if an error code string is a known gateway error code.
func ValidateErrorCode(code string) bool {
	if len(code) == 0 {
		return false
	}
	ptr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(code)))
	rc := C.deneb_validate_error_code(ptr, C.ulong(len(code)))
	return rc == 0
}
