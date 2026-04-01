//! C FFI: Compaction evaluation and sweep state machine.

use crate::ffi_utils::*;

/// C FFI: Evaluate whether compaction is needed.
/// Writes JSON `CompactionDecision` into `out_ptr`.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `config_ptr` must be valid UTF-8 JSON of `config_len` bytes.
/// `out_ptr` must be writable for `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_compaction_evaluate(
    config_ptr: *const u8,
    config_len: usize,
    stored_tokens: u64,
    live_tokens: u64,
    token_budget: u64,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if config_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_POINTER;
    }
    if config_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    // SAFETY: config_ptr and out_ptr are null-checked above, config_len bounded
    // by FFI_MAX_INPUT_LEN. The Go caller guarantees both buffers are valid.
    let config_slice = std::slice::from_raw_parts(config_ptr, config_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_RUST_PANIC, move || {
        let config_str = match std::str::from_utf8(config_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let config: crate::compaction::CompactionConfig = match serde_json::from_str(config_str) {
            Ok(c) => c,
            Err(_) => return FFI_ERR_JSON_ERROR,
        };
        let decision =
            crate::compaction::evaluate(&config, stored_tokens, live_tokens, token_budget);
        ffi_write_json(out_slice, &decision)
    })
}

/// C FFI: Create a new compaction sweep engine.
/// Returns a positive handle on success, negative on error.
///
/// # Safety
/// `config_ptr` must be valid UTF-8 JSON of `config_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_compaction_sweep_new(
    config_ptr: *const u8,
    config_len: usize,
    conversation_id: u64,
    token_budget: u64,
    force: i32,
    hard_trigger: i32,
    now_ms: i64,
) -> i64 {
    if config_ptr.is_null() {
        return FFI_ERR_NULL_POINTER as i64;
    }
    if config_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE as i64;
    }
    // SAFETY: config_ptr is null-checked above, config_len bounded by FFI_MAX_INPUT_LEN.
    let config_slice = std::slice::from_raw_parts(config_ptr, config_len);
    ffi_catch(FFI_ERR_RUST_PANIC, move || {
        let config_str = match std::str::from_utf8(config_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        // Validate u64→u32 narrowing to prevent silent truncation.
        if conversation_id > u32::MAX as u64 || token_budget > u32::MAX as u64 {
            return FFI_ERR_INPUT_TOO_LARGE;
        }
        let handle = crate::compaction::handle::compaction_sweep_new(
            config_str.to_string(),
            conversation_id as u32,
            token_budget as u32,
            force != 0,
            hard_trigger != 0,
            now_ms as f64,
        );
        // Validate handle fits in i32 to prevent truncation in i32→i64 cast chain.
        if handle > i32::MAX as u32 {
            return FFI_ERR_OVERFLOW;
        }
        handle as i32
    }) as i64
}

/// C FFI: Start a sweep engine. Writes first `SweepCommand` JSON to `out_ptr`.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `out_ptr` must be writable for `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_compaction_sweep_start(
    handle: u32,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if out_ptr.is_null() {
        return FFI_ERR_NULL_POINTER;
    }
    // SAFETY: out_ptr is null-checked above. The Go caller guarantees the buffer is valid.
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_RUST_PANIC, move || {
        let json = crate::compaction::handle::compaction_sweep_start(handle);
        ffi_write_bytes(out_slice, json.as_bytes())
    })
}

/// C FFI: Step a sweep engine with a response. Writes next `SweepCommand` JSON.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `resp_ptr` must be valid UTF-8 JSON. `out_ptr` must be writable.
#[no_mangle]
pub unsafe extern "C" fn deneb_compaction_sweep_step(
    handle: u32,
    resp_ptr: *const u8,
    resp_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if resp_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_POINTER;
    }
    if resp_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    // SAFETY: resp_ptr and out_ptr are null-checked above, resp_len bounded
    // by FFI_MAX_INPUT_LEN. The Go caller guarantees both buffers are valid.
    let resp_slice = std::slice::from_raw_parts(resp_ptr, resp_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_RUST_PANIC, move || {
        let resp_str = match std::str::from_utf8(resp_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let json = crate::compaction::handle::compaction_sweep_step(handle, resp_str.to_string());
        ffi_write_bytes(out_slice, json.as_bytes())
    })
}

/// C FFI: Drop a sweep engine, freeing its resources.
#[no_mangle]
pub extern "C" fn deneb_compaction_sweep_drop(handle: u32) {
    crate::compaction::handle::compaction_sweep_drop(handle);
}
