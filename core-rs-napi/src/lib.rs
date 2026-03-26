//! Unified napi-rs native addon for Deneb.
//!
//! Combines core-rs bridge (protocol, security, media) with performance-critical
//! utilities (gitignore, EXIF, PNG) in a single .node binary.

mod exif;
mod gitignore;
mod png;

use napi::bindgen_prelude::*;
use napi_derive::napi;

/// Hard cap on JSON input to prevent OOM during serde parsing (16 MiB).
const MAX_JSON_BYTES: usize = 16 * 1024 * 1024;

/// Hard cap on string input for sanitization and injection checks (64 MiB).
const MAX_STRING_BYTES: usize = 64 * 1024 * 1024;

/// Hard cap on buffer size for constant-time comparison to prevent event-loop stall (256 MiB).
const MAX_COMPARE_BYTES: usize = 256 * 1024 * 1024;

/// Frame type enum values returned by validate_frame.
/// 0 = req, 1 = res, 2 = event. Mapped to strings on the TS side.
const FRAME_TYPE_REQ: u32 = 0;
const FRAME_TYPE_RES: u32 = 1;
const FRAME_TYPE_EVENT: u32 = 2;

/// Validate a gateway protocol frame (JSON string).
/// Returns the frame type as a numeric ID (0=req, 1=res, 2=event) on success.
/// Uses fast envelope-only validation (skips deep parsing of payload/params/error).
/// Uses zero-copy JsString to avoid Rust String allocation on the input side.
/// Throws a JS error if the frame is invalid or exceeds the size limit.
#[napi]
pub fn validate_frame(_env: Env, json: napi::JsString) -> Result<u32> {
    let json_utf8 = json.into_utf8()?;
    let json_str = json_utf8.as_str()?;
    if json_str.len() > MAX_JSON_BYTES {
        return Err(Error::from_reason(format!(
            "frame JSON exceeds size limit ({} > {} bytes)",
            json_str.len(),
            MAX_JSON_BYTES
        )));
    }
    deneb_core::protocol::validate_frame_type(json_str)
        .map(|ft| match ft {
            deneb_core::protocol::FrameType::Req => FRAME_TYPE_REQ,
            deneb_core::protocol::FrameType::Res => FRAME_TYPE_RES,
            deneb_core::protocol::FrameType::Event => FRAME_TYPE_EVENT,
        })
        .map_err(|e| Error::from_reason(e.to_string()))
}

/// Constant-time byte comparison to prevent timing attacks.
/// Both buffers must be the same length for equality.
/// Returns false immediately if either buffer exceeds the size limit.
#[napi]
pub fn constant_time_eq(a: Buffer, b: Buffer) -> bool {
    let (a, b) = (a.as_ref(), b.as_ref());
    if a.len() > MAX_COMPARE_BYTES || b.len() > MAX_COMPARE_BYTES {
        return false;
    }
    deneb_core::security::constant_time_eq(a, b)
}

/// Detect MIME type from file magic bytes.
/// Only reads the first 4096 bytes for detection regardless of buffer size.
/// Returns the MIME string (e.g. "image/png") or "application/octet-stream" for unknown.
#[napi]
pub fn detect_mime(env: Env, data: Buffer) -> Result<napi::JsString> {
    let slice = data.as_ref();
    // Only the first bytes matter for magic-byte sniffing; cap to avoid unnecessary work.
    let head = if slice.len() > 4096 {
        &slice[..4096]
    } else {
        slice
    };
    // Return &'static str directly via napi Env — avoids Rust String allocation.
    env.create_string(deneb_core::media::detect_mime(head))
}

/// Check if a string contains potential injection patterns.
/// Returns true if the input appears safe.
/// Returns false (unsafe) for inputs exceeding the size limit as a safe default.
#[napi]
pub fn is_safe_input(input: String) -> bool {
    if input.len() > MAX_STRING_BYTES {
        return false;
    }
    deneb_core::security::is_safe_input(&input)
}

/// Sanitize a string by removing control characters (except newline/tab/CR).
/// Returns the input unchanged if it exceeds the size limit (safe default).
#[napi]
pub fn sanitize_control_chars(input: String) -> String {
    if input.len() > MAX_STRING_BYTES {
        return input;
    }
    deneb_core::security::sanitize_control_chars(&input).into_owned()
}

/// Remove invisible Unicode characters (zero-width, bidi marks, tag chars, BOM, etc.).
/// Returns the input unchanged if no invisible characters are present.
#[napi]
pub fn strip_invisible_unicode(input: String) -> String {
    if input.len() > MAX_STRING_BYTES {
        return input;
    }
    deneb_core::security::strip_invisible_unicode(&input).into_owned()
}

// ---------------------------------------------------------------------------
// Parsing functions (HTML→Markdown, URL extraction, media tokens, base64)
// ---------------------------------------------------------------------------

/// Convert HTML to markdown. Returns JSON `{"text":"...","title":"..."}`.
#[napi]
pub fn parsing_html_to_markdown(html: String) -> String {
    if html.len() > MAX_JSON_BYTES {
        return r#"{"text":"","title":null}"#.to_string();
    }
    let result = deneb_core::parsing::html_to_markdown::html_to_markdown(&html);
    serde_json::to_string(&result).unwrap_or_else(|_| r#"{"text":"","title":null}"#.to_string())
}

/// Extract safe links from text. Returns JSON array of URL strings.
#[napi]
pub fn parsing_extract_links(text: String, config_json: String) -> String {
    if text.len() > MAX_JSON_BYTES {
        return "[]".to_string();
    }
    let config: deneb_core::parsing::url_extract::ExtractLinksConfig =
        serde_json::from_str(&config_json).unwrap_or_default();
    let links = deneb_core::parsing::url_extract::extract_links(&text, &config);
    serde_json::to_string(&links).unwrap_or_else(|_| "[]".to_string())
}

/// Parse MEDIA: tokens from command output. Returns JSON MediaParseResult.
#[napi]
pub fn parsing_split_media_from_output(raw: String) -> String {
    if raw.len() > MAX_JSON_BYTES {
        return r#"{"text":"","media_urls":[]}"#.to_string();
    }
    let result = deneb_core::parsing::media_tokens::split_media_from_output(&raw);
    serde_json::to_string(&result)
        .unwrap_or_else(|_| r#"{"text":"","media_urls":[]}"#.to_string())
}

/// Estimate decoded byte size from base64 string length.
#[napi]
pub fn parsing_estimate_base64_decoded_bytes(input: String) -> u32 {
    deneb_core::parsing::base64_util::estimate_base64_decoded_bytes(&input) as u32
}

/// Normalize and validate a base64 string. Returns canonical form or null.
#[napi]
pub fn parsing_canonicalize_base64(input: String) -> Option<String> {
    deneb_core::parsing::base64_util::canonicalize_base64(&input)
}

// ---------------------------------------------------------------------------
// Media helper functions (extension, category, classification)
// ---------------------------------------------------------------------------

fn category_to_str(cat: deneb_core::media::extensions::MediaCategory) -> &'static str {
    match cat {
        deneb_core::media::extensions::MediaCategory::Image => "image",
        deneb_core::media::extensions::MediaCategory::Audio => "audio",
        deneb_core::media::extensions::MediaCategory::Video => "video",
        deneb_core::media::extensions::MediaCategory::Document => "document",
        deneb_core::media::extensions::MediaCategory::Archive => "archive",
        deneb_core::media::extensions::MediaCategory::Text => "text",
        deneb_core::media::extensions::MediaCategory::Unknown => "unknown",
    }
}

/// Get file extension for a MIME type (e.g., "image/png" → "png").
#[napi]
pub fn media_extension_for_mime(mime: String) -> String {
    deneb_core::media::extensions::extension_for_mime(&mime).to_string()
}

/// Get media category for a MIME type.
/// Returns one of: "image", "audio", "video", "document", "archive", "text", "unknown".
#[napi]
pub fn media_category_for_mime(mime: String) -> String {
    category_to_str(deneb_core::media::extensions::category_for_mime(&mime)).to_string()
}

/// Detect MIME type from magic bytes and return full info as JSON.
/// Returns `{"mime":"...","extension":"...","category":"..."}`.
#[napi]
pub fn media_detect_mime_with_info(data: Buffer) -> String {
    let slice = data.as_ref();
    let head = if slice.len() > 4096 { &slice[..4096] } else { slice };
    let info = deneb_core::media::extensions::detect_mime_with_info(head);
    let cat = category_to_str(info.category);
    // Use serde_json to guarantee valid JSON escaping.
    serde_json::json!({
        "mime": info.mime,
        "extension": info.extension,
        "category": cat,
    })
    .to_string()
}

/// Check if a MIME type is an image format.
#[napi]
pub fn media_is_image(mime: String) -> bool {
    deneb_core::media::extensions::is_image(&mime)
}

/// Check if a MIME type is an audio format.
#[napi]
pub fn media_is_audio(mime: String) -> bool {
    deneb_core::media::extensions::is_audio(&mime)
}

/// Check if a MIME type is a video format.
#[napi]
pub fn media_is_video(mime: String) -> bool {
    deneb_core::media::extensions::is_video(&mime)
}

// ---------------------------------------------------------------------------
// Security & validation functions (Phase 2 — bridge Rust FFI to Node.js)
// ---------------------------------------------------------------------------

/// Validate a session key: non-empty, max 512 characters, no control characters.
/// Returns true if the key is valid.
#[napi]
pub fn validate_session_key(key: String) -> bool {
    if key.len() > MAX_STRING_BYTES {
        return false;
    }
    deneb_core::security::is_valid_session_key(&key)
}

/// Sanitize HTML by escaping significant characters (<, >, &, ", ').
/// Prevents XSS when user input is rendered in HTML contexts.
/// Returns the input unchanged for inputs exceeding 1 MB (safe default).
#[napi]
pub fn sanitize_html(input: String) -> String {
    // Match Go's 1 MB limit to prevent OOM from 6x expansion.
    const MAX_SANITIZE_INPUT: usize = 1024 * 1024;
    if input.len() > MAX_SANITIZE_INPUT {
        return input;
    }
    deneb_core::security::sanitize_html(&input)
}

/// Check if a URL is safe for outbound requests (not targeting internal/private networks).
/// Returns true if the URL is safe, false if it targets private networks or is malformed.
#[napi]
pub fn is_safe_url(url: String) -> bool {
    if url.is_empty() || url.len() > 8192 {
        return false;
    }
    deneb_core::security::is_safe_url(&url)
}

/// Validate that an error code string is a known gateway error code.
#[napi]
pub fn validate_error_code(code: String) -> bool {
    deneb_core::protocol::error_codes::is_valid_error_code(&code)
}

/// Check if an error code is retryable by default.
/// Returns false for unknown codes.
#[napi]
pub fn is_retryable_error_code(code: String) -> bool {
    deneb_core::protocol::error_codes::ErrorCode::parse(&code)
        .map(|c| c.is_retryable())
        .unwrap_or(false)
}

// ---------------------------------------------------------------------------
// Protocol schema validation (Phase 3 — RPC parameter validation in Rust)
// ---------------------------------------------------------------------------

/// Validate RPC parameters for a given method name.
/// Returns a JSON string with the validation result: `{ "valid": true/false, "errors": [...] }`.
/// Throws if the method name is unknown or JSON is invalid.
#[napi]
pub fn validate_params(method: String, json: String) -> Result<String> {
    if json.len() > MAX_JSON_BYTES {
        return Err(Error::from_reason(format!(
            "params JSON exceeds size limit ({} > {} bytes)",
            json.len(),
            MAX_JSON_BYTES
        )));
    }
    match deneb_core::protocol::validation::validate_params(&method, &json) {
        Ok(result) => serde_json::to_string(&result)
            .map_err(|e| Error::from_reason(format!("serialization error: {e}"))),
        Err(deneb_core::protocol::validation::ValidateParamsError::UnknownMethod(m)) => {
            Err(Error::from_reason(format!("unknown method: {m}")))
        }
        Err(deneb_core::protocol::validation::ValidateParamsError::InvalidJson(e)) => {
            Err(Error::from_reason(format!("invalid JSON: {e}")))
        }
    }
}
