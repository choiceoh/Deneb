//! Deneb Core — Rust implementation of performance-critical modules.
//!
//! This crate provides:
//! - Protocol frame validation (replacing AJV)
//! - Security verification primitives
//! - Media MIME detection and processing helpers
//!
//! It exposes both a Rust API and a C FFI surface for integration
//! with Go (via CGo) and Node.js (via napi-rs).

pub mod media;
pub mod protocol;
pub mod security;

/// C FFI: Validate a gateway frame (JSON bytes).
/// Returns 0 on success, negative error code on failure.
///
/// # Safety
/// `json_ptr` must point to a valid UTF-8 byte buffer of length `json_len`.
#[no_mangle]
pub unsafe extern "C" fn deneb_validate_frame(json_ptr: *const u8, json_len: usize) -> i32 {
    if json_ptr.is_null() {
        return -1;
    }
    let slice = unsafe { std::slice::from_raw_parts(json_ptr, json_len) };
    let json_str = match std::str::from_utf8(slice) {
        Ok(s) => s,
        Err(_) => return -2,
    };
    match protocol::validate_frame(json_str) {
        Ok(_) => 0,
        Err(_) => -3,
    }
}

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
        return -1;
    }
    let a = unsafe { std::slice::from_raw_parts(a_ptr, a_len) };
    let b = unsafe { std::slice::from_raw_parts(b_ptr, b_len) };
    if security::constant_time_eq(a, b) {
        0
    } else {
        1
    }
}

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
        return -1;
    }
    let data = unsafe { std::slice::from_raw_parts(data_ptr, data_len) };
    let mime = media::detect_mime(data);
    let mime_bytes = mime.as_bytes();
    if mime_bytes.len() > out_len {
        return -2;
    }
    unsafe {
        std::ptr::copy_nonoverlapping(mime_bytes.as_ptr(), out_ptr, mime_bytes.len());
    }
    mime_bytes.len() as i32
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_validate_frame_valid_request() {
        let json = r#"{"type":"req","id":"1","method":"chat.send"}"#;
        let result =
            unsafe { deneb_validate_frame(json.as_ptr(), json.len()) };
        assert_eq!(result, 0);
    }

    #[test]
    fn test_validate_frame_invalid() {
        let json = r#"{"type":"unknown"}"#;
        let result =
            unsafe { deneb_validate_frame(json.as_ptr(), json.len()) };
        assert!(result < 0);
    }

    #[test]
    fn test_constant_time_eq() {
        let a = b"secret123";
        let b = b"secret123";
        let c = b"different";
        assert_eq!(
            unsafe { deneb_constant_time_eq(a.as_ptr(), a.len(), b.as_ptr(), b.len()) },
            0
        );
        assert_ne!(
            unsafe { deneb_constant_time_eq(a.as_ptr(), a.len(), c.as_ptr(), c.len()) },
            0
        );
    }

    #[test]
    fn test_detect_mime() {
        // PNG magic bytes
        let png = [0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A];
        let mut out = [0u8; 64];
        let len = unsafe {
            deneb_detect_mime(png.as_ptr(), png.len(), out.as_mut_ptr(), out.len())
        };
        assert!(len > 0);
        let mime = std::str::from_utf8(&out[..len as usize]).unwrap();
        assert_eq!(mime, "image/png");
    }
}
