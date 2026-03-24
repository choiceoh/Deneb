//! Fence span parsing — detects fenced code blocks (``` or ~~~) in raw
//! markdown text.
//!
//! Mirrors `parseFenceSpans`, `findFenceSpanAt`, `isSafeFenceBreak`
//! from `src/markdown/fences.ts`.

use serde::{Deserialize, Serialize};

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct FenceSpan {
    pub start: usize,
    pub end: usize,
    #[serde(rename = "openLine")]
    pub open_line: String,
    pub marker: String,
    pub indent: String,
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

/// Parse fenced code block spans from raw markdown text.
///
/// A fence opens with 3+ backticks or tildes (with up to 3 spaces indent)
/// and closes with a matching or longer run of the same character.
/// Unclosed fences extend to end-of-buffer.
pub fn parse_fence_spans(buffer: &str) -> Vec<FenceSpan> {
    let mut spans = Vec::new();
    let bytes = buffer.as_bytes();
    let len = bytes.len();

    struct OpenFence {
        start: usize,
        marker_char: u8,
        marker_len: usize,
        open_line: String,
        marker: String,
        indent: String,
    }

    let mut open: Option<OpenFence> = None;
    let mut offset: usize = 0;

    while offset <= len {
        // Find end of current line
        let line_end = memchr::memchr(b'\n', &bytes[offset..])
            .map(|pos| offset + pos)
            .unwrap_or(len);

        let line = &buffer[offset..line_end];

        // Match fence pattern: /^( {0,3})(`{3,}|~{3,})(.*)$/
        if let Some((indent, marker, _rest)) = match_fence_line(line) {
            let marker_char = marker.as_bytes()[0];
            let marker_len = marker.len();

            if let Some(ref o) = open {
                // Check if this closes the open fence
                if o.marker_char == marker_char && marker_len >= o.marker_len {
                    spans.push(FenceSpan {
                        start: o.start,
                        end: line_end,
                        open_line: o.open_line.clone(),
                        marker: o.marker.clone(),
                        indent: o.indent.clone(),
                    });
                    open = None;
                }
            } else {
                open = Some(OpenFence {
                    start: offset,
                    marker_char,
                    marker_len,
                    open_line: line.to_string(),
                    marker: marker.to_string(),
                    indent: indent.to_string(),
                });
            }
        }

        if line_end >= len {
            break;
        }
        offset = line_end + 1;
    }

    // Unclosed fence extends to end of buffer
    if let Some(o) = open {
        spans.push(FenceSpan {
            start: o.start,
            end: len,
            open_line: o.open_line,
            marker: o.marker,
            indent: o.indent,
        });
    }

    spans
}

/// Match a fence line: `^( {0,3})(`{3,}|~{3,})(.*)$`
/// Returns (indent, marker, rest) or None.
fn match_fence_line(line: &str) -> Option<(&str, &str, &str)> {
    let bytes = line.as_bytes();
    let len = bytes.len();

    // Count leading spaces (0-3)
    let mut indent_len = 0;
    while indent_len < len && indent_len < 3 && bytes[indent_len] == b' ' {
        indent_len += 1;
    }
    // Also allow exactly 3 spaces
    if indent_len < len && indent_len == 3 && bytes[indent_len] == b' ' {
        // Already at max indent, but the regex allows {0,3} so 3 spaces is the max.
        // Actually we checked < 3, so indent_len is at most 3 already. Let me re-check.
        // indent_len goes up while < 3, so max is 3. Good.
    }

    if indent_len >= len {
        return None;
    }

    let marker_char = bytes[indent_len];
    if marker_char != b'`' && marker_char != b'~' {
        return None;
    }

    let mut marker_end = indent_len;
    while marker_end < len && bytes[marker_end] == marker_char {
        marker_end += 1;
    }

    let marker_len = marker_end - indent_len;
    if marker_len < 3 {
        return None;
    }

    Some((&line[..indent_len], &line[indent_len..marker_end], &line[marker_end..]))
}

// ---------------------------------------------------------------------------
// Lookup
// ---------------------------------------------------------------------------

/// Binary search for a fence span containing `index`.
/// Returns the span if `index` is strictly inside it (start < index < end).
pub fn find_fence_span_at(spans: &[FenceSpan], index: usize) -> Option<&FenceSpan> {
    let mut low: usize = 0;
    let mut high = spans.len();

    while low < high {
        let mid = low + (high - low) / 2;
        let span = &spans[mid];
        if index <= span.start {
            high = mid;
        } else if index >= span.end {
            low = mid + 1;
        } else {
            return Some(span);
        }
    }
    None
}

/// Check if splitting at `index` would not break a fenced code block.
pub fn is_safe_fence_break(spans: &[FenceSpan], index: usize) -> bool {
    find_fence_span_at(spans, index).is_none()
}

// ---------------------------------------------------------------------------
// napi exports
// ---------------------------------------------------------------------------

#[cfg(feature = "napi_binding")]
pub mod napi_exports {
    use super::*;
    use napi_derive::napi;

    #[napi(object)]
    pub struct JsFenceSpan {
        pub start: u32,
        pub end: u32,
        #[napi(js_name = "openLine")]
        pub open_line: String,
        pub marker: String,
        pub indent: String,
    }

    fn to_js(spans: Vec<FenceSpan>) -> Vec<JsFenceSpan> {
        spans
            .into_iter()
            .map(|s| JsFenceSpan {
                start: s.start as u32,
                end: s.end as u32,
                open_line: s.open_line,
                marker: s.marker,
                indent: s.indent,
            })
            .collect()
    }

    fn from_js(spans: &[JsFenceSpan]) -> Vec<FenceSpan> {
        spans
            .iter()
            .map(|s| FenceSpan {
                start: s.start as usize,
                end: s.end as usize,
                open_line: s.open_line.clone(),
                marker: s.marker.clone(),
                indent: s.indent.clone(),
            })
            .collect()
    }

    #[napi]
    pub fn markdown_parse_fence_spans(buffer: String) -> Vec<JsFenceSpan> {
        to_js(parse_fence_spans(&buffer))
    }

    #[napi]
    pub fn markdown_is_safe_fence_break(spans: Vec<JsFenceSpan>, index: u32) -> bool {
        let internal = from_js(&spans);
        is_safe_fence_break(&internal, index as usize)
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_simple_fence() {
        let input = "hello\n```js\ncode\n```\nend";
        let spans = parse_fence_spans(input);
        assert_eq!(spans.len(), 1);
        assert_eq!(spans[0].open_line, "```js");
        assert_eq!(spans[0].marker, "```");
    }

    #[test]
    fn parse_tilde_fence() {
        let input = "~~~\ncode\n~~~";
        let spans = parse_fence_spans(input);
        assert_eq!(spans.len(), 1);
        assert_eq!(spans[0].marker, "~~~");
    }

    #[test]
    fn parse_unclosed_fence() {
        let input = "```\ncode\nno close";
        let spans = parse_fence_spans(input);
        assert_eq!(spans.len(), 1);
        assert_eq!(spans[0].end, input.len());
    }

    #[test]
    fn parse_indented_fence() {
        let input = "   ```\ncode\n   ```";
        let spans = parse_fence_spans(input);
        assert_eq!(spans.len(), 1);
        assert_eq!(spans[0].indent, "   ");
    }

    #[test]
    fn parse_no_fences() {
        let input = "just text\nno fences here";
        let spans = parse_fence_spans(input);
        assert!(spans.is_empty());
    }

    #[test]
    fn parse_multiple_fences() {
        let input = "```\na\n```\n\n```\nb\n```";
        let spans = parse_fence_spans(input);
        assert_eq!(spans.len(), 2);
    }

    #[test]
    fn closing_fence_needs_same_char() {
        let input = "```\ncode\n~~~\nmore\n```";
        let spans = parse_fence_spans(input);
        assert_eq!(spans.len(), 1);
        assert_eq!(spans[0].marker, "```");
    }

    #[test]
    fn closing_fence_needs_enough_markers() {
        let input = "````\ncode\n```\nstill open\n````";
        let spans = parse_fence_spans(input);
        assert_eq!(spans.len(), 1);
        // The ``` inside is not a valid close (only 3 < 4).
    }

    #[test]
    fn find_span_at_inside() {
        let spans = vec![FenceSpan {
            start: 5,
            end: 20,
            open_line: "```".into(),
            marker: "```".into(),
            indent: String::new(),
        }];
        assert!(find_fence_span_at(&spans, 10).is_some());
        assert!(find_fence_span_at(&spans, 5).is_none()); // at start boundary
        assert!(find_fence_span_at(&spans, 20).is_none()); // at end boundary
        assert!(find_fence_span_at(&spans, 3).is_none()); // before
    }

    #[test]
    fn safe_fence_break() {
        let spans = vec![FenceSpan {
            start: 5,
            end: 20,
            open_line: "```".into(),
            marker: "```".into(),
            indent: String::new(),
        }];
        assert!(is_safe_fence_break(&spans, 3));
        assert!(!is_safe_fence_break(&spans, 10));
    }
}
