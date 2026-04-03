use super::*;

#[test]
fn test_constant_time_eq() {
    let cases: &[(&[u8], &[u8], bool)] = &[
        (b"hello", b"hello", true),
        (b"hello", b"world", false),
        (b"short", b"longer", false),
        (b"", b"", true),
    ];
    for (a, b, want) in cases {
        assert_eq!(
            constant_time_eq(a, b),
            *want,
            "constant_time_eq({a:?}, {b:?})"
        );
    }
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
