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
import (
	"context"
	"errors"
	"unsafe"
)

const vegaOutBufSize = 1024 * 1024 // 1 MB default output buffer (grows on demand up to 16 MB)

// VegaExecuteCtx executes a Vega command via the Rust FFI, respecting context cancellation.
func VegaExecuteCtx(ctx context.Context, cmd string) ([]byte, error) {
	if len(cmd) == 0 {
		return nil, errors.New("ffi: vega_execute: empty input")
	}
	cmdPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(cmd)))
	return ffiCallWithGrowCtx(ctx, "vega_execute", vegaOutBufSize,
		func(outPtr unsafe.Pointer, outLen int) int {
			return int(C.deneb_vega_execute(
				cmdPtr, C.ulong(len(cmd)),
				(*C.uchar)(outPtr), C.ulong(outLen),
			))
		})
}

// VegaSearchCtx executes a Vega search query via the Rust FFI, respecting context cancellation.
func VegaSearchCtx(ctx context.Context, query string) ([]byte, error) {
	if len(query) == 0 {
		return nil, errors.New("ffi: vega_search: empty input")
	}
	queryPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(query)))
	return ffiCallWithGrowCtx(ctx, "vega_search", vegaOutBufSize,
		func(outPtr unsafe.Pointer, outLen int) int {
			return int(C.deneb_vega_search(
				queryPtr, C.ulong(len(query)),
				(*C.uchar)(outPtr), C.ulong(outLen),
			))
		})
}

// VegaExecute executes a Vega command via the Rust FFI.
func VegaExecute(cmd string) ([]byte, error) {
	return VegaExecuteCtx(context.Background(), cmd)
}

// VegaSearch executes a Vega search query via the Rust FFI.
func VegaSearch(query string) ([]byte, error) {
	return VegaSearchCtx(context.Background(), query)
}
