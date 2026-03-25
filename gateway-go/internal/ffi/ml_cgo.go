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
import "unsafe"

const mlOutBufSize = 256 * 1024 // 256 KB output buffer (embeddings can be large)

// MLEmbed generates text embeddings via the Rust FFI.
// Takes a JSON request (text array), returns JSON result (vectors).
func MLEmbed(input string) ([]byte, error) {
	if len(input) == 0 {
		return nil, ffiError("ml_embed", -1)
	}
	inputPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(input)))
	out := make([]byte, mlOutBufSize)
	outPtr := (*C.uchar)(unsafe.Pointer(&out[0]))

	rc := C.deneb_ml_embed(inputPtr, C.ulong(len(input)), outPtr, C.ulong(len(out)))
	if rc < 0 {
		return nil, ffiError("ml_embed", int(rc))
	}
	return out[:rc], nil
}

// MLRerank reranks documents against a query via the Rust FFI.
// Takes a JSON request (query + documents), returns JSON ranked results.
func MLRerank(input string) ([]byte, error) {
	if len(input) == 0 {
		return nil, ffiError("ml_rerank", -1)
	}
	inputPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(input)))
	out := make([]byte, mlOutBufSize)
	outPtr := (*C.uchar)(unsafe.Pointer(&out[0]))

	rc := C.deneb_ml_rerank(inputPtr, C.ulong(len(input)), outPtr, C.ulong(len(out)))
	if rc < 0 {
		return nil, ffiError("ml_rerank", int(rc))
	}
	return out[:rc], nil
}
