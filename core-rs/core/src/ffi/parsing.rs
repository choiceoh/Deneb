//! Parsing FFI exports (link extraction, HTML-to-Markdown, base64, media tokens).

#![allow(unsafe_code)]

use super::helpers::{ffi_read_str, ffi_write_bytes, ffi_write_json};
use super::*;
use crate::parsing;

/// C FFI: Extract links from message text.
/// Takes the message text and a JSON config `{"max_links": N}`.
/// Writes a JSON array of URL strings to `out_ptr`.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// All pointers must be valid for their respective lengths.
#[no_mangle]
pub unsafe extern "C" fn deneb_extract_links(
    text_ptr: *const u8,
    text_len: usize,
    config_ptr: *const u8,
    config_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if text_ptr.is_null() || config_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if text_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    // SAFETY: text_ptr, config_ptr, and out_ptr are null-checked above, text_len
    // bounded by FFI_MAX_INPUT_LEN. The Go caller guarantees all buffers are valid.
    let text_slice = std::slice::from_raw_parts(text_ptr, text_len);
    let config_slice = std::slice::from_raw_parts(config_ptr, config_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let text_str = match std::str::from_utf8(text_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let config_str = match std::str::from_utf8(config_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };

        #[derive(serde::Deserialize)]
        struct ConfigInput {
            #[serde(default = "default_max_links")]
            max_links: usize,
        }
        fn default_max_links() -> usize {
            5
        }

        let config: ConfigInput = match serde_json::from_str(config_str) {
            Ok(c) => c,
            Err(_) => return FFI_ERR_JSON,
        };
        let cfg = parsing::url_extract::ExtractLinksConfig {
            max_links: config.max_links,
        };
        let urls = parsing::url_extract::extract_links(text_str, &cfg);
        ffi_write_json(&urls, out_slice)
    })
}

/// C FFI: Convert HTML to Markdown.
/// Writes JSON `{"text":"...","title":"..."}` to `out_ptr`.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `html_ptr` must be valid UTF-8. `out_ptr` must be writable.
#[no_mangle]
pub unsafe extern "C" fn deneb_html_to_markdown(
    html_ptr: *const u8,
    html_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    let str_result = ffi_read_str(html_ptr, html_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let html_str = match str_result {
            Ok(s) => s,
            Err(e) => return e,
        };
        let result = parsing::html_to_markdown::html_to_markdown(html_str);
        ffi_write_json(&result, out_slice)
    })
}

/// C FFI: Estimate decoded size of a base64 string.
/// Returns estimated byte count (>= 0) on success, negative on error.
///
/// # Safety
/// `input_ptr` must be valid UTF-8.
#[no_mangle]
pub unsafe extern "C" fn deneb_base64_estimate(input_ptr: *const u8, input_len: usize) -> i64 {
    if input_ptr.is_null() {
        return FFI_ERR_NULL_PTR as i64;
    }
    if input_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE as i64;
    }
    // SAFETY: input_ptr is null-checked above, input_len bounded by FFI_MAX_INPUT_LEN.
    let slice = std::slice::from_raw_parts(input_ptr, input_len);
    std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        let input_str = match std::str::from_utf8(slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8 as i64,
        };
        parsing::base64_util::estimate_base64_decoded_bytes(input_str) as i64
    }))
    .unwrap_or(FFI_ERR_PANIC as i64)
}

/// C FFI: Canonicalize a base64 string (strip whitespace, validate).
/// Writes the canonical base64 string to `out_ptr`.
/// Returns bytes written on success, -3 if invalid, other negatives on error.
///
/// # Safety
/// `input_ptr` must be valid UTF-8. `out_ptr` must be writable.
#[no_mangle]
pub unsafe extern "C" fn deneb_base64_canonicalize(
    input_ptr: *const u8,
    input_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    let str_result = ffi_read_str(input_ptr, input_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let input_str = match str_result {
            Ok(s) => s,
            Err(e) => return e,
        };
        match parsing::base64_util::canonicalize_base64(input_str) {
            Some(canonical) => ffi_write_bytes(canonical.as_bytes(), out_slice),
            None => FFI_ERR_VALIDATION, // invalid base64
        }
    })
}

/// C FFI: Parse MEDIA: tokens from text output.
/// Writes JSON `{"text":"...","media_urls":[...],"audio_as_voice":bool}` to `out_ptr`.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `text_ptr` must be valid UTF-8. `out_ptr` must be writable.
#[no_mangle]
pub unsafe extern "C" fn deneb_parse_media_tokens(
    text_ptr: *const u8,
    text_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    let str_result = ffi_read_str(text_ptr, text_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let text_str = match str_result {
            Ok(s) => s,
            Err(e) => return e,
        };
        let result = parsing::media_tokens::split_media_from_output(text_str);
        ffi_write_json(&result, out_slice)
    })
}
