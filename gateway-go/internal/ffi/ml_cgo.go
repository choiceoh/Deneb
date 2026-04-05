//go:build !no_ffi && cgo

package ffi

/*
// ML FFI functions (from core-rs/core/src/ffi/ml.rs).
extern int deneb_ml_embed(
	const unsigned char *input_ptr, unsigned long input_len,
	unsigned char *out_ptr, unsigned long out_len);
*/
import "C"
import (
	"context"
	"errors"
	"unsafe"
)

// 256 KB initial buffer (1024 dims * 4 bytes/float * ~64 texts).
const mlOutBufSize = 256 * 1024

// MLEmbedCtx embeds texts using a local GGUF model via Rust FFI, respecting context cancellation.
func MLEmbedCtx(ctx context.Context, inputJSON string) ([]byte, error) {
	if len(inputJSON) == 0 {
		return nil, errors.New("ffi: ml_embed: empty input")
	}
	inputPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(inputJSON)))
	return ffiCallWithGrowCtx(ctx, "ml_embed", mlOutBufSize,
		func(outPtr unsafe.Pointer, outLen int) int {
			return int(C.deneb_ml_embed(
				inputPtr, C.ulong(len(inputJSON)),
				(*C.uchar)(outPtr), C.ulong(outLen),
			))
		})
}

// MLEmbed embeds texts using a local GGUF model via Rust FFI.
func MLEmbed(inputJSON string) ([]byte, error) {
	return MLEmbedCtx(context.Background(), inputJSON)
}
