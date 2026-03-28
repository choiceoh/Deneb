//! Protocol frame and parameter validation FFI exports.

#![allow(unsafe_code)]

use super::helpers::ffi_read_str;
use super::*;
use crate::protocol;

/// C FFI: Validate a gateway frame (JSON bytes).
/// Returns 0 on success, negative error code on failure.
///
/// # Safety
/// `json_ptr` must point to a valid UTF-8 byte buffer of length `json_len`.
#[no_mangle]
pub unsafe extern "C" fn deneb_validate_frame(json_ptr: *const u8, json_len: usize) -> i32 {
    let slice_result = ffi_read_str(json_ptr, json_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let json_str = match slice_result {
            Ok(s) => s,
            Err(e) => return e,
        };
        match protocol::validate_frame(json_str) {
            Ok(_) => 0,
            Err(_) => FFI_ERR_VALIDATION,
        }
    })
}

/// C FFI: Validate an error code string.
/// Returns 0 if valid, 1 if unknown, negative on error.
///
/// # Safety
/// `code_ptr` must point to valid UTF-8 of `code_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_validate_error_code(code_ptr: *const u8, code_len: usize) -> i32 {
    let slice_result = ffi_read_str(code_ptr, code_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let code_str = match slice_result {
            Ok(s) => s,
            Err(e) => return e,
        };
        if protocol::error_codes::is_valid_error_code(code_str) {
            0
        } else {
            1
        }
    })
}

/// C FFI: Validate RPC parameters for a given method name.
/// Returns 0 if valid, positive N = bytes written to `errors_out` (JSON error array),
/// negative on error (-1 = null ptr, -2 = invalid UTF-8, -3 = unknown method,
/// -4 = input too large, -5 = invalid JSON).
///
/// # Safety
/// All pointers must be valid for their respective lengths.
/// `errors_out` must be writable for `errors_out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_validate_params(
    method_ptr: *const u8,
    method_len: usize,
    json_ptr: *const u8,
    json_len: usize,
    errors_out: *mut u8,
    errors_out_len: usize,
) -> i32 {
    if method_ptr.is_null() || json_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    // Cap both method name and JSON payload lengths.
    if json_len > FFI_MAX_INPUT_LEN || method_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    // SAFETY: method_ptr and json_ptr are null-checked above, both lengths
    // bounded by FFI_MAX_INPUT_LEN. The Go caller guarantees valid buffers.
    let method_slice = std::slice::from_raw_parts(method_ptr, method_len);
    let json_slice = std::slice::from_raw_parts(json_ptr, json_len);

    ffi_catch(FFI_ERR_PANIC, move || {
        let method_str = match std::str::from_utf8(method_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let json_str = match std::str::from_utf8(json_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };

        match protocol::validation::validate_params(method_str, json_str) {
            Ok(result) => {
                if result.valid {
                    0
                } else {
                    // Write errors as JSON to output buffer.
                    // Returns the TOTAL required bytes (snprintf convention) so
                    // callers can detect truncation: if retval > errors_out_len,
                    // the output was truncated.
                    let json_bytes = serde_json::to_vec(&result.errors).unwrap_or_default();
                    let total_len = json_bytes.len();
                    if !errors_out.is_null() && !json_bytes.is_empty() {
                        let write_len = total_len.min(errors_out_len);
                        // SAFETY: errors_out is null-checked in this branch, errors_out_len
                        // is the caller-provided buffer capacity.
                        let out = std::slice::from_raw_parts_mut(errors_out, errors_out_len);
                        out[..write_len].copy_from_slice(&json_bytes[..write_len]);
                    }
                    // Always return total required bytes so caller can detect
                    // truncation (total > buffer size) or no-buffer case.
                    total_len.max(1) as i32
                }
            }
            Err(protocol::validation::ValidateParamsError::UnknownMethod(_)) => FFI_ERR_VALIDATION,
            Err(protocol::validation::ValidateParamsError::InvalidJson(_)) => FFI_ERR_JSON,
        }
    })
}

