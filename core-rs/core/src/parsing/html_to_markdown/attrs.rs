//! HTML attribute extraction and tag-boundary helpers.

/// Extract an attribute value from a tag string like `<a href="value">`.
pub(crate) fn extract_attr(tag: &str, attr: &str) -> Option<String> {
    let lower = tag.to_ascii_lowercase();
    let pattern = format!("{attr}=");
    let idx = lower.find(&pattern)?;
    let after_eq = idx + pattern.len();
    let bytes = tag.as_bytes();
    if after_eq >= bytes.len() {
        return None;
    }
    let quote = bytes[after_eq];
    if quote == b'"' || quote == b'\'' {
        let start = after_eq + 1;
        let end = tag.get(start..)?.find(quote as char).map(|e| start + e)?;
        Some(tag.get(start..end)?.to_string())
    } else {
        // Unquoted attribute: read until whitespace or >.
        let start = after_eq;
        let rest = tag.get(start..)?;
        let end = rest
            .find(|c: char| c.is_ascii_whitespace() || c == '>')
            .map_or(tag.len(), |e| start + e);
        Some(tag.get(start..end)?.to_string())
    }
}

/// Extract language from `<code class="language-X">` or `class="lang-X"`.
pub(crate) fn extract_code_language(tag: &str) -> String {
    let Some(class) = extract_attr(tag, "class") else {
        return String::new();
    };
    for prefix in &["language-", "lang-"] {
        if let Some(rest) = class.strip_prefix(prefix) {
            let lang = rest
                .split(|c: char| c.is_ascii_whitespace())
                .next()
                .unwrap_or("");
            if !lang.is_empty() {
                return lang.to_string();
            }
        }
    }
    String::new()
}

/// Extract a filename from a URL path for use as an image label.
pub(crate) fn filename_from_url(url: &str) -> String {
    url.rsplit('/')
        .next()
        .and_then(|s| s.split('?').next())
        .filter(|s| !s.is_empty())
        .unwrap_or("image")
        .to_string()
}
