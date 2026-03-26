//! Security verification primitives.
//!
//! Provides constant-time comparison, input sanitization,
//! and regex safety validation — ported from `src/security/`.

use memchr::memmem;

/// Constant-time byte comparison to prevent timing attacks.
/// Both slices must be the same length for equality.
///
/// **Note:** The early return on length mismatch leaks whether the lengths
/// differ. This is acceptable because all callers compare fixed-length values
/// (session tokens, API keys) where the expected length is not secret.
/// If variable-length secret comparison is ever needed, this function must
/// be revised to avoid the length-dependent branch.
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

/// Pre-built case-insensitive finders for XSS/injection patterns.
/// Uses the `memchr` crate's SIMD-accelerated substring search to detect
/// dangerous patterns like `<script`, `javascript:`, `data:text/html`, etc.
/// Patterns are matched against a lowercased copy of the input.
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
            finders: PATTERNS.iter().map(memmem::Finder::new).collect(),
        }
    }

    fn matches(&self, haystack: &[u8]) -> bool {
        // Fast reject: all patterns start with '<', 'j', 'd', or 'o'.
        // If none of these bytes exist (case-insensitive), skip the expensive lowercase+search.
        if !haystack
            .iter()
            .any(|&b| matches!(b.to_ascii_lowercase(), b'<' | b'j' | b'd' | b'o'))
        {
            return false;
        }
        // Pre-sized allocation for small inputs avoids excess capacity; both branches heap-allocate.
        let lower: Vec<u8> = if haystack.len() <= 256 {
            let mut buf = Vec::with_capacity(haystack.len());
            buf.extend(haystack.iter().map(|b| b.to_ascii_lowercase()));
            buf
        } else {
            haystack.iter().map(|b| b.to_ascii_lowercase()).collect()
        };
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
/// Returns the input unchanged (zero-alloc via Cow) if no control characters are present.
pub fn sanitize_control_chars(input: &str) -> std::borrow::Cow<'_, str> {
    // Fast path: scan for any strippable control chars before allocating.
    if !input.chars().any(is_strippable_control) {
        return std::borrow::Cow::Borrowed(input);
    }
    std::borrow::Cow::Owned(
        input
            .chars()
            .filter(|c| !is_strippable_control(*c))
            .collect(),
    )
}

/// Maximum session key length (matches TypeScript ChatSendSessionKeyString).
const MAX_SESSION_KEY_LEN: usize = 512;

/// Validate a session key: non-empty, max 512 characters, no control characters.
/// Uses char count (not byte length) to match TypeScript's `maxLength` semantics.
pub fn is_valid_session_key(key: &str) -> bool {
    if key.is_empty() {
        return false;
    }
    // ASCII fast path: for pure-ASCII strings, byte length == char count.
    // Avoids costly chars() iteration for the common case.
    if key.is_ascii() {
        if key.len() > MAX_SESSION_KEY_LEN {
            return false;
        }
        // Check for control chars at byte level (faster than char iteration).
        return !key
            .bytes()
            .any(|b| b.is_ascii_control() && b != b'\n' && b != b'\t' && b != b'\r');
    }
    // Non-ASCII: single-pass char count + control check.
    let mut count = 0usize;
    for c in key.chars() {
        count += 1;
        if count > MAX_SESSION_KEY_LEN {
            return false;
        }
        if is_strippable_control(c) {
            return false;
        }
    }
    true
}

/// Sanitize user input by escaping HTML-significant characters.
/// Prevents XSS when user input is rendered in HTML contexts.
/// Operates at byte level since all HTML-special chars are ASCII.
pub fn sanitize_html(input: &str) -> String {
    // Fast path: no special chars — avoid allocation entirely.
    if !input
        .bytes()
        .any(|b| matches!(b, b'<' | b'>' | b'&' | b'"' | b'\''))
    {
        return input.to_string();
    }
    // All escapable characters are single-byte ASCII, so we can work at byte level.
    let mut out = Vec::with_capacity(input.len() + input.len() / 4);
    for &b in input.as_bytes() {
        match b {
            b'<' => out.extend_from_slice(b"&lt;"),
            b'>' => out.extend_from_slice(b"&gt;"),
            b'&' => out.extend_from_slice(b"&amp;"),
            b'"' => out.extend_from_slice(b"&quot;"),
            b'\'' => out.extend_from_slice(b"&#x27;"),
            _ => out.push(b),
        }
    }
    // SAFETY: input is valid UTF-8 and we only replaced single-byte ASCII characters
    // (< > & " ') with ASCII-only entity sequences (e.g., "&lt;"). Non-ASCII bytes
    // are passed through unchanged, so the output remains valid UTF-8.
    unsafe { String::from_utf8_unchecked(out) }
}

/// Remove invisible Unicode characters that can be used for prompt injection.
/// Strips: zero-width chars (U+200B-U+200F), bidi marks (U+202A-U+202E),
/// word joiners (U+2060-U+2064), deprecated format chars (U+206A-U+206F),
/// BOM (U+FEFF), and tag characters (U+E0000-U+E007F).
/// Returns the input unchanged (zero-alloc via Cow) if no invisible characters are present.
pub fn strip_invisible_unicode(input: &str) -> std::borrow::Cow<'_, str> {
    if !input.chars().any(is_invisible_unicode) {
        return std::borrow::Cow::Borrowed(input);
    }
    std::borrow::Cow::Owned(
        input
            .chars()
            .filter(|c| !is_invisible_unicode(*c))
            .collect(),
    )
}

/// Returns true if the character is an invisible Unicode character that should be stripped.
#[inline]
fn is_invisible_unicode(c: char) -> bool {
    matches!(c,
        // Zero-width and joining chars
        '\u{200B}'..='\u{200F}' |
        // Bidi control characters
        '\u{202A}'..='\u{202E}' |
        // Word joiner and invisible operators
        '\u{2060}'..='\u{2064}' |
        // Deprecated format characters
        '\u{206A}'..='\u{206F}' |
        // Byte order mark
        '\u{FEFF}' |
        // Tag characters (U+E0000-U+E007F)
        '\u{E0000}'..='\u{E007F}'
    )
}

/// Basic SSRF protection: reject URLs targeting internal/private networks.
/// Returns true if the URL appears safe for outbound requests.
/// Only lowercases the scheme+host portion to avoid copying the full URL.
pub fn is_safe_url(url: &str) -> bool {
    // Quick byte-level scheme check (case-insensitive, no alloc).
    let bytes = url.as_bytes();

    // Defense-in-depth: explicitly reject file:// URLs and UNC/network paths.
    // The scheme check below already covers file://, but explicit rejection
    // provides clearer intent, better error tracing, and protects against
    // future refactors that might weaken the scheme allowlist.
    if bytes.len() >= 5 {
        let mut lower5 = [0u8; 5];
        for (i, b) in bytes[..5].iter().enumerate() {
            lower5[i] = b.to_ascii_lowercase();
        }
        if &lower5 == b"file:" {
            return false;
        }
    }
    // UNC paths: \\host\share or //host/share (Windows SMB, NFS).
    // URLs like http://example.com start with 'h', not '/' or '\', so they
    // won't match here. Only bare //host/share without a scheme is caught.
    if bytes.len() >= 2
        && ((bytes[0] == b'\\' && bytes[1] == b'\\') || (bytes[0] == b'/' && bytes[1] == b'/'))
    {
        return false;
    }

    let scheme_len = if bytes.len() >= 8 && bytes[..8].eq_ignore_ascii_case(b"https://") {
        8
    } else if bytes.len() >= 7 && bytes[..7].eq_ignore_ascii_case(b"http://") {
        7
    } else {
        return false;
    };

    // Extract authority (up to first '/') from after the scheme.
    let rest = &url[scheme_len..];
    let authority = rest.split('/').next().unwrap_or("");
    // Strip userinfo (user:pass@host) — prevents SSRF bypass via http://evil@localhost/
    let after_userinfo = match authority.rfind('@') {
        Some(pos) => &authority[pos + 1..],
        None => authority,
    };
    // Strip port — handle IPv6 bracket notation correctly.
    // IPv6 URLs look like [::1]:8080, so only split on ':' after closing bracket.
    let host_raw = if after_userinfo.starts_with('[') {
        // IPv6: host is everything inside brackets (inclusive).
        match after_userinfo.find(']') {
            Some(end) => &after_userinfo[..=end],
            None => after_userinfo,
        }
    } else {
        after_userinfo.split(':').next().unwrap_or("")
    };
    // Stack-allocated lowercase for typical hostnames (≤253 bytes per RFC).
    let host = host_raw.to_ascii_lowercase();

    if host.is_empty() {
        return false;
    }

    // Normalize IPv6 brackets and strip zone IDs for consistent matching.
    let host_no_brackets = host.trim_start_matches('[').trim_end_matches(']');
    let host_normalized = strip_ipv6_zone_id(host_no_brackets);

    // Block common private/internal hostnames and IPs.
    const BLOCKED_HOSTS: &[&str] = &[
        "localhost",
        "127.0.0.1",
        "0.0.0.0",
        "::1",
        "::0",
        "0000:0000:0000:0000:0000:0000:0000:0001",
        "metadata.google.internal",
        "169.254.169.254",
    ];
    if BLOCKED_HOSTS.contains(&host_normalized) {
        return false;
    }

    // Block IPv4-mapped IPv6 loopback (e.g., ::ffff:127.0.0.1).
    if host_normalized.starts_with("::ffff:127.")
        || host_normalized.starts_with("::ffff:10.")
        || host_normalized.starts_with("::ffff:192.168.")
        || host_normalized.starts_with("::ffff:169.254.")
    {
        return false;
    }

    // Block IPv6 private ranges: fc00::/7 (ULA) and fe80::/10 (link-local).
    if host_normalized.starts_with("fc")
        || host_normalized.starts_with("fd")
        || host_normalized.starts_with("fe80")
    {
        return false;
    }

    // Block private IPv4 ranges (10.x, 172.16-31.x, 192.168.x).
    if host_normalized.starts_with("10.") || host_normalized.starts_with("192.168.") {
        return false;
    }
    if host_normalized.starts_with("172.") {
        if let Some(second) = host_normalized.split('.').nth(1) {
            if let Ok(n) = second.parse::<u8>() {
                if (16..=31).contains(&n) {
                    return false;
                }
            }
        }
    }

    // Block numeric IPv4 bypass techniques (octal, hex, decimal).
    if is_numeric_private_ipv4(host_normalized) {
        return false;
    }

    true
}

/// Strip IPv6 zone ID (e.g., `fe80::1%25eth0` → `fe80::1`).
/// Zone IDs appear as `%` or `%25` (URL-encoded) followed by an interface name.
fn strip_ipv6_zone_id(host: &str) -> &str {
    // Check URL-encoded `%25` first (more common in URLs), then raw `%`.
    if let Some(pos) = host.find("%25") {
        return &host[..pos];
    }
    if let Some(pos) = host.find('%') {
        return &host[..pos];
    }
    host
}

/// Detect numeric IPv4 representations that resolve to private/loopback addresses.
/// Handles: octal octets (0177.0.0.1), hex (0x7f000001), and single decimal (2130706433).
fn is_numeric_private_ipv4(host: &str) -> bool {
    // Single decimal integer (e.g., 2130706433 = 127.0.0.1).
    if let Ok(num) = host.parse::<u32>() {
        return is_private_ipv4_u32(num);
    }

    // Hex integer (e.g., 0x7f000001).
    if let Some(hex) = host.strip_prefix("0x").or_else(|| host.strip_prefix("0X")) {
        if let Ok(num) = u32::from_str_radix(hex, 16) {
            return is_private_ipv4_u32(num);
        }
    }

    // Octal/mixed-radix dotted notation (e.g., 0177.0.0.01).
    let parts: Vec<&str> = host.split('.').collect();
    if parts.len() == 4 && parts.iter().all(|p| !p.is_empty()) {
        let mut octets = [0u8; 4];
        let mut all_parsed = true;
        for (i, part) in parts.iter().enumerate() {
            match parse_octet_mixed_radix(part) {
                Some(v) => octets[i] = v,
                None => {
                    all_parsed = false;
                    break;
                }
            }
        }
        if all_parsed {
            // Only flag if this notation differs from plain decimal
            // (i.e., has octal/hex octets) AND resolves to a private IP.
            let has_non_decimal = parts.iter().any(|p| {
                p.starts_with("0x")
                    || p.starts_with("0X")
                    || (p.len() > 1 && p.starts_with('0') && p.chars().all(|c| c.is_ascii_digit()))
            });
            if has_non_decimal {
                let ip = u32::from_be_bytes(octets);
                return is_private_ipv4_u32(ip);
            }
        }
    }

    false
}

/// Parse a single octet that may be decimal, octal (0-prefix), or hex (0x-prefix).
fn parse_octet_mixed_radix(s: &str) -> Option<u8> {
    if let Some(hex) = s.strip_prefix("0x").or_else(|| s.strip_prefix("0X")) {
        u8::from_str_radix(hex, 16).ok()
    } else if s.len() > 1 && s.starts_with('0') && s.chars().all(|c| c.is_ascii_digit()) {
        // Octal notation.
        u8::from_str_radix(s, 8).ok()
    } else {
        s.parse::<u8>().ok()
    }
}

/// Check if a 32-bit IPv4 address falls in private/loopback/link-local ranges.
fn is_private_ipv4_u32(ip: u32) -> bool {
    let a = (ip >> 24) as u8;
    let b = (ip >> 16) as u8;

    // 127.0.0.0/8 (loopback)
    if a == 127 {
        return true;
    }
    // 0.0.0.0
    if ip == 0 {
        return true;
    }
    // 10.0.0.0/8
    if a == 10 {
        return true;
    }
    // 172.16.0.0/12
    if a == 172 && (16..=31).contains(&b) {
        return true;
    }
    // 192.168.0.0/16
    if a == 192 && b == 168 {
        return true;
    }
    // 169.254.0.0/16 (link-local / cloud metadata)
    if a == 169 && b == 254 {
        return true;
    }
    false
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
    fn test_is_safe_url_ipv6() {
        // IPv6 loopback variants
        assert!(!is_safe_url("http://[::1]/"));
        assert!(!is_safe_url("http://[::1]:8080/path"));

        // IPv4-mapped IPv6
        assert!(!is_safe_url("http://[::ffff:127.0.0.1]/"));
        assert!(!is_safe_url("http://[::ffff:10.0.0.1]/"));
        assert!(!is_safe_url("http://[::ffff:192.168.1.1]/"));

        // IPv6 ULA (fc00::/7) and link-local (fe80::/10)
        assert!(!is_safe_url("http://[fd12:3456::1]/"));
        assert!(!is_safe_url("http://[fc00::1]/"));
        assert!(!is_safe_url("http://[fe80::1]/"));

        // Public IPv6 should pass
        assert!(is_safe_url("http://[2001:db8::1]/"));
    }

    #[test]
    fn test_is_safe_url_metadata_ipv6() {
        // Cloud metadata via IPv4-mapped IPv6
        assert!(!is_safe_url("http://[::ffff:169.254.169.254]/"));
    }

    #[test]
    fn test_is_safe_url_numeric_bypass() {
        // Octal IPv4 (0177.0.0.1 = 127.0.0.1)
        assert!(!is_safe_url("http://0177.0.0.1/"));
        assert!(!is_safe_url("http://0177.0.0.01/admin"));

        // Hex integer (0x7f000001 = 127.0.0.1)
        assert!(!is_safe_url("http://0x7f000001/"));
        assert!(!is_safe_url("http://0X7F000001/"));

        // Decimal integer (2130706433 = 127.0.0.1)
        assert!(!is_safe_url("http://2130706433/"));

        // Octal for 10.0.0.1
        assert!(!is_safe_url("http://012.0.0.01/"));

        // Hex for 192.168.1.1 = 0xC0A80101
        assert!(!is_safe_url("http://0xC0A80101/"));

        // Decimal for 169.254.169.254 = 2852039166
        assert!(!is_safe_url("http://2852039166/"));

        // Public IP in decimal should pass (8.8.8.8 = 134744072)
        assert!(is_safe_url("http://134744072/"));

        // Normal dotted decimal (not octal) should still work through existing checks
        assert!(!is_safe_url("http://127.0.0.1/"));
        assert!(is_safe_url("http://8.8.8.8/"));
    }

    #[test]
    fn test_is_safe_url_ipv6_zone_id() {
        // IPv6 with zone ID (URL-encoded %25)
        assert!(!is_safe_url("http://[fe80::1%25eth0]/"));
        assert!(!is_safe_url("http://[::1%25lo]/"));

        // IPv6 zone ID with raw %
        assert!(!is_safe_url("http://[fe80::1%eth0]/"));
    }

    #[test]
    fn test_file_url_blocked() {
        assert!(!is_safe_url("file:///etc/passwd"));
        assert!(!is_safe_url("FILE:///etc/passwd"));
        assert!(!is_safe_url("File:///etc/passwd"));
        assert!(!is_safe_url("file://localhost/etc/passwd"));
        assert!(!is_safe_url("file:///C:/Windows/System32"));
        assert!(!is_safe_url("file:\\\\C:\\Windows\\System32"));
    }

    #[test]
    fn test_unc_path_blocked() {
        assert!(!is_safe_url("\\\\server\\share"));
        assert!(!is_safe_url("\\\\?\\UNC\\server\\share"));
        assert!(!is_safe_url("//server/share"));
        assert!(!is_safe_url("//169.254.169.254/latest/meta-data"));
    }

    #[test]
    fn test_strip_invisible_unicode() {
        // No invisible chars — returns borrowed
        assert_eq!(strip_invisible_unicode("hello world"), "hello world");
        // Zero-width space
        assert_eq!(strip_invisible_unicode("hello\u{200B}world"), "helloworld");
        // BOM
        assert_eq!(strip_invisible_unicode("\u{FEFF}hello"), "hello");
        // Bidi marks
        assert_eq!(
            strip_invisible_unicode("hello\u{202A}\u{202C}world"),
            "helloworld"
        );
        // Word joiner
        assert_eq!(strip_invisible_unicode("a\u{2060}b"), "ab");
        // Tag characters
        assert_eq!(strip_invisible_unicode("a\u{E0001}b"), "ab");
        // Mixed invisible chars
        assert_eq!(
            strip_invisible_unicode(
                "\u{200B}\u{200C}\u{200D}\u{2060}\u{FEFF}text\u{E0000}\u{E007F}"
            ),
            "text"
        );
        // Preserves normal Unicode (emoji, CJK, accents)
        assert_eq!(strip_invisible_unicode("café 🎉 한글"), "café 🎉 한글");
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
