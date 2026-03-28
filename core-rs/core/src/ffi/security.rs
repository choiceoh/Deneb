//! Security-related FFI exports (constant-time eq, session key, HTML sanitize, SSRF).

#![allow(unsafe_code)]

use super::helpers::ffi_read_str;
use super::*;
use crate::security;

/// C FFI: Constant-time secret comparison.
/// Returns 0 if equal, non-zero otherwise.
///
/// # Safety
/// Both pointers must be valid byte buffers of their respective lengths.
#[no_mangle]
pub unsafe extern "C" fn deneb_constant_time_eq(
    a_ptr: *const u8,
    a_len: usize,
    b_ptr: *const u8,
    b_len: usize,
) -> i32 {
    if a_ptr.is_null() || b_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    // SAFETY: both pointers are null-checked above. The Go caller guarantees
    // each buffer is valid for its respective length.
    let a = std::slice::from_raw_parts(a_ptr, a_len);
    let b = std::slice::from_raw_parts(b_ptr, b_len);
    if security::constant_time_eq(a, b) {
        0
    } else {
        1
    }
}

/// C FFI: Validate a session key string.
/// Returns 0 if valid, -1 if null pointer, -2 if invalid UTF-8, -7 if invalid key.
///
/// # Safety
/// `key_ptr` must point to a valid UTF-8 byte buffer of length `key_len`.
#[no_mangle]
pub unsafe extern "C" fn deneb_validate_session_key(key_ptr: *const u8, key_len: usize) -> i32 {
    let slice_result = ffi_read_str(key_ptr, key_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let key_str = match slice_result {
            Ok(s) => s,
            Err(e) => return e,
        };
        if security::is_valid_session_key(key_str) {
            0
        } else {
            FFI_ERR_VALIDATION
        }
    })
}

/// C FFI: Sanitize HTML in a string.
/// Writes the sanitized output into `out_ptr` (max `out_len` bytes).
/// Returns the number of bytes written, or negative on error.
///
/// # Safety
/// `input_ptr` must be valid UTF-8 of `input_len` bytes.
/// `out_ptr` must be writable for `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_sanitize_html(
    input_ptr: *const u8,
    input_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if input_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if input_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    // SAFETY: input_ptr and out_ptr are null-checked above, input_len bounded
    // by FFI_MAX_INPUT_LEN. The Go caller guarantees both buffers are valid.
    let slice = std::slice::from_raw_parts(input_ptr, input_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let input_str = match std::str::from_utf8(slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let sanitized = security::sanitize_html(input_str);
        let bytes = sanitized.as_bytes();
        if bytes.len() > out_slice.len() {
            return FFI_ERR_OUTPUT_TOO_SMALL;
        }
        out_slice[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
    })
}

/// C FFI: Check if a URL is safe (not targeting internal networks).
/// Returns 0 if safe, 1 if unsafe, negative on error.
///
/// # Safety
/// `url_ptr` must point to valid UTF-8 of `url_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_is_safe_url(url_ptr: *const u8, url_len: usize) -> i32 {
    if url_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    // URLs should not be extremely long; cap at 8 KB.
    // Returns 1 (unsafe) rather than FFI_ERR_INPUT_TOO_LARGE because an
    // oversized URL is a policy rejection (SSRF risk), not a parameter error.
    if url_len > 8192 {
        return 1;
    }
    // SAFETY: url_ptr is null-checked above, url_len capped at 8 KB.
    let slice = std::slice::from_raw_parts(url_ptr, url_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let url_str = match std::str::from_utf8(slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        if security::is_safe_url(url_str) {
            0
        } else {
            1
        }
    })
}
