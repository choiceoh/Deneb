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
        // Safe: we only branch on ASCII bytes; non-ASCII is passed through.
        out.push(bytes[i] as char);
        i += 1;
    }
    out
}

/// Try to match `[...](https?://...)` starting at `start`.
/// Returns the index past the closing `)` on success.
fn match_markdown_link(bytes: &[u8], start: usize) -> Option<usize> {
    let len = bytes.len();
    // Find closing `]`.
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

/// Find all bare `http://` or `https://` URLs in the text.
/// A URL is a contiguous run of non-whitespace characters starting with the scheme.
fn find_bare_urls(text: &str) -> Vec<&str> {
    let mut results = Vec::new();
    let bytes = text.as_bytes();
    let len = bytes.len();
    let mut i = 0;

    while i < len {
        // Look for 'h' or 'H' as a quick filter.
        if !matches!(bytes[i], b'h' | b'H') {
            i += 1;
            continue;
        }
        let remaining = &bytes[i..];
        if starts_with_http(remaining) {
            // Find end of URL (next whitespace).
            let start = i;
            while i < len && !bytes[i].is_ascii_whitespace() {
                i += 1;
            }
            // Safety: the original text is valid UTF-8 and we split on ASCII boundaries.
            results.push(&text[start..i]);
        } else {
            i += 1;
        }
    }
    results
}

/// Check if a raw URL string is allowed (valid URL, http/https scheme, passes SSRF check).
fn is_allowed_url(raw: &str) -> bool {
    // Quick scheme check before attempting full parse.
    if !raw.starts_with("http://")
        && !raw.starts_with("https://")
        && !raw.starts_with("HTTP://")
        && !raw.starts_with("HTTPS://")
    {
        // Case-insensitive prefix check.
        let lower = raw.get(..8).map(|s| s.to_ascii_lowercase());
        if !matches!(lower.as_deref(), Some("https://") | Some("http://\x00")) {
            let lower7 = raw.get(..7).map(|s| s.to_ascii_lowercase());
            if !matches!(lower7.as_deref(), Some("http://")) {
                return false;
            }
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
    fn no_non_http_schemes() {
        let cfg = ExtractLinksConfig::default();
        let text = "ftp://files.example.com ssh://server.example.com https://ok.example.com";
        let urls = extract_links(text, &cfg);
        assert_eq!(urls.len(), 1);
        assert_eq!(urls[0], "https://ok.example.com");
    }
}
