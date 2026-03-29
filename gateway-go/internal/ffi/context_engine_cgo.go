//go:build !no_ffi && cgo

package ffi

/*
// Context engine FFI functions (from core-rs/core/src/lib.rs).
extern unsigned int deneb_context_assembly_new(
	unsigned long long conversation_id, unsigned long long token_budget,
	unsigned int fresh_tail_count);
extern int deneb_context_assembly_start(
	unsigned int handle,
	unsigned char *out_ptr, unsigned long out_len);
extern int deneb_context_assembly_step(
	unsigned int handle,
	const unsigned char *resp_ptr, unsigned long resp_len,
	unsigned char *out_ptr, unsigned long out_len);
extern unsigned int deneb_context_expand_new(
	const unsigned char *summary_id_ptr, unsigned long summary_id_len,
	unsigned int max_depth, int include_messages, unsigned long long token_cap);
extern int deneb_context_expand_start(
	unsigned int handle,
	unsigned char *out_ptr, unsigned long out_len);
extern int deneb_context_expand_step(
	unsigned int handle,
	const unsigned char *resp_ptr, unsigned long resp_len,
	unsigned char *out_ptr, unsigned long out_len);
extern void deneb_context_engine_drop(unsigned int handle);
*/
import "C"
import (
	"encoding/json"
	"errors"
	"unsafe"
)

// ContextAssemblyNew creates a new context assembly engine.
// Returns a handle for subsequent start/step calls.
func ContextAssemblyNew(conversationID, tokenBudget uint64, freshTailCount uint32) (uint32, error) {
	handle := uint32(C.deneb_context_assembly_new(
		C.ulonglong(conversationID),
		C.ulonglong(tokenBudget),
		C.uint(freshTailCount),
	))
	if handle == 0 {
		return 0, errors.New("ffi: context_assembly_new: failed to create engine")
	}
	return handle, nil
}

// ContextAssemblyStart starts an assembly engine, returning the first command JSON.
// The output buffer grows automatically if the Rust side signals it is too small.
func ContextAssemblyStart(handle uint32) (json.RawMessage, error) {
	data, err := ffiCallWithPool("context_assembly_start", &ffiOutPool1MB,
		func(outPtr unsafe.Pointer, outLen int) int {
			return int(C.deneb_context_assembly_start(
				C.uint(handle),
				(*C.uchar)(outPtr), C.ulong(outLen),
			))
		})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// ContextAssemblyStep advances the assembly engine with a host response.
// The output buffer grows automatically if the Rust side signals it is too small.
func ContextAssemblyStep(handle uint32, responseJSON []byte) (json.RawMessage, error) {
	if len(responseJSON) == 0 {
		return nil, errors.New("ffi: empty response JSON")
	}
	respPtr := (*C.uchar)(unsafe.Pointer(&responseJSON[0]))
	data, err := ffiCallWithPool("context_assembly_step", &ffiOutPool1MB,
		func(outPtr unsafe.Pointer, outLen int) int {
			return int(C.deneb_context_assembly_step(
				C.uint(handle),
				respPtr, C.ulong(len(responseJSON)),
				(*C.uchar)(outPtr), C.ulong(outLen),
			))
		})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// ContextExpandNew creates a new context expand engine for memory retrieval.
func ContextExpandNew(summaryID string, maxDepth uint32, includeMessages bool, tokenCap uint64) (uint32, error) {
	if len(summaryID) == 0 {
		return 0, errors.New("ffi: context_expand_new: empty summary_id")
	}
	summaryPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(summaryID)))
	includeInt := C.int(0)
	if includeMessages {
		includeInt = 1
	}

	handle := uint32(C.deneb_context_expand_new(
		summaryPtr, C.ulong(len(summaryID)),
		C.uint(maxDepth), includeInt, C.ulonglong(tokenCap),
	))
	if handle == 0 {
		return 0, errors.New("ffi: context_expand_new: failed to create engine")
	}
	return handle, nil
}

// ContextExpandStart starts an expand engine, returning the first command JSON.
// The output buffer grows automatically if the Rust side signals it is too small.
func ContextExpandStart(handle uint32) (json.RawMessage, error) {
	data, err := ffiCallWithPool("context_expand_start", &ffiOutPool1MB,
		func(outPtr unsafe.Pointer, outLen int) int {
			return int(C.deneb_context_expand_start(
				C.uint(handle),
				(*C.uchar)(outPtr), C.ulong(outLen),
			))
		})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// ContextExpandStep advances the expand engine with a host response.
// The output buffer grows automatically if the Rust side signals it is too small.
func ContextExpandStep(handle uint32, responseJSON []byte) (json.RawMessage, error) {
	if len(responseJSON) == 0 {
		return nil, errors.New("ffi: empty response JSON")
	}
	respPtr := (*C.uchar)(unsafe.Pointer(&responseJSON[0]))
	data, err := ffiCallWithPool("context_expand_step", &ffiOutPool1MB,
		func(outPtr unsafe.Pointer, outLen int) int {
			return int(C.deneb_context_expand_step(
				C.uint(handle),
				respPtr, C.ulong(len(responseJSON)),
				(*C.uchar)(outPtr), C.ulong(outLen),
			))
		})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// ContextEngineDrop releases a context engine handle's resources.
func ContextEngineDrop(handle uint32) {
	C.deneb_context_engine_drop(C.uint(handle))
}
