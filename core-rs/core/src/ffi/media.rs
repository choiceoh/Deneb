//! Media MIME detection FFI export.

#![allow(unsafe_code)]

use super::*;
use crate::media;

/// C FFI: Detect MIME type from a byte buffer.
/// Writes the MIME string into `out_ptr` (max `out_len` bytes).
/// Returns the number of bytes written, or negative on error.
///
/// # Safety
/// `data_ptr` must point to a valid buffer of `data_len` bytes.
/// `out_ptr` must point to a writable buffer of `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_detect_mime(
    data_ptr: *const u8,
    data_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if data_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    // SAFETY: data_ptr and out_ptr are null-checked above. The Go caller guarantees
    // both buffers are valid for their respective lengths.
    let data = std::slice::from_raw_parts(data_ptr, data_len);
    let mime = media::detect_mime(data);
    let mime_bytes = mime.as_bytes();
    if mime_bytes.len() > out_len {
        return FFI_ERR_OUTPUT_TOO_SMALL;
    }
    // SAFETY: mime_bytes.len() <= out_len checked above; source (stack) and
    // destination (caller-owned) never overlap.
    std::ptr::copy_nonoverlapping(mime_bytes.as_ptr(), out_ptr, mime_bytes.len());
    mime_bytes.len() as i32
}
