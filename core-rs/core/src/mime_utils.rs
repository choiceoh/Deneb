//! MIME type normalization utilities.
//!
//! Ports pure functions from `src/media/mime.ts`:
//! - `normalizeMimeType` — split on `;`, trim, lowercase
//! - `isGenericMime` — check for generic container types

#[cfg(feature = "napi_binding")]
use napi::bindgen_prelude::*;

// ---------------------------------------------------------------------------
// Implementation
// ---------------------------------------------------------------------------

/// Normalize a MIME type string: split on `;`, trim, lowercase.
/// Returns None for empty or missing input.
pub fn normalize_mime_type_impl(mime: &str) -> Option<String> {
    if mime.is_empty() {
        return None;
    }
    let cleaned = mime.split(';').next()?.trim().to_lowercase();
    if cleaned.is_empty() {
        return None;
    }
    Some(cleaned)
}

/// Check if a MIME type is a generic container (octet-stream or zip).
pub fn is_generic_mime_impl(mime: &str) -> bool {
    if mime.is_empty() {
        return true;
    }
    let m = mime.to_lowercase();
    m == "application/octet-stream" || m == "application/zip"
}

// ---------------------------------------------------------------------------
// napi exports
// ---------------------------------------------------------------------------

/// Normalize a MIME type: extract base type, trim, lowercase.
/// Returns null for empty input.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn normalize_mime_type(mime: String) -> Option<String> {
    normalize_mime_type_impl(&mime)
}

/// Check if a MIME type is a generic container (octet-stream or zip).
#[cfg_attr(feature = "napi_binding", napi)]
pub fn is_generic_mime(mime: String) -> bool {
    is_generic_mime_impl(&mime)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn normalize_basic() {
        assert_eq!(
            normalize_mime_type_impl("image/jpeg"),
            Some("image/jpeg".to_string())
        );
    }

    #[test]
    fn normalize_with_params() {
        assert_eq!(
            normalize_mime_type_impl("text/html; charset=utf-8"),
            Some("text/html".to_string())
        );
    }

    #[test]
    fn normalize_trims_whitespace() {
        assert_eq!(
            normalize_mime_type_impl("  IMAGE/PNG  "),
            Some("image/png".to_string())
        );
    }

    #[test]
    fn normalize_empty() {
        assert_eq!(normalize_mime_type_impl(""), None);
    }

    #[test]
    fn normalize_semicolon_only() {
        assert_eq!(normalize_mime_type_impl("; charset=utf-8"), None);
    }

    #[test]
    fn is_generic_octet_stream() {
        assert!(is_generic_mime_impl("application/octet-stream"));
    }

    #[test]
    fn is_generic_zip() {
        assert!(is_generic_mime_impl("application/zip"));
    }

    #[test]
    fn is_generic_case_insensitive() {
        assert!(is_generic_mime_impl("APPLICATION/OCTET-STREAM"));
    }

    #[test]
    fn is_not_generic_jpeg() {
        assert!(!is_generic_mime_impl("image/jpeg"));
    }

    #[test]
    fn is_generic_empty() {
        assert!(is_generic_mime_impl(""));
    }
}
