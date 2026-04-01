//! C FFI: Context assembly and expansion state machines (handle-based).
//!
//! Stateful pattern: `*_new()` → handle → `*_start(handle)` → `*_step(handle, resp)` → drop.

use crate::ffi_utils::*;

/// C FFI: Create a new context assembly engine.
/// Returns a positive handle on success.
#[no_mangle]
pub extern "C" fn deneb_context_assembly_new(
    conversation_id: u64,
    token_budget: u64,
    fresh_tail_count: u32,
) -> u32 {
    crate::context_engine::handle::context_assembly_new(
        conversation_id as u32,
        token_budget as u32,
        fresh_tail_count,
    )
}

/// C FFI: Start an assembly engine. Writes first `AssemblyCommand` JSON to `out_ptr`.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `out_ptr` must be writable for `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_context_assembly_start(
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
        let json = crate::context_engine::handle::context_assembly_start(handle);
        ffi_write_bytes(out_slice, json.as_bytes())
    })
}

/// C FFI: Step an assembly engine with a host response.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `resp_ptr` must be valid UTF-8 JSON. `out_ptr` must be writable.
#[no_mangle]
pub unsafe extern "C" fn deneb_context_assembly_step(
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
        let json = crate::context_engine::handle::context_assembly_step(handle, resp_str.to_string());
        ffi_write_bytes(out_slice, json.as_bytes())
    })
}

/// C FFI: Create a new context expand engine.
/// Returns a positive handle on success.
///
/// # Safety
/// `summary_id_ptr` must be valid UTF-8.
#[no_mangle]
pub unsafe extern "C" fn deneb_context_expand_new(
    summary_id_ptr: *const u8,
    summary_id_len: usize,
    max_depth: u32,
    include_messages: i32,
    token_cap: u64,
) -> u32 {
    if summary_id_ptr.is_null() {
        return 0;
    }
    // SAFETY: summary_id_ptr is null-checked above.
    let slice = std::slice::from_raw_parts(summary_id_ptr, summary_id_len);
    let summary_id = match std::str::from_utf8(slice) {
        Ok(s) => s.to_string(),
        Err(_) => return 0,
    };
    crate::context_engine::handle::context_expand_new(
        summary_id,
        max_depth,
        include_messages != 0,
        token_cap as u32,
    )
}

/// C FFI: Start an expand engine. Writes first `RetrievalCommand` JSON to `out_ptr`.
///
/// # Safety
/// `out_ptr` must be writable for `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_context_expand_start(
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
        let json = crate::context_engine::handle::context_expand_start(handle);
        ffi_write_bytes(out_slice, json.as_bytes())
    })
}

/// C FFI: Step an expand engine with a host response.
///
/// # Safety
/// `resp_ptr` must be valid UTF-8 JSON. `out_ptr` must be writable.
#[no_mangle]
pub unsafe extern "C" fn deneb_context_expand_step(
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
        let json = crate::context_engine::handle::context_expand_step(handle, resp_str.to_string());
        ffi_write_bytes(out_slice, json.as_bytes())
    })
}

/// C FFI: Drop any context engine, freeing its resources.
#[no_mangle]
pub extern "C" fn deneb_context_engine_drop(handle: u32) {
    crate::context_engine::handle::context_engine_drop(handle);
}
