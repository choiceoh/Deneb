//! Media token extraction from LLM output text.
//!
//! Ports `src/media/parse.ts:splitMediaFromOutput`.
//! Extracts `MEDIA: <url/path>` tokens from text while respecting
//! fenced code blocks, and detects `[[audio_as_voice]]` tags.

use serde::Serialize;

/// Result of media token parsing.
#[derive(Debug, Serialize)]
pub struct MediaParseResult {
    pub text: String,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub media_urls: Vec<String>,
    /// First media URL for backward compatibility.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub media_url: Option<String>,
    #[serde(skip_serializing_if = "std::ops::Not::not")]
    pub audio_as_voice: bool,
}

/// Span of a fenced code block.
struct FenceSpan {
    start: usize,
    end: usize,
}

/// Parse fenced code block spans (``` or ~~~).
fn parse_fence_spans(text: &str) -> Vec<FenceSpan> {
    let mut spans = Vec::new();
    let lines: Vec<&str> = text.split('\n').collect();
    let mut in_fence = false;
    let mut fence_char = b'`';
    let mut fence_len = 0;
    let mut fence_start = 0;
    let mut offset = 0;

    for line in &lines {
        let trimmed = line.trim_start();
        if !in_fence {
            if let Some((ch, len)) = detect_fence_open(trimmed) {
                in_fence = true;
                fence_char = ch;
                fence_len = len;
                fence_start = offset;
            }
        } else if is_fence_close(trimmed, fence_char, fence_len) {
            spans.push(FenceSpan {
                start: fence_start,
                end: offset + line.len(),
            });
            in_fence = false;
        }
        offset += line.len() + 1; // +1 for newline
    }

    // If fence was never closed, extend to end.
    if in_fence {
        spans.push(FenceSpan {
            start: fence_start,
            end: text.len(),
        });
    }

    spans
}

fn detect_fence_open(trimmed: &str) -> Option<(u8, usize)> {
    let bytes = trimmed.as_bytes();
    if bytes.len() < 3 {
        return None;
    }
    let ch = bytes[0];
    if ch != b'`' && ch != b'~' {
        return None;
    }
    let mut count = 0;
    for &b in bytes {
        if b == ch {
            count += 1;
        } else {
            break;
        }
    }
    if count >= 3 {
        Some((ch, count))
    } else {
        None
    }
}

fn is_fence_close(trimmed: &str, fence_char: u8, fence_len: usize) -> bool {
    let bytes = trimmed.as_bytes();
    if bytes.len() < fence_len {
        return false;
    }
    let mut count = 0;
    for &b in bytes {
        if b == fence_char {
            count += 1;
        } else if b == b' ' || b == b'\t' {
            // trailing whitespace is OK
            break;
        } else {
            return false;
        }
    }
    count >= fence_len
}

fn is_inside_fence(spans: &[FenceSpan], offset: usize) -> bool {
    spans.iter().any(|s| offset >= s.start && offset < s.end)
}

/// Check if candidate looks like a valid media source (URL or local path).
fn is_valid_media(candidate: &str) -> bool {
    if candidate.is_empty() || candidate.len() > 4096 {
        return false;
    }
    // No whitespace allowed in simple mode.
    if candidate.chars().any(|c| c.is_whitespace()) {
        return false;
    }
    // HTTP(S) URL.
    if candidate.starts_with("http://") || candidate.starts_with("https://") {
        return true;
    }
    // Local path patterns.
    if candidate.starts_with('/')
        || candidate.starts_with("./")
        || candidate.starts_with("../")
        || candidate.starts_with('~')
    {
        return true;
    }
    // Windows drive letter.
    let bytes = candidate.as_bytes();
    if bytes.len() >= 3
        && bytes[0].is_ascii_alphabetic()
        && bytes[1] == b':'
        && (bytes[2] == b'\\' || bytes[2] == b'/')
    {
        return true;
    }
    // UNC path.
    if candidate.starts_with("\\\\") {
        return true;
    }
    false
}

/// Clean wrapping quotes/brackets from a candidate.
fn clean_candidate(raw: &str) -> &str {
    let mut s = raw;
    // Strip leading punctuation.
    while !s.is_empty() {
        let first = s.as_bytes()[0];
        if matches!(first, b'`' | b'"' | b'\'' | b'[' | b'{' | b'(') {
            s = &s[1..];
        } else {
            break;
        }
    }
    // Strip trailing punctuation.
    while !s.is_empty() {
        let last = s.as_bytes()[s.len() - 1];
        if matches!(
            last,
            b'`' | b'"' | b'\'' | b'\\' | b'}' | b')' | b']' | b','
        ) {
            s = &s[..s.len() - 1];
        } else {
            break;
        }
    }
    s
}

/// Normalize `file://` prefix.
fn normalize_media_source(src: &str) -> String {
    if let Some(rest) = src.strip_prefix("file://") {
        rest.to_string()
    } else {
        src.to_string()
    }
}

/// Strip `[[audio_as_voice]]` tag from text.
fn strip_audio_tag(text: &str) -> (String, bool) {
    let tag = "[[audio_as_voice]]";
    if let Some(idx) = text.find(tag) {
        let mut result = String::with_capacity(text.len());
        result.push_str(&text[..idx]);
        result.push_str(&text[idx + tag.len()..]);
        (result, true)
    } else {
        (text.to_string(), false)
    }
}

/// Extract `MEDIA:` tokens from text output.
///
/// Returns cleaned text with MEDIA lines removed, extracted media URLs,
/// and whether `[[audio_as_voice]]` was present.
pub fn split_media_from_output(raw: &str) -> MediaParseResult {
    let trimmed_raw = raw.trim_end();
    if trimmed_raw.trim().is_empty() {
        return MediaParseResult {
            text: String::new(),
            media_urls: Vec::new(),
            media_url: None,
            audio_as_voice: false,
        };
    }

    let has_media_token = trimmed_raw
        .as_bytes()
        .windows(6)
        .any(|w| w.eq_ignore_ascii_case(b"media:"));
    let has_audio_tag = trimmed_raw.contains("[[");

    if !has_media_token && !has_audio_tag {
        return MediaParseResult {
            text: trimmed_raw.to_string(),
            media_urls: Vec::new(),
            media_url: None,
            audio_as_voice: false,
        };
    }

    let has_fence_markers = trimmed_raw.contains("```") || trimmed_raw.contains("~~~");
    let fence_spans = if has_fence_markers {
        parse_fence_spans(trimmed_raw)
    } else {
        Vec::new()
    };

    let mut media: Vec<String> = Vec::new();
    let mut found_media_token = false;
    let lines: Vec<&str> = trimmed_raw.split('\n').collect();
    let mut kept_lines: Vec<String> = Vec::new();
    let mut line_offset = 0;

    for line in &lines {
        // Skip MEDIA extraction inside fenced code blocks.
        if has_fence_markers && is_inside_fence(&fence_spans, line_offset) {
            kept_lines.push(line.to_string());
            line_offset += line.len() + 1;
            continue;
        }

        let trimmed_start = line.trim_start();
        if !trimmed_start.starts_with("MEDIA:") {
            kept_lines.push(line.to_string());
            line_offset += line.len() + 1;
            continue;
        }

        // Extract the payload after "MEDIA:".
        let media_idx = trimmed_start.find("MEDIA:").unwrap();
        let payload = &trimmed_start[media_idx + 6..];
        let payload = payload.trim();
        // Strip optional wrapping backtick.
        let payload = payload.strip_prefix('`').unwrap_or(payload);
        let payload = payload.strip_suffix('`').unwrap_or(payload);
        let payload = payload.trim();

        if payload.is_empty() {
            kept_lines.push(line.to_string());
            line_offset += line.len() + 1;
            continue;
        }

        // Try each space-separated part.
        let mut any_valid = false;
        for part in payload.split_whitespace() {
            let candidate = clean_candidate(part);
            let normalized = normalize_media_source(candidate);
            if is_valid_media(&normalized) {
                media.push(normalized);
                any_valid = true;
                found_media_token = true;
            }
        }

        // Fallback: try the entire payload as a single path (may contain spaces).
        if !any_valid {
            let candidate = clean_candidate(payload);
            let normalized = normalize_media_source(candidate);
            if is_valid_media_allow_spaces(&normalized) {
                media.push(normalized);
                found_media_token = true;
                any_valid = true;
            }
        }

        if !any_valid {
            // Check if it looks like a local path — strip even if invalid.
            let candidate = clean_candidate(payload);
            if is_likely_local_path(candidate) {
                found_media_token = true;
                // Drop the line (don't keep it).
            } else {
                kept_lines.push(line.to_string());
            }
        }
        // If valid media found, the MEDIA line is stripped (not added to kept_lines).

        line_offset += line.len() + 1;
    }

    let mut cleaned_text = kept_lines.join("\n");
    // Collapse multiple spaces and blank lines.
    cleaned_text = collapse_whitespace(&cleaned_text);

    // Strip [[audio_as_voice]] tag.
    let (cleaned_text, audio_as_voice) = strip_audio_tag(&cleaned_text);
    let cleaned_text = if audio_as_voice {
        collapse_whitespace(&cleaned_text)
    } else {
        cleaned_text
    };
    let cleaned_text = cleaned_text.trim().to_string();

    if media.is_empty() {
        return MediaParseResult {
            text: if found_media_token || audio_as_voice {
                cleaned_text
            } else {
                trimmed_raw.to_string()
            },
            media_urls: Vec::new(),
            media_url: None,
            audio_as_voice,
        };
    }

    let media_url = media.first().cloned();
    MediaParseResult {
        text: cleaned_text,
        media_urls: media,
        media_url,
        audio_as_voice,
    }
}

fn is_valid_media_allow_spaces(candidate: &str) -> bool {
    if candidate.is_empty() || candidate.len() > 4096 {
        return false;
    }
    if candidate.starts_with("http://") || candidate.starts_with("https://") {
        return true;
    }
    if candidate.starts_with('/')
        || candidate.starts_with("./")
        || candidate.starts_with("../")
        || candidate.starts_with('~')
        || candidate.starts_with("\\\\")
    {
        return true;
    }
    let bytes = candidate.as_bytes();
    if bytes.len() >= 3
        && bytes[0].is_ascii_alphabetic()
        && bytes[1] == b':'
        && (bytes[2] == b'\\' || bytes[2] == b'/')
    {
        return true;
    }
    false
}

fn is_likely_local_path(candidate: &str) -> bool {
    candidate.starts_with('/')
        || candidate.starts_with("./")
        || candidate.starts_with("../")
        || candidate.starts_with('~')
        || candidate.starts_with("file://")
        || candidate.starts_with("\\\\")
}

fn collapse_whitespace(input: &str) -> String {
    let mut result = String::with_capacity(input.len());

    // Collapse trailing whitespace on lines.
    for line in input.split('\n') {
        let trimmed_end = line.trim_end();
        result.push_str(trimmed_end);
        result.push('\n');
    }
    // Remove trailing newline added by loop.
    if result.ends_with('\n') {
        result.pop();
    }

    // Collapse multiple consecutive blank lines.
    let mut final_result = String::with_capacity(result.len());
    let mut newline_count = 0;
    for ch in result.chars() {
        if ch == '\n' {
            newline_count += 1;
            if newline_count <= 1 {
                final_result.push('\n');
            }
        } else {
            newline_count = 0;
            final_result.push(ch);
        }
    }

    // Collapse multiple spaces.
    let mut collapsed = String::with_capacity(final_result.len());
    let mut prev_space = false;
    for ch in final_result.chars() {
        if ch == ' ' || ch == '\t' {
            if !prev_space {
                collapsed.push(' ');
            }
            prev_space = true;
        } else {
            prev_space = false;
            collapsed.push(ch);
        }
    }

    collapsed
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn empty_input() {
        let result = split_media_from_output("");
        assert_eq!(result.text, "");
        assert!(result.media_urls.is_empty());
    }

    #[test]
    fn no_media_tokens() {
        let result = split_media_from_output("Hello world, no media here.");
        assert_eq!(result.text, "Hello world, no media here.");
        assert!(result.media_urls.is_empty());
    }

    #[test]
    fn single_url_token() {
        let result =
            split_media_from_output("Here is an image\nMEDIA: https://example.com/image.png");
        assert_eq!(result.media_urls, vec!["https://example.com/image.png"]);
        assert!(result.media_url.is_some());
        assert!(result.text.contains("Here is an image"));
        assert!(!result.text.contains("MEDIA:"));
    }

    #[test]
    fn local_path_token() {
        let result = split_media_from_output("MEDIA: /tmp/output.wav\nDone.");
        assert_eq!(result.media_urls, vec!["/tmp/output.wav"]);
        assert!(result.text.contains("Done."));
    }

    #[test]
    fn file_protocol_normalized() {
        let result = split_media_from_output("MEDIA: file:///tmp/audio.mp3");
        assert_eq!(result.media_urls, vec!["/tmp/audio.mp3"]);
    }

    #[test]
    fn inside_fence_ignored() {
        let input = "text\n```\nMEDIA: https://example.com/skip.png\n```\nMEDIA: https://example.com/keep.png";
        let result = split_media_from_output(input);
        assert_eq!(result.media_urls.len(), 1);
        assert_eq!(result.media_urls[0], "https://example.com/keep.png");
        // The fenced MEDIA line should be kept as text.
        assert!(result.text.contains("MEDIA: https://example.com/skip.png"));
    }

    #[test]
    fn audio_as_voice_tag() {
        let result = split_media_from_output("Hello [[audio_as_voice]]\nMEDIA: /tmp/voice.wav");
        assert!(result.audio_as_voice);
        assert!(!result.text.contains("[[audio_as_voice]]"));
    }

    #[test]
    fn multiple_media_urls() {
        let input = "MEDIA: https://a.com/1.png\ntext\nMEDIA: https://b.com/2.png";
        let result = split_media_from_output(input);
        assert_eq!(result.media_urls.len(), 2);
    }

    #[test]
    fn backtick_wrapped() {
        let result = split_media_from_output("MEDIA: `https://example.com/img.png`");
        assert_eq!(result.media_urls, vec!["https://example.com/img.png"]);
    }
}
