//! Reusable FFI helpers to reduce boilerplate in domain modules.

#![allow(unsafe_code)]

use super::*;

/// Read a UTF-8 string from an FFI input buffer with null check and length validation.
/// Returns the string slice or an FFI error code.
pub(crate) unsafe fn ffi_read_str<'a>(ptr: *const u8, len: usize) -> Result<&'a str, i32> {
    if ptr.is_null() {
        return Err(FFI_ERR_NULL_PTR);
    }
    if len > FFI_MAX_INPUT_LEN {
        return Err(FFI_ERR_INPUT_TOO_LARGE);
    }
    let slice = std::slice::from_raw_parts(ptr, len);
    std::str::from_utf8(slice).map_err(|_| FFI_ERR_INVALID_UTF8)
}

/// Write bytes to an FFI output buffer. Returns bytes written or FFI_ERR_OUTPUT_TOO_SMALL.
pub(crate) fn ffi_write_bytes(bytes: &[u8], out: &mut [u8]) -> i32 {
    if bytes.len() > out.len() {
        return FFI_ERR_OUTPUT_TOO_SMALL;
    }
    out[..bytes.len()].copy_from_slice(bytes);
    bytes.len() as i32
}

/// Serialize value to JSON and write to output buffer. Returns bytes written or error code.
pub(crate) fn ffi_write_json(value: &impl serde::Serialize, out: &mut [u8]) -> i32 {
    match serde_json::to_string(value) {
        Ok(json) => ffi_write_bytes(json.as_bytes(), out),
        Err(_) => FFI_ERR_JSON,
    }
}
