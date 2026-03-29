//! Spoiler syntax preprocessing for the markdown-to-IR parser.
//!
//! Converts `||hidden text||` into zero-width-character sentinel markers before
//! handing the input to pulldown-cmark, which would otherwise misinterpret the
//! pipe characters as table syntax.

use super::render_state::RenderState;
use super::spans::MarkdownStyle;

/// Zero-width sentinel inserted in place of the opening `||`.
pub(crate) const SPOILER_OPEN: &str = "\u{200B}\u{FEFF}SPOILER_OPEN\u{200B}";
/// Zero-width sentinel inserted in place of the closing `||`.
pub(crate) const SPOILER_CLOSE: &str = "\u{200B}\u{FEFF}SPOILER_CLOSE\u{200B}";

/// Preprocess markdown text to convert `||text||` into placeholder markers
/// that pulldown-cmark won't strip. We use zero-width chars as sentinels.
pub(crate) fn preprocess_spoilers(text: &str) -> String {
    // Count || delimiters
    let mut total_delims = 0;
    let mut i = 0;
    let bytes = text.as_bytes();
    while i < bytes.len() {
        if i + 1 < bytes.len() && bytes[i] == b'|' && bytes[i + 1] == b'|' {
            total_delims += 1;
            i += 2;
        } else {
            i += 1;
        }
    }

    if total_delims < 2 {
        return text.to_string();
    }
    let usable_delims = total_delims - (total_delims % 2);

    let mut result = String::with_capacity(text.len() + 64);
    let mut consumed = 0;
    let mut spoiler_open = false;
    let mut idx = 0;

    while idx < bytes.len() {
        if idx + 1 < bytes.len() && bytes[idx] == b'|' && bytes[idx + 1] == b'|' {
            if consumed >= usable_delims {
                result.push_str("||");
                idx += 2;
                continue;
            }
            consumed += 1;
            spoiler_open = !spoiler_open;
            result.push_str(if spoiler_open {
                SPOILER_OPEN
            } else {
                SPOILER_CLOSE
            });
            idx += 2;
        } else {
            // Push the char (handle multi-byte UTF-8)
            // Safety: idx is always at a valid UTF-8 boundary (advanced by
            // len_utf8()), but we guard defensively in case of unexpected state.
            let Some(ch) = text[idx..].chars().next() else {
                break;
            };
            result.push(ch);
            idx += ch.len_utf8();
        }
    }

    result
}

/// Handle text that contains spoiler sentinel markers.
pub(crate) fn handle_spoiler_text(state: &mut RenderState, text: &str) {
    let mut remaining = text;
    while !remaining.is_empty() {
        if let Some(pos) = remaining.find(SPOILER_OPEN) {
            if pos > 0 {
                state.append_text(&remaining[..pos]);
            }
            state.open_style(MarkdownStyle::Spoiler);
            remaining = &remaining[pos + SPOILER_OPEN.len()..];
        } else if let Some(pos) = remaining.find(SPOILER_CLOSE) {
            if pos > 0 {
                state.append_text(&remaining[..pos]);
            }
            state.close_style(MarkdownStyle::Spoiler);
            remaining = &remaining[pos + SPOILER_CLOSE.len()..];
        } else {
            state.append_text(remaining);
            break;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::markdown::render_state::{HeadingStyle, RenderState, TableMode};
    use crate::markdown::spans::MarkdownStyle;

    // --- preprocess_spoilers ---

    #[test]
    fn passthrough_no_delimiters() {
        assert_eq!(preprocess_spoilers("hello world"), "hello world");
    }

    #[test]
    fn passthrough_single_delimiter() {
        // Only one || — not a matched pair, passthrough.
        assert_eq!(preprocess_spoilers("hello || world"), "hello || world");
    }

    #[test]
    fn empty_string_passthrough() {
        assert_eq!(preprocess_spoilers(""), "");
    }

    #[test]
    fn basic_spoiler_pair() {
        let result = preprocess_spoilers("||secret||");
        assert!(result.contains(SPOILER_OPEN));
        assert!(result.contains(SPOILER_CLOSE));
        assert!(result.contains("secret"));
        assert!(!result.contains("||"));
    }

    #[test]
    fn spoiler_with_surrounding_text() {
        let result = preprocess_spoilers("before ||hidden|| after");
        assert!(result.starts_with("before "));
        assert!(result.ends_with(" after"));
        assert!(result.contains(SPOILER_OPEN));
        assert!(result.contains("hidden"));
        assert!(result.contains(SPOILER_CLOSE));
    }

    #[test]
    fn two_spoiler_pairs() {
        let result = preprocess_spoilers("||a|| and ||b||");
        let open_count = result.matches(SPOILER_OPEN).count();
        let close_count = result.matches(SPOILER_CLOSE).count();
        assert_eq!(open_count, 2);
        assert_eq!(close_count, 2);
    }

    #[test]
    fn odd_delimiters_last_one_preserved() {
        // Three || → only 2 (even) converted, last left as ||.
        let result = preprocess_spoilers("||a|| ||b");
        // Two converted pairs, trailing || left as-is (third delimiter is unmatched).
        let open_count = result.matches(SPOILER_OPEN).count();
        let close_count = result.matches(SPOILER_CLOSE).count();
        assert_eq!(open_count, 1);
        assert_eq!(close_count, 1);
        // The leftover || from "||b" is still present as a raw pipe pair.
        assert!(result.contains("||b") || result.contains("||"));
    }

    #[test]
    fn unicode_content_preserved() {
        let result = preprocess_spoilers("||안녕하세요||");
        assert!(result.contains("안녕하세요"));
        assert!(result.contains(SPOILER_OPEN));
        assert!(result.contains(SPOILER_CLOSE));
    }

    // --- handle_spoiler_text ---

    fn make_state() -> RenderState {
        RenderState::new(HeadingStyle::None, String::new(), TableMode::Off)
    }

    #[test]
    fn handle_spoiler_applies_style() {
        let mut state = make_state();
        let input = format!("{}secret{}", SPOILER_OPEN, SPOILER_CLOSE);
        handle_spoiler_text(&mut state, &input);
        assert_eq!(state.text, "secret");
        assert_eq!(state.styles.len(), 1);
        assert_eq!(state.styles[0].style, MarkdownStyle::Spoiler);
        assert_eq!(state.styles[0].start, 0);
        assert_eq!(state.styles[0].end, 6);
    }

    #[test]
    fn handle_spoiler_mixed_content() {
        let mut state = make_state();
        let input = format!("hello {}world{} bye", SPOILER_OPEN, SPOILER_CLOSE);
        handle_spoiler_text(&mut state, &input);
        assert!(state.text.contains("hello"));
        assert!(state.text.contains("world"));
        assert!(state.text.contains("bye"));
        let spoiler_spans: Vec<_> = state
            .styles
            .iter()
            .filter(|s| s.style == MarkdownStyle::Spoiler)
            .collect();
        assert_eq!(spoiler_spans.len(), 1);
    }

    #[test]
    fn handle_plain_text_no_styles() {
        let mut state = make_state();
        handle_spoiler_text(&mut state, "just plain text");
        assert_eq!(state.text, "just plain text");
        assert!(state.styles.is_empty());
    }
}
