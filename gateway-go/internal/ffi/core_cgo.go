//go:build !no_ffi && cgo

package ffi

/*
// Static linking avoids LD_LIBRARY_PATH issues at runtime.
#cgo LDFLAGS: ${SRCDIR}/../../../core-rs/target/release/libdeneb_core.a -lm -ldl -lpthread -lstdc++ -lgomp
#cgo CFLAGS: -I${SRCDIR}

// Deneb core FFI functions — protocol validation + schema (not yet ported to Go).
extern int deneb_validate_frame(const unsigned char *json_ptr, unsigned long json_len);
extern int deneb_validate_error_code(const unsigned char *code_ptr, unsigned long code_len);
extern int deneb_validate_params(
	const unsigned char *method_ptr, unsigned long method_len,
	const unsigned char *json_ptr, unsigned long json_len,
	unsigned char *errors_out, unsigned long errors_out_len);
extern int deneb_get_last_panic_msg(
	unsigned char *out_ptr, unsigned long out_len);
*/
import "C"
import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/choiceoh/deneb/gateway-go/internal/coremedia"
	"github.com/choiceoh/deneb/gateway-go/internal/coresecurity"
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
	case rcNullPointer:
		return errors.New("ffi: null pointer")
	case rcInvalidUTF8:
		return errors.New("ffi: invalid UTF-8")
	case rcValidation:
		return errors.New("ffi: frame validation failed")
	default:
		return errors.New("ffi: unknown error")
	}
}

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

// maxErrorsBufSize is the buffer size for validation error JSON output (64 KB).
const maxErrorsBufSize = 64 * 1024

// ValidateParams validates RPC parameters for a given method name using the
// Rust schema validators. Returns nil if valid, or the raw JSON error array.
func ValidateParams(method, json string) (valid bool, errorsJSON []byte, err error) {
	if len(method) == 0 {
		return false, nil, errors.New("ffi: empty method name")
	}
	if len(json) == 0 {
		return false, nil, errors.New("ffi: empty JSON input")
	}
	methodPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(method)))
	jsonPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(json)))
	var out [maxErrorsBufSize]byte
	outPtr := (*C.uchar)(unsafe.Pointer(&out[0]))
	rc := C.deneb_validate_params(
		methodPtr, C.ulong(len(method)),
		jsonPtr, C.ulong(len(json)),
		outPtr, C.ulong(len(out)),
	)
	switch {
	case rc == 0:
		return true, nil, nil
	case rc == rcNullPointer:
		return false, nil, errors.New("ffi: null pointer")
	case rc == rcInvalidUTF8:
		return false, nil, errors.New("ffi: invalid UTF-8")
	case rc == rcValidation:
		return false, nil, errors.New("ffi: unknown method")
	case rc == rcInputTooLarge:
		return false, nil, errors.New("ffi: input too large")
	case rc == rcJSONError:
		return false, nil, errors.New("ffi: invalid JSON")
	case rc > 0:
		// rc is the number of bytes written to the output buffer (JSON error array).
		if int(rc) > maxErrorsBufSize {
			return false, nil, fmt.Errorf("ffi: validate_params: output (%d bytes) exceeds buffer (%d bytes)", rc, maxErrorsBufSize)
		}
		return false, out[:rc], nil
	default:
		return false, nil, errors.New("ffi: unknown error")
	}
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

// getLastPanicMsg retrieves the panic message from the most recent Rust panic
// on the current OS thread. Returns empty string if no panic was recorded.
func getLastPanicMsg() string {
	var buf [4096]byte
	rc := C.deneb_get_last_panic_msg((*C.uchar)(unsafe.Pointer(&buf[0])), C.ulong(len(buf)))
	if rc <= 0 {
		return ""
	}
	return string(buf[:rc])
}
