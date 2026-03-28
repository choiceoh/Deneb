//! FFI integration tests — call the #[no_mangle] extern "C" functions directly.

use super::compaction::*;
use super::context::*;
use super::markdown::*;
use super::media::*;
use super::memory::*;
use super::parsing::*;
use super::protocol::*;
use super::security::*;
use super::vega::*;
use super::*;

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
fn test_sanitize_html_ffi() {
    let input = "<b>hi</b>";
    let mut out = [0u8; 256];
    let len = unsafe {
        deneb_sanitize_html(input.as_ptr(), input.len(), out.as_mut_ptr(), out.len())
    };
    assert!(len > 0);
    let result = std::str::from_utf8(&out[..len as usize]).unwrap();
    assert_eq!(result, "&lt;b&gt;hi&lt;/b&gt;");
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
fn test_detect_mime() {
    let png = [0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A];
    let mut out = [0u8; 64];
    let len =
        unsafe { deneb_detect_mime(png.as_ptr(), png.len(), out.as_mut_ptr(), out.len()) };
    assert!(len > 0);
    let mime = std::str::from_utf8(&out[..len as usize]).unwrap();
    assert_eq!(mime, "image/png");
}

#[test]
fn test_vega_execute_stub() {
    let cmd = r#"{"command":"search","query":"test"}"#;
    let mut out = [0u8; 256];
    let len =
        unsafe { deneb_vega_execute(cmd.as_ptr(), cmd.len(), out.as_mut_ptr(), out.len()) };
    assert!(len > 0);
    let result = std::str::from_utf8(&out[..len as usize]).unwrap();
    assert!(result.contains("vega_not_implemented"));
}

#[test]
fn test_vega_search_stub() {
    let query = r#"{"query":"test"}"#;
    let mut out = [0u8; 256];
    let len =
        unsafe { deneb_vega_search(query.as_ptr(), query.len(), out.as_mut_ptr(), out.len()) };
    assert!(len > 0);
    let result = std::str::from_utf8(&out[..len as usize]).unwrap();
    assert!(result.contains("results"));
}

#[test]
fn test_extract_links_ffi() {
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
    let result = std::str::from_utf8(&out[..len as usize]).unwrap();
    assert!(result.contains("https://example.com"));
    assert!(result.contains("https://rust-lang.org"));
}

#[test]
fn test_html_to_markdown_ffi() {
    let html =
        "<html><head><title>Test</title></head><body><h1>Hello</h1><p>World</p></body></html>";
    let mut out = [0u8; 4096];
    let len = unsafe {
        deneb_html_to_markdown(html.as_ptr(), html.len(), out.as_mut_ptr(), out.len())
    };
    assert!(len > 0);
    let result = std::str::from_utf8(&out[..len as usize]).unwrap();
    assert!(result.contains("Hello"));
    assert!(result.contains("World"));
    assert!(result.contains("Test"));
}

#[test]
fn test_base64_estimate_ffi() {
    let input = "AAAA";
    let result = unsafe { deneb_base64_estimate(input.as_ptr(), input.len()) };
    assert_eq!(result, 3);
}

#[test]
fn test_base64_canonicalize_ffi() {
    let input = " A A A A ";
    let mut out = [0u8; 256];
    let len = unsafe {
        deneb_base64_canonicalize(input.as_ptr(), input.len(), out.as_mut_ptr(), out.len())
    };
    assert!(len > 0);
    let result = std::str::from_utf8(&out[..len as usize]).unwrap();
    assert_eq!(result, "AAAA");
}

#[test]
fn test_base64_canonicalize_invalid() {
    let input = "AAA"; // not multiple of 4
    let mut out = [0u8; 256];
    let len = unsafe {
        deneb_base64_canonicalize(input.as_ptr(), input.len(), out.as_mut_ptr(), out.len())
    };
    assert_eq!(len, -7); // FFI_ERR_VALIDATION: invalid base64
}

#[test]
fn test_parse_media_tokens_ffi() {
    let text = "Here is output\nMEDIA: https://example.com/img.png\nDone.";
    let mut out = [0u8; 4096];
    let len = unsafe {
        deneb_parse_media_tokens(text.as_ptr(), text.len(), out.as_mut_ptr(), out.len())
    };
    assert!(len > 0);
    let result = std::str::from_utf8(&out[..len as usize]).unwrap();
    assert!(result.contains("https://example.com/img.png"));
    assert!(result.contains("media_urls"));
}
