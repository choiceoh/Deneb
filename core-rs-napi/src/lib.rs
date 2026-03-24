//! napi-rs bridge for deneb-core.
//!
//! Exposes core-rs Rust functions to Node.js via N-API,
//! enabling direct calls from TypeScript without C FFI overhead.

use napi::bindgen_prelude::*;
use napi_derive::napi;

/// Hard cap on JSON input to prevent OOM during serde parsing (16 MiB).
const MAX_JSON_BYTES: usize = 16 * 1024 * 1024;

/// Hard cap on string input for sanitization (64 MiB).
const MAX_STRING_BYTES: usize = 64 * 1024 * 1024;

/// Validate a gateway protocol frame (JSON string).
/// Returns the frame type ("req", "res", or "event") on success.
/// Throws a JS error if the frame is invalid or exceeds the size limit.
#[napi]
pub fn validate_frame(json: String) -> Result<String> {
    if json.len() > MAX_JSON_BYTES {
        return Err(Error::from_reason(format!(
            "frame JSON exceeds size limit ({} > {} bytes)",
            json.len(),
            MAX_JSON_BYTES
        )));
    }
    deneb_core::protocol::validate_frame(&json)
        .map(|frame| match frame {
            deneb_core::protocol::GatewayFrame::Request(_) => "req".to_string(),
            deneb_core::protocol::GatewayFrame::Response(_) => "res".to_string(),
            deneb_core::protocol::GatewayFrame::Event(_) => "event".to_string(),
        })
        .map_err(|e| Error::from_reason(e.to_string()))
}

/// Constant-time byte comparison to prevent timing attacks.
/// Both buffers must be the same length for equality.
#[napi]
pub fn constant_time_eq(a: Buffer, b: Buffer) -> bool {
    deneb_core::security::constant_time_eq(a.as_ref(), b.as_ref())
}

/// Detect MIME type from file magic bytes.
/// Only reads the first 4096 bytes for detection regardless of buffer size.
/// Returns the MIME string (e.g. "image/png") or "application/octet-stream" for unknown.
#[napi]
pub fn detect_mime(data: Buffer) -> String {
    let slice = data.as_ref();
    // Only the first bytes matter for magic-byte sniffing; cap to avoid unnecessary work.
    let head = if slice.len() > 4096 {
        &slice[..4096]
    } else {
        slice
    };
    deneb_core::media::detect_mime(head).to_string()
}

/// Check if a string contains potential injection patterns.
/// Returns true if the input appears safe.
#[napi]
pub fn is_safe_input(input: String) -> bool {
    deneb_core::security::is_safe_input(&input)
}

/// Sanitize a string by removing control characters (except newline/tab/CR).
/// Returns an error if the input exceeds the size limit.
#[napi]
pub fn sanitize_control_chars(input: String) -> Result<String> {
    if input.len() > MAX_STRING_BYTES {
        return Err(Error::from_reason(format!(
            "input exceeds size limit ({} > {} bytes)",
            input.len(),
            MAX_STRING_BYTES
        )));
    }
    Ok(deneb_core::security::sanitize_control_chars(&input))
}
