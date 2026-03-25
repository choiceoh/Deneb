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
// napi exports
// ---------------------------------------------------------------------------

#[cfg(feature = "napi_binding")]
pub mod napi_exports {
    use super::*;
    use napi::bindgen_prelude::*;
    use napi_derive::napi;

    #[napi(object)]
    pub struct JsStyleSpan {
        pub start: u32,
        pub end: u32,
        pub style: String,
    }

    #[napi(object)]
    pub struct JsLinkSpan {
        pub start: u32,
        pub end: u32,
        pub href: String,
    }

    fn style_from_str(s: &str) -> Option<MarkdownStyle> {
        match s {
            "bold" => Some(MarkdownStyle::Bold),
            "italic" => Some(MarkdownStyle::Italic),
            "strikethrough" => Some(MarkdownStyle::Strikethrough),
            "code" => Some(MarkdownStyle::Code),
            "code_block" => Some(MarkdownStyle::CodeBlock),
            "spoiler" => Some(MarkdownStyle::Spoiler),
            "blockquote" => Some(MarkdownStyle::Blockquote),
            _ => None,
        }
    }

    fn style_to_str(s: MarkdownStyle) -> &'static str {
        match s {
            MarkdownStyle::Bold => "bold",
            MarkdownStyle::Italic => "italic",
            MarkdownStyle::Strikethrough => "strikethrough",
            MarkdownStyle::Code => "code",
            MarkdownStyle::CodeBlock => "code_block",
            MarkdownStyle::Spoiler => "spoiler",
            MarkdownStyle::Blockquote => "blockquote",
        }
    }

    fn to_internal(spans: Vec<JsStyleSpan>) -> Vec<StyleSpan> {
        spans
            .into_iter()
            .filter_map(|s| {
                style_from_str(&s.style).map(|style| StyleSpan {
                    start: s.start as usize,
                    end: s.end as usize,
                    style,
                })
            })
            .collect()
    }

    fn to_js(spans: Vec<StyleSpan>) -> Vec<JsStyleSpan> {
        spans
            .into_iter()
            .map(|s| JsStyleSpan {
                start: s.start as u32,
                end: s.end as u32,
                style: style_to_str(s.style).to_string(),
            })
            .collect()
    }

    #[napi]
    pub fn markdown_merge_style_spans(spans: Vec<JsStyleSpan>) -> Vec<JsStyleSpan> {
        let internal = to_internal(spans);
        to_js(merge_style_spans(&internal))
    }

    #[napi]
    pub fn markdown_clamp_style_spans(
        spans: Vec<JsStyleSpan>,
        max_length: u32,
    ) -> Vec<JsStyleSpan> {
        let internal = to_internal(spans);
        to_js(clamp_style_spans(&internal, max_length as usize))
    }

    #[napi]
    pub fn markdown_clamp_link_spans(spans: Vec<JsLinkSpan>, max_length: u32) -> Vec<JsLinkSpan> {
        let internal: Vec<LinkSpan> = spans
            .into_iter()
            .map(|s| LinkSpan {
                start: s.start as usize,
                end: s.end as usize,
                href: s.href,
            })
            .collect();
        clamp_link_spans(&internal, max_length as usize)
            .into_iter()
            .map(|s| JsLinkSpan {
                start: s.start as u32,
                end: s.end as u32,
                href: s.href,
            })
            .collect()
    }

    #[napi]
    pub fn markdown_slice_style_spans(
        spans: Vec<JsStyleSpan>,
        start: u32,
        end: u32,
    ) -> Vec<JsStyleSpan> {
        let internal = to_internal(spans);
        to_js(slice_style_spans(&internal, start as usize, end as usize))
    }

    #[napi]
    pub fn markdown_slice_link_spans(
        spans: Vec<JsLinkSpan>,
        start: u32,
        end: u32,
    ) -> Vec<JsLinkSpan> {
        let internal: Vec<LinkSpan> = spans
            .into_iter()
            .map(|s| LinkSpan {
                start: s.start as usize,
                end: s.end as usize,
                href: s.href,
            })
            .collect();
        slice_link_spans(&internal, start as usize, end as usize)
            .into_iter()
            .map(|s| JsLinkSpan {
                start: s.start as u32,
                end: s.end as u32,
                href: s.href,
            })
            .collect()
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn merge_empty() {
        assert_eq!(merge_style_spans(&[]), Vec::new());
    }

    #[test]
    fn merge_adjacent_same_style() {
        let spans = vec![
            StyleSpan {
                start: 0,
                end: 5,
                style: MarkdownStyle::Bold,
            },
            StyleSpan {
                start: 5,
                end: 10,
                style: MarkdownStyle::Bold,
            },
        ];
        let merged = merge_style_spans(&spans);
        assert_eq!(merged.len(), 1);
        assert_eq!(merged[0].start, 0);
        assert_eq!(merged[0].end, 10);
    }

    #[test]
    fn merge_overlapping() {
        let spans = vec![
            StyleSpan {
                start: 0,
                end: 7,
                style: MarkdownStyle::Italic,
            },
            StyleSpan {
                start: 5,
                end: 12,
                style: MarkdownStyle::Italic,
            },
        ];
        let merged = merge_style_spans(&spans);
        assert_eq!(merged.len(), 1);
        assert_eq!(merged[0].end, 12);
    }

    #[test]
    fn merge_different_styles_not_merged() {
        let spans = vec![
            StyleSpan {
                start: 0,
                end: 5,
                style: MarkdownStyle::Bold,
            },
            StyleSpan {
                start: 5,
                end: 10,
                style: MarkdownStyle::Italic,
            },
        ];
        let merged = merge_style_spans(&spans);
        assert_eq!(merged.len(), 2);
    }

    #[test]
    fn merge_blockquote_adjacent_not_merged() {
        let spans = vec![
            StyleSpan {
                start: 0,
                end: 5,
                style: MarkdownStyle::Blockquote,
            },
            StyleSpan {
                start: 5,
                end: 10,
                style: MarkdownStyle::Blockquote,
            },
        ];
        let merged = merge_style_spans(&spans);
        assert_eq!(merged.len(), 2, "adjacent blockquotes must not merge");
    }

    #[test]
    fn merge_blockquote_overlapping_merged() {
        let spans = vec![
            StyleSpan {
                start: 0,
                end: 6,
                style: MarkdownStyle::Blockquote,
            },
            StyleSpan {
                start: 5,
                end: 10,
                style: MarkdownStyle::Blockquote,
            },
        ];
        let merged = merge_style_spans(&spans);
        assert_eq!(merged.len(), 1);
        assert_eq!(merged[0].end, 10);
    }

    #[test]
    fn clamp_style_within_bounds() {
        let spans = vec![StyleSpan {
            start: 2,
            end: 8,
            style: MarkdownStyle::Code,
        }];
        let clamped = clamp_style_spans(&spans, 10);
        assert_eq!(clamped[0].start, 2);
        assert_eq!(clamped[0].end, 8);
    }

    #[test]
    fn clamp_style_exceeds_max() {
        let spans = vec![StyleSpan {
            start: 5,
            end: 20,
            style: MarkdownStyle::Bold,
        }];
        let clamped = clamp_style_spans(&spans, 10);
        assert_eq!(clamped[0].end, 10);
    }

    #[test]
    fn clamp_style_drops_empty() {
        let spans = vec![StyleSpan {
            start: 15,
            end: 20,
            style: MarkdownStyle::Bold,
        }];
        let clamped = clamp_style_spans(&spans, 10);
        assert!(clamped.is_empty());
    }

    #[test]
    fn slice_style_basic() {
        let spans = vec![StyleSpan {
            start: 3,
            end: 12,
            style: MarkdownStyle::Italic,
        }];
        let sliced = slice_style_spans(&spans, 5, 10);
        assert_eq!(sliced.len(), 1);
        assert_eq!(sliced[0].start, 0); // 5-5
        assert_eq!(sliced[0].end, 5); // 10-5
    }

    #[test]
    fn slice_style_outside_range() {
        let spans = vec![StyleSpan {
            start: 0,
            end: 3,
            style: MarkdownStyle::Bold,
        }];
        let sliced = slice_style_spans(&spans, 5, 10);
        assert!(sliced.is_empty());
    }

    #[test]
    fn slice_link_basic() {
        let spans = vec![LinkSpan {
            start: 2,
            end: 8,
            href: "https://example.com".into(),
        }];
        let sliced = slice_link_spans(&spans, 3, 7);
        assert_eq!(sliced.len(), 1);
        assert_eq!(sliced[0].start, 0);
        assert_eq!(sliced[0].end, 4);
        assert_eq!(sliced[0].href, "https://example.com");
    }

    #[test]
    fn clamp_link_drops_empty() {
        let spans = vec![LinkSpan {
            start: 10,
            end: 15,
            href: "x".into(),
        }];
        let clamped = clamp_link_spans(&spans, 5);
        assert!(clamped.is_empty());
    }
}
