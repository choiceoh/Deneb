//! Deneb Core — Rust implementation of performance-critical modules.
//!
//! This crate provides:
//! - Protocol frame validation (replacing AJV)
//! - Security verification primitives + `ReDoS` detection
//! - Media MIME detection, EXIF parsing, PNG encoding
//!
//! It exposes both a Rust API and a C FFI surface for integration
//! with Go (via `CGo`) and Node.js (via napi-rs).

// This crate uses unsafe for C FFI exports (#[no_mangle] extern "C" functions)
// required by the Go gateway CGo integration.
#![allow(unsafe_code)]

#[cfg(feature = "napi_binding")]
#[macro_use]
extern crate napi_derive;

// FFI utilities: error codes, FFI_MAX_INPUT_LEN, ffi_catch
mod ffi_utils;
use ffi_utils::*;

// Core modules (C FFI + Rust API)
pub mod auth;
pub mod compaction;
pub mod context_engine;
pub mod markdown;
pub mod media;
pub mod memory_search;
pub mod parsing;
pub mod protocol;
pub mod security;

// napi-rs modules (Node.js native addon)
pub mod exif;
pub mod external_content;
pub mod mime_utils;
pub mod png;
pub mod safe_regex;

// C FFI exports organised by domain (used by Go via CGo).
// Each submodule in ffi/ owns the `deneb_*` functions for its domain.
mod ffi;

// Re-export all FFI symbols into the crate root so that existing callers
// and tests that do `use super::*` continue to resolve them without changes.
pub use ffi::compaction::*;
pub use ffi::context_engine::*;
pub use ffi::markdown::*;
pub use ffi::media::*;
pub use ffi::memory_search::*;
pub use ffi::parsing::*;
pub use ffi::protocol::*;
pub use ffi::security::*;
pub use ffi::vega::*;

#[cfg(test)]
mod tests {
    use super::*;

    fn ffi_validate_params(method: &str, params_json: &str, out: &mut [u8]) -> (i32, String) {
        let code = unsafe {
            deneb_validate_params(
                method.as_ptr(),
                method.len(),
                params_json.as_ptr(),
                params_json.len(),
                out.as_mut_ptr(),
                out.len(),
            )
        };
        let written = if code > 0 {
            (code as usize).min(out.len())
        } else {
            0
        };
        let payload = String::from_utf8_lossy(&out[..written]).to_string();
        (code, payload)
    }

    #[test]
    fn test_validate_frame_valid_request() {
        let json = r#"{"type":"req","id":"1","method":"chat.send"}"#;
        let result = unsafe { deneb_validate_frame(json.as_ptr(), json.len()) };
        assert_eq!(result, 0);
    }

    #[test]
    fn test_validate_frame_invalid() {
        let json = r#"{"type":"unknown"}"#;
        let result = unsafe { deneb_validate_frame(json.as_ptr(), json.len()) };
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
    fn test_validate_session_key_valid() {
        let key = "my-session-123";
        let result = unsafe { deneb_validate_session_key(key.as_ptr(), key.len()) };
        assert_eq!(result, 0);
    }

    #[test]
    fn test_validate_session_key_empty() {
        let key = "";
        let result = unsafe { deneb_validate_session_key(key.as_ptr(), key.len()) };
        assert_eq!(result, FFI_ERR_VALIDATION);
    }

    #[test]
    fn test_sanitize_html_ffi() -> Result<(), Box<dyn std::error::Error>> {
        let input = "<b>hi</b>";
        let mut out = [0u8; 256];
        let len = unsafe {
            deneb_sanitize_html(input.as_ptr(), input.len(), out.as_mut_ptr(), out.len())
        };
        assert!(len > 0);
        let result = std::str::from_utf8(&out[..len as usize])?;
        assert_eq!(result, "&lt;b&gt;hi&lt;/b&gt;");
        Ok(())
    }

    #[test]
    fn test_is_safe_url_ffi() {
        let safe = "https://example.com";
        assert_eq!(unsafe { deneb_is_safe_url(safe.as_ptr(), safe.len()) }, 0);

        let unsafe_url = "http://localhost/admin";
        assert_eq!(
            unsafe { deneb_is_safe_url(unsafe_url.as_ptr(), unsafe_url.len()) },
            1
        );
    }

    #[test]
    fn test_validate_error_code_ffi() {
        let valid = "NOT_FOUND";
        assert_eq!(
            unsafe { deneb_validate_error_code(valid.as_ptr(), valid.len()) },
            0
        );

        let invalid = "BOGUS";
        assert_eq!(
            unsafe { deneb_validate_error_code(invalid.as_ptr(), invalid.len()) },
            1
        );
    }

    #[test]
    fn test_bridge_native_equivalence_validate_params_valid(
    ) -> Result<(), Box<dyn std::error::Error>> {
        let method = "chat.send";
        let params = r#"{"sessionKey":"sess-1","message":"hello","idempotencyKey":"idk-1"}"#;

        let native = protocol::validation::validate_params(method, params)?;
        assert!(native.valid);

        let mut out = [0u8; 256];
        let (ffi_code, ffi_payload) = ffi_validate_params(method, params, &mut out);
        assert_eq!(ffi_code, 0, "ffi payload={ffi_payload}");
        Ok(())
    }

    #[test]
    fn test_bridge_native_equivalence_validate_params_invalid(
    ) -> Result<(), Box<dyn std::error::Error>> {
        let method = "chat.send";
        let params = r#"{"text":"missing required key"}"#;

        let native = protocol::validation::validate_params(method, params)?;
        assert!(!native.valid);
        let expected = serde_json::to_string(&native.errors)?;

        let mut out = [0u8; 1024];
        let (ffi_code, ffi_payload) = ffi_validate_params(method, params, &mut out);
        assert!(
            ffi_code > 0,
            "expected serialized errors size, got {ffi_code}"
        );
        assert_eq!(ffi_payload, expected);
        Ok(())
    }

    #[test]
    fn test_detect_mime() -> Result<(), Box<dyn std::error::Error>> {
        let png = [0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A];
        let mut out = [0u8; 64];
        let len =
            unsafe { deneb_detect_mime(png.as_ptr(), png.len(), out.as_mut_ptr(), out.len()) };
        assert!(len > 0);
        let mime = std::str::from_utf8(&out[..len as usize])?;
        assert_eq!(mime, "image/png");
        Ok(())
    }

    #[test]
    fn test_vega_execute_stub() -> Result<(), Box<dyn std::error::Error>> {
        let cmd = r#"{"command":"search","query":"test"}"#;
        let mut out = [0u8; 256];
        let len =
            unsafe { deneb_vega_execute(cmd.as_ptr(), cmd.len(), out.as_mut_ptr(), out.len()) };
        assert!(len > 0);
        let result = std::str::from_utf8(&out[..len as usize])?;
        assert!(result.contains("vega_not_implemented"));
        Ok(())
    }

    #[test]
    fn test_vega_search_stub() -> Result<(), Box<dyn std::error::Error>> {
        let query = r#"{"query":"test"}"#;
        let mut out = [0u8; 256];
        let len =
            unsafe { deneb_vega_search(query.as_ptr(), query.len(), out.as_mut_ptr(), out.len()) };
        assert!(len > 0);
        let result = std::str::from_utf8(&out[..len as usize])?;
        assert!(result.contains("results"));
        Ok(())
    }

    #[test]
    fn test_extract_links_ffi() -> Result<(), Box<dyn std::error::Error>> {
        let text = "Check https://example.com and https://rust-lang.org please";
        let config = r#"{"max_links":5}"#;
        let mut out = [0u8; 1024];
        let len = unsafe {
            deneb_extract_links(
                text.as_ptr(),
                text.len(),
                config.as_ptr(),
                config.len(),
                out.as_mut_ptr(),
                out.len(),
            )
        };
        assert!(len > 0);
        let result = std::str::from_utf8(&out[..len as usize])?;
        assert!(result.contains("https://example.com"));
        assert!(result.contains("https://rust-lang.org"));
        Ok(())
    }
}
