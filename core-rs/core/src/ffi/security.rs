//! C FFI: Security primitives (constant-time comparison, session key validation,
//! HTML sanitization, URL safety checks).

use crate::ffi_utils::*;

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
        return FFI_ERR_NULL_POINTER;
    }
    // SAFETY: both pointers are null-checked above. The Go caller guarantees
    // each buffer is valid for its respective length.
    let a = std::slice::from_raw_parts(a_ptr, a_len);
    let b = std::slice::from_raw_parts(b_ptr, b_len);
    if crate::security::constant_time_eq(a, b) {
        0
    } else {
        1
    }
}

ffi_string_to_int!(
    /// C FFI: Validate a session key string.
    /// Returns 0 if valid, -1 if null pointer, -2 if invalid UTF-8, -7 if invalid key.
    ///
    /// # Safety
    /// `key_ptr` must point to a valid UTF-8 byte buffer of length `key_len`.
    fn deneb_validate_session_key(key_ptr, key_len, max_len = FFI_MAX_INPUT_LEN, key_str) {
        if crate::security::is_valid_session_key(key_str) {
            0
        } else {
            FFI_ERR_VALIDATION
        }
    }
);

ffi_string_to_buffer!(
    /// C FFI: Sanitize HTML in a string.
    /// Writes the sanitized output into `out_ptr` (max `out_len` bytes).
    /// Returns the number of bytes written, or negative on error.
    ///
    /// # Safety
    /// `input_ptr` must be valid UTF-8 of `input_len` bytes.
    /// `out_ptr` must be writable for `out_len` bytes.
    fn deneb_sanitize_html(
        input_ptr,
        input_len,
        out = (out_ptr, out_len),
        max_len = FFI_MAX_INPUT_LEN,
        input_str,
        out_slice
    ) {
        let sanitized = crate::security::sanitize_html(input_str);
        let bytes = sanitized.as_bytes();
        if bytes.len() > out_slice.len() {
            return FFI_ERR_OUTPUT_TOO_SMALL;
        }
        out_slice[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
    }
);

ffi_string_to_int!(
    /// C FFI: Check if a URL is safe (not targeting internal networks).
    /// Returns 0 if safe, 1 if unsafe, negative on error.
    ///
    /// # Safety
    /// `url_ptr` must point to valid UTF-8 of `url_len` bytes.
    fn deneb_is_safe_url(
        url_ptr,
        url_len,
        max_len = 8192,
        too_large = 1,
        url_str
    ) {
        if crate::security::is_safe_url(url_str) {
            0
        } else {
            1
        }
    }
);
