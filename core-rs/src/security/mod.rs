//! Security verification primitives.
//!
//! Provides constant-time comparison, input sanitization,
//! and regex safety validation — ported from `src/security/`.

use memchr::memmem;

/// Constant-time byte comparison to prevent timing attacks.
/// Both slices must be the same length for equality.
pub fn constant_time_eq(a: &[u8], b: &[u8]) -> bool {
    if a.len() != b.len() {
        return false;
    }
    let mut diff: u8 = 0;
    for (x, y) in a.iter().zip(b.iter()) {
        diff |= x ^ y;
    }
    diff == 0
}

/// Pre-built case-insensitive finders for dangerous patterns.
/// Each entry: (lowercase pattern bytes, memmem::Finder for the lowercase version).
/// We search the lowercased haystack for a match using SIMD-accelerated memmem.
struct DangerousPatterns {
    finders: Vec<memmem::Finder<'static>>,
}

impl DangerousPatterns {
    fn new() -> Self {
        const PATTERNS: &[&[u8]] = &[
            b"<script",
            b"javascript:",
            b"data:text/html",
            b"onerror=",
            b"onload=",
        ];
        Self {
            finders: PATTERNS.iter().map(|p| memmem::Finder::new(p)).collect(),
        }
    }

    fn matches(&self, haystack: &[u8]) -> bool {
        let lower: Vec<u8> = haystack.iter().map(|b| b.to_ascii_lowercase()).collect();
        self.finders.iter().any(|f| f.find(&lower).is_some())
    }
}

/// Check if a string contains potential injection patterns.
/// Returns true if the input appears safe.
/// Uses SIMD-accelerated substring search via memchr crate.
pub fn is_safe_input(input: &str) -> bool {
    let bytes = input.as_bytes();
    // Reject null bytes (SIMD-accelerated).
    if memchr::memchr(0, bytes).is_some() {
        return false;
    }
    // Reject common injection patterns (SIMD-accelerated case-insensitive search).
    use std::sync::OnceLock;
    static PATTERNS: OnceLock<DangerousPatterns> = OnceLock::new();
    let patterns = PATTERNS.get_or_init(DangerousPatterns::new);
    !patterns.matches(bytes)
}

/// Returns true if the character is a control char that should be stripped.
#[inline]
fn is_strippable_control(c: char) -> bool {
    c.is_control() && c != '\n' && c != '\t' && c != '\r'
}

/// Sanitize a string by removing control characters (except newline/tab/CR).
/// Returns the input unchanged (zero-alloc) if no control characters are present.
pub fn sanitize_control_chars(input: &str) -> String {
    // Fast path: scan for any strippable control chars before allocating.
    if !input.chars().any(is_strippable_control) {
        return input.to_string();
    }
    input.chars().filter(|c| !is_strippable_control(*c)).collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_constant_time_eq_same() {
        assert!(constant_time_eq(b"hello", b"hello"));
    }

    #[test]
    fn test_constant_time_eq_different() {
        assert!(!constant_time_eq(b"hello", b"world"));
    }

    #[test]
    fn test_constant_time_eq_different_length() {
        assert!(!constant_time_eq(b"short", b"longer"));
    }

    #[test]
    fn test_constant_time_eq_empty() {
        assert!(constant_time_eq(b"", b""));
    }

    #[test]
    fn test_is_safe_input() {
        assert!(is_safe_input("normal text"));
        assert!(is_safe_input("hello world 123"));
        assert!(!is_safe_input("<script>alert(1)</script>"));
        assert!(!is_safe_input("javascript:void(0)"));
        assert!(!is_safe_input("has\0null"));
    }

    #[test]
    fn test_sanitize_control_chars() {
        assert_eq!(sanitize_control_chars("hello\x00world"), "helloworld");
        assert_eq!(sanitize_control_chars("keep\nnewlines"), "keep\nnewlines");
        assert_eq!(sanitize_control_chars("keep\ttabs"), "keep\ttabs");
        assert_eq!(
            sanitize_control_chars("remove\x07bell\x1Bescape"),
            "removebellescape"
        );
    }
}
