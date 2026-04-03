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

/// Parse fenced code block spans (`` ``` `` or `~~~`).
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
    if candidate.chars().any(char::is_whitespace) {
        return false;
    }
    is_valid_media_core(candidate) || is_bare_filename(candidate)
}

/// Core path/URL validation without whitespace restriction.
fn is_valid_media_core(candidate: &str) -> bool {
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

/// Check if a candidate is a bare filename with a file extension (1-10 chars).
/// E.g., "image.png", "recording.m4a"
fn is_bare_filename(candidate: &str) -> bool {
    if candidate.is_empty() || candidate.len() > 260 {
        return false;
    }
    if let Some(dot_pos) = candidate.rfind('.') {
        let ext_len = candidate.len() - dot_pos - 1;
        if (1..=10).contains(&ext_len) {
            let name = &candidate[..dot_pos];
            // Name must be non-empty and contain no path separators
            return !name.is_empty() && !name.contains('/') && !name.contains('\\');
        }
    }
    false
}

/// Try to extract a quoted string payload from the text.
/// Supports double quotes, single quotes.
/// Returns the unquoted content if found.
fn try_unwrap_quoted(payload: &str) -> Option<&str> {
    let trimmed = payload.trim();
    if trimmed.len() < 2 {
        return None;
    }
    let first = trimmed.as_bytes()[0];
    let last = trimmed.as_bytes()[trimmed.len() - 1];
    if (first == b'"' && last == b'"') || (first == b'\'' && last == b'\'') {
        Some(&trimmed[1..trimmed.len() - 1])
    } else {
        None
    }
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

/// A parsed inline directive like `[[key]]` or `[[key=value]]`.
#[derive(Debug)]
struct InlineDirective {
    key: String,
}

/// Parse and strip all `[[...]]` inline directives from text.
/// Returns the cleaned text and a list of parsed directives.
fn strip_inline_directives(text: &str) -> (String, Vec<InlineDirective>) {
    let mut directives = Vec::new();
    let mut result = String::with_capacity(text.len());
    let mut rest = text;

    while let Some(start) = rest.find("[[") {
        if let Some(end) = rest[start + 2..].find("]]") {
            let inner = &rest[start + 2..start + 2 + end];
            // Parse key=value or just key
            let directive = if let Some(eq_pos) = inner.find('=') {
                InlineDirective {
                    key: inner[..eq_pos].trim().to_string(),
                }
            } else {
                InlineDirective {
                    key: inner.trim().to_string(),
                }
            };
            result.push_str(&rest[..start]);
            directives.push(directive);
            rest = &rest[start + 2 + end + 2..];
        } else {
            // No closing ]] found — keep the rest as-is
            result.push_str(rest);
            rest = "";
            break;
        }
    }
    result.push_str(rest);

    (result, directives)
}

/// Strip directives and detect `audio_as_voice`.
fn strip_audio_tag(text: &str) -> (String, bool) {
    let (cleaned, directives) = strip_inline_directives(text);
    let audio_as_voice = directives
        .iter()
        .any(|d| d.key == "audio_as_voice" || d.key == "voice");
    (cleaned, audio_as_voice)
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
        let Some(media_idx) = trimmed_start.find("MEDIA:") else {
            // Verified by starts_with above; unreachable in practice.
            kept_lines.push(line.to_string());
            line_offset += line.len() + 1;
            continue;
        };
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

        // Stage 1: Try unwrapping quoted payload (e.g., MEDIA: "path with spaces.mp3").
        let mut any_valid = false;
        if let Some(unquoted) = try_unwrap_quoted(payload) {
            let candidate = clean_candidate(unquoted);
            let normalized = normalize_media_source(candidate);
            if is_valid_media_allow_spaces(&normalized) || is_bare_filename(&normalized) {
                media.push(normalized);
                any_valid = true;
                found_media_token = true;
            }
        }

        // Stage 2: Try each space-separated part.
        if !any_valid {
            for part in payload.split_whitespace() {
                let candidate = clean_candidate(part);
                let normalized = normalize_media_source(candidate);
                if is_valid_media(&normalized) {
                    media.push(normalized);
                    any_valid = true;
                    found_media_token = true;
                }
            }
        }

        // Stage 3: Fallback — try entire payload as a single path (may contain spaces).
        if !any_valid {
            let candidate = clean_candidate(payload);
            let normalized = normalize_media_source(candidate);
            if is_valid_media_allow_spaces(&normalized) || is_bare_filename(&normalized) {
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
    is_valid_media_core(candidate)
}

fn is_likely_local_path(candidate: &str) -> bool {
    candidate.starts_with('/')
        || candidate.starts_with("./")
        || candidate.starts_with("../")
        || candidate.starts_with('~')
        || candidate.starts_with("file://")
        || candidate.starts_with("\\\\")
}

/// Single-pass whitespace normalization:
/// - Trim trailing whitespace on each line
/// - Collapse consecutive blank lines into one newline
/// - Collapse consecutive spaces/tabs into a single space
fn collapse_whitespace(input: &str) -> String {
    let mut out = String::with_capacity(input.len());
    // trailing_ws tracks spaces/tabs that may be trailing (not yet flushed).
    let mut trailing_ws: usize = 0;
    let mut newline_count: u32 = 0;
    let mut prev_space = false;

    for ch in input.chars() {
        match ch {
            '\n' => {
                // Drop any trailing whitespace before the newline.
                trailing_ws = 0;
                prev_space = false;
                newline_count += 1;
                if newline_count <= 1 {
                    out.push('\n');
                }
            }
            ' ' | '\t' => {
                newline_count = 0;
                // Buffer this as potential trailing whitespace.
                if !prev_space {
                    trailing_ws += 1;
                }
                prev_space = true;
            }
            _ => {
                // Flush buffered whitespace as a single space.
                if trailing_ws > 0 || prev_space {
                    out.push(' ');
                    trailing_ws = 0;
                }
                prev_space = false;
                newline_count = 0;
                out.push(ch);
            }
        }
    }
    out
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

    #[test]
    fn quoted_path_with_spaces() {
        let result = split_media_from_output(r#"MEDIA: "/tmp/my file with spaces.mp3""#);
        assert_eq!(result.media_urls, vec!["/tmp/my file with spaces.mp3"]);
    }

    #[test]
    fn bare_filename() {
        let result = split_media_from_output("MEDIA: image.png");
        assert_eq!(result.media_urls, vec!["image.png"]);
    }

    #[test]
    fn bare_filename_with_extension() {
        let result = split_media_from_output("MEDIA: recording.m4a");
        assert_eq!(result.media_urls, vec!["recording.m4a"]);
    }

    #[test]
    fn directive_key_value() {
        let result = split_media_from_output(
            "Hello [[audio_as_voice]] [[format=wav]]\nMEDIA: /tmp/voice.wav",
        );
        assert!(result.audio_as_voice);
        assert!(!result.text.contains("[["));
    }

    #[test]
    fn directive_unclosed_bracket() {
        let result = split_media_from_output("Hello [[ not closed");
        assert_eq!(result.text, "Hello [[ not closed");
        assert!(!result.audio_as_voice);
    }

    #[test]
    fn voice_tag_alias() {
        let result = split_media_from_output("Hello [[voice]]\nMEDIA: /tmp/voice.wav");
        assert!(result.audio_as_voice);
        assert!(!result.text.contains("[[voice]]"));
    }
}
