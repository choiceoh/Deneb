//! URL extraction from message text.
//!
//! Ports `src/link-understanding/detect.ts:extractLinksFromMessage`.
//! Strips markdown link syntax, extracts bare `https?://` URLs,
//! deduplicates, SSRF-checks each via `security::is_safe_url`, and
//! limits to a configurable maximum.

use crate::security;
use std::collections::HashSet;

/// Default maximum number of links to extract.
const DEFAULT_MAX_LINKS: usize = 5;

/// Schemes recognized for URL extraction beyond http/https.
const EXTRA_SCHEMES: &[&str] = &["ftp://"];

/// Configuration for link extraction.
#[derive(serde::Deserialize)]
pub struct ExtractLinksConfig {
    pub max_links: usize,
}

impl Default for ExtractLinksConfig {
    fn default() -> Self {
        Self {
            max_links: DEFAULT_MAX_LINKS,
        }
    }
}

/// Strip markdown link syntax `[text](url)` by replacing the entire match
/// with a space so the URL inside the parens is NOT picked up as a bare link.
fn strip_markdown_links(input: &str) -> String {
    let bytes = input.as_bytes();
    let len = bytes.len();
    let mut out = String::with_capacity(len);
    let mut i = 0;

    while i < len {
        if bytes[i] == b'[' {
            // Try to match [...](...) pattern.
            if let Some(end) = match_markdown_link(bytes, i) {
                // Replace the entire [text](url) with a space.
                out.push(' ');
                i = end;
                continue;
            }
        }
        // Copy the original str slice for correct UTF-8 handling.
        // Advance past the full character (1–4 bytes) to avoid splitting
        // multi-byte sequences like Korean or emoji.
        let ch_len = utf8_char_width(bytes[i]);
        if let Some(s) = input.get(i..i + ch_len) {
            out.push_str(s);
        }
        i += ch_len;
    }
    out
}

/// Return the byte length of the UTF-8 character starting with `b`.
#[inline]
fn utf8_char_width(b: u8) -> usize {
    match b {
        0..=0x7F => 1,
        0xC0..=0xDF => 2,
        0xE0..=0xEF => 3,
        0xF0..=0xFF => 4,
        _ => 1,
    }
}

/// Try to match `[...](https?://...)` starting at `start`.
/// Returns the index past the closing `)` on success.
fn match_markdown_link(bytes: &[u8], start: usize) -> Option<usize> {
    let len = bytes.len();
    // Find closing `]` using depth tracking to handle nested brackets,
    // e.g., `[link [sub] text](url)` correctly matches the outermost pair.
    let mut i = start + 1;
    let mut depth = 1;
    while i < len && depth > 0 {
        match bytes[i] {
            b'[' => depth += 1,
            b']' => depth -= 1,
            _ => {}
        }
        i += 1;
    }
    if depth != 0 || i >= len || bytes[i] != b'(' {
        return None;
    }
    // `i` is at `(`. Find matching `)`.
    let paren_start = i;
    i += 1;
    let mut paren_depth = 1;
    while i < len && paren_depth > 0 {
        match bytes[i] {
            b'(' => paren_depth += 1,
            b')' => paren_depth -= 1,
            _ => {}
        }
        i += 1;
    }
    if paren_depth != 0 {
        return None;
    }
    // Check that the URL inside starts with http:// or https://.
    let url_bytes = &bytes[paren_start + 1..i - 1];
    let url_trimmed = trim_ascii(url_bytes);
    if starts_with_http(url_trimmed) {
        Some(i)
    } else {
        None
    }
}

fn trim_ascii(bytes: &[u8]) -> &[u8] {
    let start = bytes
        .iter()
        .position(|&b| b != b' ' && b != b'\t')
        .unwrap_or(bytes.len());
    let end = bytes
        .iter()
        .rposition(|&b| b != b' ' && b != b'\t')
        .map_or(start, |e| e + 1);
    &bytes[start..end]
}

fn starts_with_http(bytes: &[u8]) -> bool {
    if bytes.len() >= 8 && bytes[..8].eq_ignore_ascii_case(b"https://") {
        return true;
    }
    if bytes.len() >= 7 && bytes[..7].eq_ignore_ascii_case(b"http://") {
        return true;
    }
    false
}

/// Find all bare `http://`, `https://`, or `ftp://` URLs in the text.
/// A URL is a contiguous run of non-whitespace characters starting with the scheme.
fn find_bare_urls(text: &str) -> Vec<&str> {
    let mut results = Vec::new();
    let bytes = text.as_bytes();
    let len = bytes.len();
    let mut i = 0;

    while i < len {
        let remaining = &bytes[i..];
        let is_http = matches!(bytes[i], b'h' | b'H') && starts_with_http(remaining);
        let is_extra = !is_http && starts_with_extra_scheme(remaining);
        if is_http || is_extra {
            let start = i;
            while i < len && !bytes[i].is_ascii_whitespace() {
                i += 1;
            }
            let candidate = &text[start..i];
            let cleaned = strip_url_tail(candidate);
            // At minimum "ftp://x" (7 chars).
            if cleaned.len() > 7 {
                results.push(cleaned);
            }
        } else {
            i += 1;
        }
    }
    results
}

/// Check if the remaining bytes start with an extra scheme (e.g., `ftp://`).
fn starts_with_extra_scheme(bytes: &[u8]) -> bool {
    for scheme in EXTRA_SCHEMES {
        let sb = scheme.as_bytes();
        if bytes.len() >= sb.len() && bytes[..sb.len()].eq_ignore_ascii_case(sb) {
            return true;
        }
    }
    false
}

/// Strip trailing punctuation that is not part of the URL.
/// Handles balanced brackets: `(`, `)`, `[`, `]`, `{`, `}`, `<`, `>`.
/// Always strips trailing: `,`, `.`, `;`, `:`, `!`, `?`, `'`, `"`.
fn strip_url_tail(url: &str) -> &str {
    let bytes = url.as_bytes();
    let mut end = bytes.len();

    loop {
        if end == 0 {
            break;
        }
        match bytes[end - 1] {
            // Always-strip trailing punctuation.
            b',' | b'.' | b';' | b'!' | b'?' | b'\'' | b'"' => {
                end -= 1;
            }
            b':' => {
                end -= 1;
            }
            // Closing brackets: strip only when unbalanced within the URL.
            b')' => {
                if count_byte(&bytes[..end], b'(') < count_byte(&bytes[..end], b')') {
                    end -= 1;
                } else {
                    break;
                }
            }
            b']' => {
                if count_byte(&bytes[..end], b'[') < count_byte(&bytes[..end], b']') {
                    end -= 1;
                } else {
                    break;
                }
            }
            b'}' => {
                if count_byte(&bytes[..end], b'{') < count_byte(&bytes[..end], b'}') {
                    end -= 1;
                } else {
                    break;
                }
            }
            b'>' => {
                if count_byte(&bytes[..end], b'<') < count_byte(&bytes[..end], b'>') {
                    end -= 1;
                } else {
                    break;
                }
            }
            _ => break,
        }
    }
    &url[..end]
}

fn count_byte(bytes: &[u8], needle: u8) -> usize {
    bytes.iter().filter(|&&b| b == needle).count()
}

/// Check if a raw URL string is allowed (valid URL, recognized scheme, passes SSRF check).
fn is_allowed_url(raw: &str) -> bool {
    // Extra schemes (ftp://) are allowed without SSRF check — is_safe_url is http(s)-only.
    if starts_with_extra_scheme(raw.as_bytes()) {
        return true;
    }

    // Quick scheme check before attempting full parse.
    let has_http = raw.starts_with("http://")
        || raw.starts_with("https://")
        || raw.starts_with("HTTP://")
        || raw.starts_with("HTTPS://");
    if !has_http {
        let lower = raw.get(..8).map(str::to_ascii_lowercase);
        let lower7 = raw.get(..7).map(str::to_ascii_lowercase);
        let is_http_ci = matches!(lower.as_deref(), Some("https://"))
            || matches!(lower7.as_deref(), Some("http://"));
        if !is_http_ci {
            return false;
        }
    }
    security::is_safe_url(raw)
}

/// Extract links from a message, stripping markdown link syntax first.
///
/// Returns a deduplicated list of safe URLs, up to `config.max_links`.
pub fn extract_links(text: &str, config: &ExtractLinksConfig) -> Vec<String> {
    let trimmed = text.trim();
    if trimmed.is_empty() {
        return Vec::new();
    }

    let sanitized = strip_markdown_links(trimmed);
    let mut seen = HashSet::new();
    let mut results = Vec::new();

    for raw in find_bare_urls(&sanitized) {
        let raw = raw.trim();
        if raw.is_empty() {
            continue;
        }
        if !is_allowed_url(raw) {
            continue;
        }
        if !seen.insert(raw.to_string()) {
            continue;
        }
        results.push(raw.to_string());
        if results.len() >= config.max_links {
            break;
        }
    }

    results
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn empty_input() {
        let cfg = ExtractLinksConfig::default();
        assert!(extract_links("", &cfg).is_empty());
        assert!(extract_links("   ", &cfg).is_empty());
    }

    #[test]
    fn bare_urls() {
        let cfg = ExtractLinksConfig::default();
        let urls = extract_links("Check https://example.com and https://rust-lang.org", &cfg);
        assert_eq!(urls.len(), 2);
        assert_eq!(urls[0], "https://example.com");
        assert_eq!(urls[1], "https://rust-lang.org");
    }

    #[test]
    fn markdown_links_stripped() {
        let cfg = ExtractLinksConfig::default();
        let text = "See [Docs](https://docs.example.com) and https://bare.example.com";
        let urls = extract_links(text, &cfg);
        // Markdown link URL should NOT appear; only bare URL.
        assert_eq!(urls.len(), 1);
        assert_eq!(urls[0], "https://bare.example.com");
    }

    #[test]
    fn deduplication() {
        let cfg = ExtractLinksConfig::default();
        let text = "https://example.com https://example.com https://example.com";
        let urls = extract_links(text, &cfg);
        assert_eq!(urls.len(), 1);
    }

    #[test]
    fn max_links_limit() {
        let cfg = ExtractLinksConfig { max_links: 2 };
        let text = "https://a.com https://b.com https://c.com https://d.com";
        let urls = extract_links(text, &cfg);
        assert_eq!(urls.len(), 2);
    }

    #[test]
    fn ssrf_blocked() {
        let cfg = ExtractLinksConfig::default();
        let text = "https://example.com http://127.0.0.1/admin http://169.254.169.254/metadata";
        let urls = extract_links(text, &cfg);
        assert_eq!(urls.len(), 1);
        assert_eq!(urls[0], "https://example.com");
    }

    #[test]
    fn ftp_scheme_supported() {
        let cfg = ExtractLinksConfig::default();
        let text = "ftp://files.example.com ssh://server.example.com https://ok.example.com";
        let urls = extract_links(text, &cfg);
        assert_eq!(urls.len(), 2);
        assert_eq!(urls[0], "ftp://files.example.com");
        assert_eq!(urls[1], "https://ok.example.com");
    }

    #[test]
    fn trailing_punctuation_stripped() {
        let cfg = ExtractLinksConfig::default();
        // Trailing comma
        assert_eq!(
            find_bare_urls("https://example.com,")[0],
            "https://example.com"
        );
        // Trailing period (end of sentence)
        assert_eq!(
            find_bare_urls("Visit https://example.com.")[0],
            "https://example.com"
        );
        // Trailing semicolon
        assert_eq!(
            find_bare_urls("https://example.com;")[0],
            "https://example.com"
        );
        // Trailing exclamation
        assert_eq!(
            find_bare_urls("https://example.com!")[0],
            "https://example.com"
        );
        // Multiple trailing punctuation
        assert_eq!(
            find_bare_urls(r#"https://example.com"),"#)[0],
            "https://example.com"
        );
        // JSON array context (the reported bug)
        let urls = extract_links(
            r#"["https://github.com/choiceoh/deneb/releases/latest/download/latest.json"],"#,
            &cfg,
        );
        assert_eq!(urls.len(), 1);
        assert_eq!(
            urls[0],
            "https://github.com/choiceoh/deneb/releases/latest/download/latest.json"
        );
    }

    #[test]
    fn balanced_parens_preserved() {
        // Wikipedia-style URL with balanced parens should be kept intact.
        let urls = find_bare_urls("https://en.wikipedia.org/wiki/Rust_(programming_language)");
        assert_eq!(
            urls[0],
            "https://en.wikipedia.org/wiki/Rust_(programming_language)"
        );
    }

    #[test]
    fn unbalanced_closing_paren_stripped() {
        // URL wrapped in prose parens: "(see https://example.com)"
        let urls = find_bare_urls("(see https://example.com)");
        assert_eq!(urls[0], "https://example.com");
    }

    #[test]
    fn trailing_quotes_stripped() {
        let urls = find_bare_urls(r#""https://example.com""#);
        assert_eq!(urls[0], "https://example.com");
    }

    #[test]
    fn multibyte_text_with_markdown_links() {
        let cfg = ExtractLinksConfig::default();
        // Korean text with markdown link — must not corrupt multibyte chars.
        let text = "한국어 [링크](https://docs.example.com) 텍스트 https://bare.example.com 끝";
        let urls = extract_links(text, &cfg);
        assert_eq!(urls.len(), 1);
        assert_eq!(urls[0], "https://bare.example.com");

        // Verify strip_markdown_links preserves Korean correctly.
        let stripped = strip_markdown_links(text);
        assert!(stripped.contains("한국어"));
        assert!(stripped.contains("텍스트"));
        assert!(stripped.contains("끝"));
    }

    #[test]
    fn emoji_with_markdown_links() {
        let cfg = ExtractLinksConfig::default();
        let text = "🌍 [link](https://skip.com) 🚀 https://keep.com 🎉";
        let urls = extract_links(text, &cfg);
        assert_eq!(urls.len(), 1);
        assert_eq!(urls[0], "https://keep.com");
    }
}
