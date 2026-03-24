//go:build !no_ffi && cgo

package ffi

/*
// Compaction FFI functions (from core-rs/src/lib.rs).
extern int deneb_compaction_evaluate(
	const unsigned char *config_ptr, unsigned long config_len,
	unsigned long long stored_tokens, unsigned long long live_tokens,
	unsigned long long token_budget,
	unsigned char *out_ptr, unsigned long out_len);
extern long long deneb_compaction_sweep_new(
	const unsigned char *config_ptr, unsigned long config_len,
	unsigned long long conversation_id, unsigned long long token_budget,
	int force, int hard_trigger, long long now_ms);
extern int deneb_compaction_sweep_start(
	unsigned int handle,
	unsigned char *out_ptr, unsigned long out_len);
extern int deneb_compaction_sweep_step(
	unsigned int handle,
	const unsigned char *resp_ptr, unsigned long resp_len,
	unsigned char *out_ptr, unsigned long out_len);
extern void deneb_compaction_sweep_drop(unsigned int handle);
*/
import "C"
import (
	"encoding/json"
	"errors"
	"unsafe"
)

const compactionOutBufSize = 1024 * 1024 // 1 MB default output buffer

// CompactionEvaluate evaluates whether compaction is needed.
// Returns the JSON-encoded CompactionDecision.
func CompactionEvaluate(configJSON string, storedTokens, liveTokens, tokenBudget uint64) ([]byte, error) {
	if len(configJSON) == 0 {
		return nil, errors.New("ffi: empty config JSON")
	}
	configPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(configJSON)))
	out := make([]byte, 512)
	outPtr := (*C.uchar)(unsafe.Pointer(&out[0]))

	rc := C.deneb_compaction_evaluate(
		configPtr, C.ulong(len(configJSON)),
		C.ulonglong(storedTokens), C.ulonglong(liveTokens), C.ulonglong(tokenBudget),
		outPtr, C.ulong(len(out)),
	)
	if rc < 0 {
		return nil, ffiError("compaction_evaluate", int(rc))
	}
	return out[:rc], nil
}

// CompactionSweepNew creates a new compaction sweep engine.
// Returns a handle for subsequent start/step/drop calls.
func CompactionSweepNew(configJSON string, conversationID, tokenBudget uint64, force, hardTrigger bool, nowMs int64) (uint32, error) {
	if len(configJSON) == 0 {
		return 0, errors.New("ffi: empty config JSON")
	}
	configPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(configJSON)))

	forceInt := C.int(0)
	if force {
		forceInt = 1
	}
	hardInt := C.int(0)
	if hardTrigger {
		hardInt = 1
	}

	rc := C.deneb_compaction_sweep_new(
		configPtr, C.ulong(len(configJSON)),
		C.ulonglong(conversationID), C.ulonglong(tokenBudget),
		forceInt, hardInt, C.longlong(nowMs),
	)
	if rc < 0 {
		return 0, ffiError("compaction_sweep_new", int(rc))
	}
	return uint32(rc), nil
}

// CompactionSweepStart starts a sweep and returns the first command JSON.
func CompactionSweepStart(handle uint32) (json.RawMessage, error) {
	out := make([]byte, compactionOutBufSize)
	outPtr := (*C.uchar)(unsafe.Pointer(&out[0]))

	rc := C.deneb_compaction_sweep_start(C.uint(handle), outPtr, C.ulong(len(out)))
	if rc < 0 {
		return nil, ffiError("compaction_sweep_start", int(rc))
	}
	return json.RawMessage(out[:rc]), nil
}

// CompactionSweepStep advances the sweep with a host response and returns the next command.
func CompactionSweepStep(handle uint32, responseJSON []byte) (json.RawMessage, error) {
	if len(responseJSON) == 0 {
		return nil, errors.New("ffi: empty response JSON")
	}
	respPtr := (*C.uchar)(unsafe.Pointer(&responseJSON[0]))
	out := make([]byte, compactionOutBufSize)
	outPtr := (*C.uchar)(unsafe.Pointer(&out[0]))

	rc := C.deneb_compaction_sweep_step(
		C.uint(handle),
		respPtr, C.ulong(len(responseJSON)),
		outPtr, C.ulong(len(out)),
	)
	if rc < 0 {
		return nil, ffiError("compaction_sweep_step", int(rc))
	}
	return json.RawMessage(out[:rc]), nil
}

// CompactionSweepDrop releases a sweep engine's resources.
func CompactionSweepDrop(handle uint32) {
	C.deneb_compaction_sweep_drop(C.uint(handle))
}

func ffiError(fn string, rc int) error {
	switch rc {
	case -1:
		return errors.New("ffi: " + fn + ": null pointer")
	case -2:
		return errors.New("ffi: " + fn + ": invalid UTF-8")
	case -3:
		return errors.New("ffi: " + fn + ": parse error")
	case -4:
		return errors.New("ffi: " + fn + ": input too large")
	case -6:
		return errors.New("ffi: " + fn + ": output buffer too small")
	case -99:
		return errors.New("ffi: " + fn + ": panic")
	default:
		return errors.New("ffi: " + fn + ": unknown error")
	}
}
