//go:build !no_ffi && cgo

package ffi

/*
// ML FFI functions (from core-rs/core/src/lib.rs).
extern int deneb_ml_embed(
	const unsigned char *input_ptr, unsigned long input_len,
	unsigned char *out_ptr, unsigned long out_len);
extern int deneb_ml_rerank(
	const unsigned char *input_ptr, unsigned long input_len,
	unsigned char *out_ptr, unsigned long out_len);
*/
import "C"
import (
	"errors"
	"unsafe"
)

// mlEmbedBufSize is the initial output buffer for embeddings.
// Sized based on input: embedding vectors are ~4KB per text, so we estimate
// based on input count. Uses auto-grow via ffiCallWithGrow.
const mlEmbedBufBase = 64 * 1024 // 64 KB base (enough for ~16 embeddings)

// MLEmbed generates text embeddings via the Rust FFI.
// Takes a JSON request (text array), returns JSON result (vectors).
// Output buffer grows automatically for large batches.
func MLEmbed(input string) ([]byte, error) {
	if len(input) == 0 {
		return nil, errors.New("ffi: ml_embed: empty input")
	}
	// Estimate output size: ~4KB per text in batch. Input JSON contains the
	// texts, so rough text count ≈ input_len / 50. Minimum 64 KB.
	estimatedTexts := len(input) / 50
	if estimatedTexts < 1 {
		estimatedTexts = 1
	}
	initialSize := estimatedTexts * 4096
	if initialSize < mlEmbedBufBase {
		initialSize = mlEmbedBufBase
	}
	inputPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(input)))
	return ffiCallWithGrow("ml_embed", initialSize,
		func(outPtr unsafe.Pointer, outLen int) int {
			return int(C.deneb_ml_embed(
				inputPtr, C.ulong(len(input)),
				(*C.uchar)(outPtr), C.ulong(outLen),
			))
		})
}

// MLRerank reranks documents against a query via the Rust FFI.
// Takes a JSON request (query + documents), returns JSON ranked results.
// Output buffer grows automatically.
func MLRerank(input string) ([]byte, error) {
	if len(input) == 0 {
		return nil, errors.New("ffi: ml_rerank: empty input")
	}
	// Rerank output is small (~100 bytes per doc). 8 KB is usually sufficient.
	initialSize := 8192
	inputPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(input)))
	return ffiCallWithGrow("ml_rerank", initialSize,
		func(outPtr unsafe.Pointer, outLen int) int {
			return int(C.deneb_ml_rerank(
				inputPtr, C.ulong(len(input)),
				(*C.uchar)(outPtr), C.ulong(outLen),
			))
		})
}
