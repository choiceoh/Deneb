//! Security verification primitives.
//!
//! Provides constant-time comparison, input sanitization,
//! and regex safety validation — ported from `src/security/`.

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

/// Case-insensitive substring search without allocating a lowercase copy.
fn contains_ignore_case(haystack: &[u8], needle: &[u8]) -> bool {
    if needle.len() > haystack.len() {
        return false;
    }
    haystack
        .windows(needle.len())
        .any(|window| window.eq_ignore_ascii_case(needle))
}

/// Check if a string contains potential injection patterns.
/// Returns true if the input appears safe.
pub fn is_safe_input(input: &str) -> bool {
    let bytes = input.as_bytes();
    // Reject null bytes (fast memchr-style scan).
    if bytes.contains(&0) {
        return false;
    }
    // Reject common injection patterns (case-insensitive, zero-alloc).
    const DANGEROUS: &[&[u8]] = &[
        b"<script",
        b"javascript:",
        b"data:text/html",
        b"onerror=",
        b"onload=",
    ];
    !DANGEROUS.iter().any(|p| contains_ignore_case(bytes, p))
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
