//go:build !no_ffi && cgo

package ffi

/*
// Vega FFI functions (from core-rs/core/src/lib.rs).
extern int deneb_vega_execute(
	const unsigned char *cmd_ptr, unsigned long cmd_len,
	unsigned char *out_ptr, unsigned long out_len);
extern int deneb_vega_search(
	const unsigned char *query_ptr, unsigned long query_len,
	unsigned char *out_ptr, unsigned long out_len);
*/
import "C"
import "unsafe"

const vegaOutBufSize = 64 * 1024 // 64 KB output buffer

// VegaExecute executes a Vega command via the Rust FFI.
// Returns the raw JSON response bytes.
func VegaExecute(cmd string) ([]byte, error) {
	if len(cmd) == 0 {
		return nil, ffiError("vega_execute", -1)
	}
	cmdPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(cmd)))
	out := make([]byte, vegaOutBufSize)
	outPtr := (*C.uchar)(unsafe.Pointer(&out[0]))

	rc := C.deneb_vega_execute(cmdPtr, C.ulong(len(cmd)), outPtr, C.ulong(len(out)))
	if rc < 0 {
		return nil, ffiError("vega_execute", int(rc))
	}
	return out[:rc], nil
}

// VegaSearch executes a Vega search query via the Rust FFI.
// Returns the raw JSON results bytes.
func VegaSearch(query string) ([]byte, error) {
	if len(query) == 0 {
		return nil, ffiError("vega_search", -1)
	}
	queryPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(query)))
	out := make([]byte, vegaOutBufSize)
	outPtr := (*C.uchar)(unsafe.Pointer(&out[0]))

	rc := C.deneb_vega_search(queryPtr, C.ulong(len(query)), outPtr, C.ulong(len(out)))
	if rc < 0 {
		return nil, ffiError("vega_search", int(rc))
	}
	return out[:rc], nil
}
