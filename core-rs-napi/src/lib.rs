//! napi-rs bridge for deneb-core.
//!
//! Exposes core-rs Rust functions to Node.js via N-API,
//! enabling direct calls from TypeScript without C FFI overhead.

use napi::bindgen_prelude::*;
use napi_derive::napi;

/// Validate a gateway protocol frame (JSON string).
/// Throws a JS error if the frame is invalid.
#[napi]
pub fn validate_frame(json: String) -> Result<()> {
    deneb_core::protocol::validate_frame(&json)
        .map(|_| ())
        .map_err(|e| Error::from_reason(e.to_string()))
}

/// Constant-time byte comparison to prevent timing attacks.
/// Both buffers must be the same length for equality.
#[napi]
pub fn constant_time_eq(a: Buffer, b: Buffer) -> bool {
    deneb_core::security::constant_time_eq(a.as_ref(), b.as_ref())
}

/// Detect MIME type from file magic bytes.
/// Returns the MIME string (e.g. "image/png") or "application/octet-stream" for unknown.
#[napi]
pub fn detect_mime(data: Buffer) -> String {
    deneb_core::media::detect_mime(data.as_ref()).to_string()
}

/// Check if a string contains potential injection patterns.
/// Returns true if the input appears safe.
#[napi]
pub fn is_safe_input(input: String) -> bool {
    deneb_core::security::is_safe_input(&input)
}

/// Sanitize a string by removing control characters (except newline/tab/CR).
#[napi]
pub fn sanitize_control_chars(input: String) -> String {
    deneb_core::security::sanitize_control_chars(&input)
}
