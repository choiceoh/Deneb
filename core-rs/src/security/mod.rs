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

/// Maximum session key length (matches TypeScript ChatSendSessionKeyString).
const MAX_SESSION_KEY_LEN: usize = 512;

/// Validate a session key: non-empty, max 512 characters, no control characters.
/// Uses char count (not byte length) to match TypeScript's `maxLength` semantics.
pub fn is_valid_session_key(key: &str) -> bool {
    if key.is_empty() {
        return false;
    }
    let char_count = key.chars().count();
    if char_count > MAX_SESSION_KEY_LEN {
        return false;
    }
    // Reject control characters (except common whitespace).
    !key.chars().any(is_strippable_control)
}

/// Sanitize user input by escaping HTML-significant characters.
/// Prevents XSS when user input is rendered in HTML contexts.
pub fn sanitize_html(input: &str) -> String {
    // Fast path: no special chars.
    if !input.bytes().any(|b| matches!(b, b'<' | b'>' | b'&' | b'"' | b'\'')) {
        return input.to_string();
    }
    let mut out = String::with_capacity(input.len() + 16);
    for c in input.chars() {
        match c {
            '<' => out.push_str("&lt;"),
            '>' => out.push_str("&gt;"),
            '&' => out.push_str("&amp;"),
            '"' => out.push_str("&quot;"),
            '\'' => out.push_str("&#x27;"),
            _ => out.push(c),
        }
    }
    out
}

/// Basic SSRF protection: reject URLs targeting internal/private networks.
/// Returns true if the URL appears safe for outbound requests.
pub fn is_safe_url(url: &str) -> bool {
    let lower = url.to_ascii_lowercase();

    // Must be http or https.
    if !lower.starts_with("http://") && !lower.starts_with("https://") {
        return false;
    }

    // Extract host portion (strip userinfo, port, and path).
    let after_scheme = if lower.starts_with("https://") {
        &lower[8..]
    } else {
        &lower[7..]
    };
    let authority = after_scheme.split('/').next().unwrap_or("");
    // Strip userinfo (user:pass@host) — prevents SSRF bypass via http://evil@localhost/
    let after_userinfo = match authority.rfind('@') {
        Some(pos) => &authority[pos + 1..],
        None => authority,
    };
    // Strip port
    let host = after_userinfo.split(':').next().unwrap_or("");

    if host.is_empty() {
        return false;
    }

    // Block common private/internal hostnames and IPs.
    const BLOCKED_HOSTS: &[&str] = &[
        "localhost",
        "127.0.0.1",
        "0.0.0.0",
        "[::1]",
        "metadata.google.internal",
        "169.254.169.254",
    ];
    if BLOCKED_HOSTS.iter().any(|&b| host == b) {
        return false;
    }

    // Block private IP ranges (10.x, 172.16-31.x, 192.168.x).
    if host.starts_with("10.") || host.starts_with("192.168.") {
        return false;
    }
    if host.starts_with("172.") {
        if let Some(second) = host.split('.').nth(1) {
            if let Ok(n) = second.parse::<u8>() {
                if (16..=31).contains(&n) {
                    return false;
                }
            }
        }
    }

    true
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

    #[test]
    fn test_is_valid_session_key() {
        assert!(is_valid_session_key("my-session-123"));
        assert!(is_valid_session_key("a")); // min length 1
        assert!(!is_valid_session_key("")); // empty
        assert!(!is_valid_session_key(&"x".repeat(513))); // too long
        assert!(is_valid_session_key(&"x".repeat(512))); // exactly at limit
        assert!(!is_valid_session_key("has\x00null")); // control char
    }

    #[test]
    fn test_sanitize_html() {
        assert_eq!(sanitize_html("hello"), "hello");
        assert_eq!(sanitize_html("<script>"), "&lt;script&gt;");
        assert_eq!(sanitize_html("a & b"), "a &amp; b");
        assert_eq!(sanitize_html("\"quoted\""), "&quot;quoted&quot;");
        assert_eq!(sanitize_html("it's"), "it&#x27;s");
        // Mixed
        assert_eq!(
            sanitize_html("<div class=\"x\">a & b</div>"),
            "&lt;div class=&quot;x&quot;&gt;a &amp; b&lt;/div&gt;"
        );
    }

    #[test]
    fn test_is_safe_url() {
        // Safe URLs
        assert!(is_safe_url("https://example.com/api"));
        assert!(is_safe_url("http://cdn.example.com/image.png"));

        // Blocked: private networks
        assert!(!is_safe_url("http://localhost/admin"));
        assert!(!is_safe_url("http://127.0.0.1:8080/"));
        assert!(!is_safe_url("http://0.0.0.0/"));
        assert!(!is_safe_url("http://10.0.0.1/secret"));
        assert!(!is_safe_url("http://192.168.1.1/"));
        assert!(!is_safe_url("http://172.16.0.1/"));
        assert!(!is_safe_url("http://172.31.255.255/"));
        assert!(is_safe_url("http://172.32.0.1/")); // 172.32 is public

        // Blocked: cloud metadata
        assert!(!is_safe_url("http://169.254.169.254/latest/meta-data/"));
        assert!(!is_safe_url("http://metadata.google.internal/"));

        // Blocked: non-http schemes
        assert!(!is_safe_url("ftp://example.com/file"));
        assert!(!is_safe_url("file:///etc/passwd"));
        assert!(!is_safe_url("javascript:alert(1)"));

        // Blocked: empty/malformed
        assert!(!is_safe_url(""));
        assert!(!is_safe_url("http://"));

        // Blocked: userinfo bypass attempts
        assert!(!is_safe_url("http://evil@localhost/"));
        assert!(!is_safe_url("http://user:pass@127.0.0.1/"));
        assert!(!is_safe_url("http://anything@10.0.0.1/secret"));
        assert!(is_safe_url("http://user@example.com/")); // public host with userinfo is ok
    }

    #[test]
    fn test_is_valid_session_key_multibyte() {
        // Multibyte chars: 512 chars is the limit, not 512 bytes.
        let key_512_chars: String = "a".repeat(512);
        assert!(is_valid_session_key(&key_512_chars));

        let key_513_chars: String = "a".repeat(513);
        assert!(!is_valid_session_key(&key_513_chars));

        // 256 two-byte chars = 256 chars, 512 bytes — should pass (under 512 char limit)
        let multibyte_key: String = "\u{00e9}".repeat(256); // e-accent, 2 bytes each
        assert!(is_valid_session_key(&multibyte_key));
        assert_eq!(multibyte_key.chars().count(), 256);
        assert_eq!(multibyte_key.len(), 512); // 512 bytes but only 256 chars
    }
}
