//! Code span detection — finds inline code spans and fenced code blocks
//! to determine if a character index is "inside code".
//!
//! Mirrors `buildCodeSpanIndex`, `parseInlineCodeSpans` from
//! `src/markdown/code-spans.ts`.

use super::fences::{self, FenceSpan};
use serde::{Deserialize, Serialize};

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct InlineCodeState {
    pub open: bool,
    pub ticks: usize,
}

/// Result of building a code span index: the set of code regions and the
/// carry-over inline state (for streaming / multi-chunk usage).
pub struct CodeSpanIndex {
    fence_spans: Vec<FenceSpan>,
    inline_spans: Vec<(usize, usize)>,
    pub inline_state: InlineCodeState,
}

impl CodeSpanIndex {
    /// Check if a character at `index` is inside a code span (fence or inline).
    pub fn is_inside(&self, index: usize) -> bool {
        is_inside_fence_span(index, &self.fence_spans)
            || is_inside_inline_span(index, &self.inline_spans)
    }
}

// ---------------------------------------------------------------------------
// Build
// ---------------------------------------------------------------------------

/// Build a code span index for the given text.
///
/// Parses both fenced code blocks and inline code spans, returning
/// an object that can check if any byte offset is "inside code".
pub fn build_code_span_index(text: &str, initial_state: Option<InlineCodeState>) -> CodeSpanIndex {
    let fence_spans = fences::parse_fence_spans(text);
    let start_state = initial_state.unwrap_or_default();
    let (inline_spans, next_state) = parse_inline_code_spans(text, &fence_spans, &start_state);

    CodeSpanIndex {
        fence_spans,
        inline_spans,
        inline_state: next_state,
    }
}

// ---------------------------------------------------------------------------
// Inline code span parsing
// ---------------------------------------------------------------------------

fn parse_inline_code_spans(
    text: &str,
    fence_spans: &[FenceSpan],
    initial_state: &InlineCodeState,
) -> (Vec<(usize, usize)>, InlineCodeState) {
    let bytes = text.as_bytes();
    let len = bytes.len();
    let mut spans: Vec<(usize, usize)> = Vec::new();

    let mut open = initial_state.open;
    let mut ticks = initial_state.ticks;
    let mut open_start: usize = if open { 0 } else { usize::MAX };

    let mut i: usize = 0;
    while i < len {
        // Skip fence spans
        if let Some(fence) = find_fence_span_at_inclusive(fence_spans, i) {
            i = fence.end;
            continue;
        }

        if bytes[i] != b'`' {
            i += 1;
            continue;
        }

        // Count backtick run
        let run_start = i;
        let mut run_length: usize = 0;
        while i < len && bytes[i] == b'`' {
            run_length += 1;
            i += 1;
        }

        if !open {
            open = true;
            ticks = run_length;
            open_start = run_start;
        } else if run_length == ticks {
            spans.push((open_start, i));
            open = false;
            ticks = 0;
            open_start = usize::MAX;
        }
    }

    // Unclosed inline code span extends to end
    if open && open_start != usize::MAX {
        spans.push((open_start, len));
    }

    (spans, InlineCodeState { open, ticks })
}

fn find_fence_span_at_inclusive(spans: &[FenceSpan], index: usize) -> Option<&FenceSpan> {
    spans
        .iter()
        .find(|span| index >= span.start && index < span.end)
}

fn is_inside_fence_span(index: usize, spans: &[FenceSpan]) -> bool {
    spans
        .iter()
        .any(|span| index >= span.start && index < span.end)
}

fn is_inside_inline_span(index: usize, spans: &[(usize, usize)]) -> bool {
    spans
        .iter()
        .any(|&(start, end)| index >= start && index < end)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn no_code_spans() {
        let idx = build_code_span_index("hello world", None);
        assert!(!idx.is_inside(0));
        assert!(!idx.is_inside(5));
    }

    #[test]
    fn inline_code_detected() {
        let text = "hello `code` world";
        let idx = build_code_span_index(text, None);
        // Before backtick
        assert!(!idx.is_inside(0));
        // Inside backtick delimiters
        assert!(idx.is_inside(6));
        assert!(idx.is_inside(10));
        // After closing backtick
        assert!(!idx.is_inside(12));
    }

    #[test]
    fn double_backtick() {
        let text = "a ``code here`` b";
        let idx = build_code_span_index(text, None);
        assert!(idx.is_inside(3));
        assert!(idx.is_inside(12));
        assert!(!idx.is_inside(16));
    }

    #[test]
    fn fence_detected() {
        let text = "before\n```\ncode\n```\nafter";
        let idx = build_code_span_index(text, None);
        assert!(!idx.is_inside(0)); // "before"
        assert!(idx.is_inside(8)); // inside fence
        assert!(!idx.is_inside(20)); // "after"
    }

    #[test]
    fn initial_state_open() {
        let text = "still code` rest";
        let state = InlineCodeState {
            open: true,
            ticks: 1,
        };
        let idx = build_code_span_index(text, Some(state));
        assert!(idx.is_inside(0)); // continuation of open code span
        assert!(!idx.is_inside(12)); // after close
    }

    #[test]
    fn state_propagation() {
        let text = "hello `open";
        let idx = build_code_span_index(text, None);
        assert!(idx.inline_state.open);
        assert_eq!(idx.inline_state.ticks, 1);
    }
}
