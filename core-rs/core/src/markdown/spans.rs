//! Style and link span manipulation: merge, clamp, slice.
//!
//! Mirrors `mergeStyleSpans`, `clampStyleSpans`, `clampLinkSpans`,
//! `sliceStyleSpans`, `sliceLinkSpans` from `src/markdown/ir.ts`.

use serde::{Deserialize, Serialize};

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum MarkdownStyle {
    Bold,
    Italic,
    Strikethrough,
    Code,
    #[serde(rename = "code_block")]
    CodeBlock,
    Spoiler,
    Blockquote,
}

impl MarkdownStyle {
    /// Sort rank matching the TypeScript `localeCompare` order on the string
    /// representation. Used for stable sort tie-breaking.
    fn sort_key(self) -> u8 {
        match self {
            Self::Blockquote => 0,
            Self::Bold => 1,
            Self::Code => 2,
            Self::CodeBlock => 3,
            Self::Italic => 4,
            Self::Spoiler => 5,
            Self::Strikethrough => 6,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct StyleSpan {
    pub start: usize,
    pub end: usize,
    pub style: MarkdownStyle,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct LinkSpan {
    pub start: usize,
    pub end: usize,
    pub href: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct MarkdownIR {
    pub text: String,
    pub styles: Vec<StyleSpan>,
    pub links: Vec<LinkSpan>,
}

// ---------------------------------------------------------------------------
// Merge
// ---------------------------------------------------------------------------

/// Merge adjacent or overlapping style spans of the same style.
///
/// Sorts spans by (start, end, style) then merges contiguous runs.
/// Blockquote spans are only merged when they overlap (not when merely
/// adjacent) to prevent style bleed across paragraph boundaries.
pub fn merge_style_spans(spans: &[StyleSpan]) -> Vec<StyleSpan> {
    if spans.is_empty() {
        return Vec::new();
    }

    let mut sorted: Vec<StyleSpan> = spans.to_vec();
    sorted.sort_by(|a, b| {
        a.start
            .cmp(&b.start)
            .then_with(|| a.end.cmp(&b.end))
            .then_with(|| a.style.sort_key().cmp(&b.style.sort_key()))
    });

    let mut merged: Vec<StyleSpan> = Vec::with_capacity(sorted.len());
    for span in sorted {
        if let Some(prev) = merged.last_mut() {
            if prev.style == span.style
                && (span.start < prev.end
                    || (span.start == prev.end && span.style != MarkdownStyle::Blockquote))
            {
                if span.end > prev.end {
                    prev.end = span.end;
                }
                continue;
            }
        }
        merged.push(span);
    }
    merged
}

// ---------------------------------------------------------------------------
// Clamp
// ---------------------------------------------------------------------------

/// Clamp style spans to `[0, max_length)`, dropping empty spans.
pub fn clamp_style_spans(spans: &[StyleSpan], max_length: usize) -> Vec<StyleSpan> {
    let mut clamped = Vec::with_capacity(spans.len());
    for span in spans {
        let start = span.start.min(max_length);
        let end = span.end.max(start).min(max_length);
        if end > start {
            clamped.push(StyleSpan {
                start,
                end,
                style: span.style,
            });
        }
    }
    clamped
}

/// Clamp link spans to `[0, max_length)`, dropping empty spans.
pub fn clamp_link_spans(spans: &[LinkSpan], max_length: usize) -> Vec<LinkSpan> {
    let mut clamped = Vec::with_capacity(spans.len());
    for span in spans {
        let start = span.start.min(max_length);
        let end = span.end.max(start).min(max_length);
        if end > start {
            clamped.push(LinkSpan {
                start,
                end,
                href: span.href.clone(),
            });
        }
    }
    clamped
}

// ---------------------------------------------------------------------------
// Slice
// ---------------------------------------------------------------------------

fn resolve_slice_bounds(
    span_start: usize,
    span_end: usize,
    start: usize,
    end: usize,
) -> Option<(usize, usize)> {
    let slice_start = span_start.max(start);
    let slice_end = span_end.min(end);
    if slice_end <= slice_start {
        None
    } else {
        Some((slice_start, slice_end))
    }
}

/// Slice style spans to the range `[start, end)`, rebasing offsets to 0.
/// The result is merged.
pub fn slice_style_spans(spans: &[StyleSpan], start: usize, end: usize) -> Vec<StyleSpan> {
    if spans.is_empty() {
        return Vec::new();
    }
    let mut sliced = Vec::new();
    for span in spans {
        if let Some((s, e)) = resolve_slice_bounds(span.start, span.end, start, end) {
            sliced.push(StyleSpan {
                start: s - start,
                end: e - start,
                style: span.style,
            });
        }
    }
    merge_style_spans(&sliced)
}

/// Slice link spans to the range `[start, end)`, rebasing offsets to 0.
pub fn slice_link_spans(spans: &[LinkSpan], start: usize, end: usize) -> Vec<LinkSpan> {
    if spans.is_empty() {
        return Vec::new();
    }
    let mut sliced = Vec::new();
    for span in spans {
        if let Some((s, e)) = resolve_slice_bounds(span.start, span.end, start, end) {
            sliced.push(LinkSpan {
                start: s - start,
                end: e - start,
                href: span.href.clone(),
            });
        }
    }
    sliced
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------
