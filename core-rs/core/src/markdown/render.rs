//! Marker-based rendering of MarkdownIR.
//!
//! Mirrors `renderMarkdownWithMarkers` from `src/markdown/render.ts`.
//! Takes a MarkdownIR and a set of style markers, producing a string
//! with opening/closing markers interleaved around the text.

use super::spans::{LinkSpan, MarkdownIR, MarkdownStyle, StyleSpan};
use serde::{Deserialize, Serialize};
use std::collections::{BTreeSet, HashMap};

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RenderStyleMarker {
    pub open: String,
    pub close: String,
}

pub type RenderStyleMap = HashMap<MarkdownStyle, RenderStyleMarker>;

#[derive(Debug, Clone)]
pub struct RenderLink {
    pub start: usize,
    pub end: usize,
    pub open: String,
    pub close: String,
}

/// Options for rendering MarkdownIR with markers.
pub struct RenderOptions<F>
where
    F: Fn(&str) -> String,
{
    pub style_markers: RenderStyleMap,
    pub escape_text: F,
    pub build_link: Option<Box<dyn Fn(&LinkSpan, &str) -> Option<RenderLink>>>,
}

// ---------------------------------------------------------------------------
// Style ordering (matches TypeScript STYLE_ORDER)
// ---------------------------------------------------------------------------

const STYLE_ORDER: &[MarkdownStyle] = &[
    MarkdownStyle::Blockquote,
    MarkdownStyle::CodeBlock,
    MarkdownStyle::Code,
    MarkdownStyle::Bold,
    MarkdownStyle::Italic,
    MarkdownStyle::Strikethrough,
    MarkdownStyle::Spoiler,
];

fn style_rank(style: MarkdownStyle) -> usize {
    STYLE_ORDER
        .iter()
        .position(|&s| s == style)
        .unwrap_or(usize::MAX)
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

/// Render a MarkdownIR into a string with style markers and escaped text.
pub fn render_markdown_with_markers<F>(ir: &MarkdownIR, options: &RenderOptions<F>) -> String
where
    F: Fn(&str) -> String,
{
    let text = &ir.text;
    if text.is_empty() {
        return String::new();
    }

    // Filter and sort style spans
    let styled: Vec<&StyleSpan> = {
        let mut v: Vec<&StyleSpan> = ir
            .styles
            .iter()
            .filter(|s| s.start != s.end && options.style_markers.contains_key(&s.style))
            .collect();
        v.sort_by(|a, b| {
            a.start
                .cmp(&b.start)
                .then_with(|| b.end.cmp(&a.end))
                .then_with(|| style_rank(a.style).cmp(&style_rank(b.style)))
        });
        v
    };

    // Collect boundary points
    let mut boundaries = BTreeSet::new();
    boundaries.insert(0);
    boundaries.insert(text.len());

    // Build starts_at map for styles
    let mut starts_at: HashMap<usize, Vec<(usize, &StyleSpan)>> = HashMap::new();
    for (idx, span) in styled.iter().enumerate() {
        boundaries.insert(span.start);
        boundaries.insert(span.end);
        starts_at.entry(span.start).or_default().push((idx, span));
    }
    // Sort each bucket: wider spans first, then by style rank
    for bucket in starts_at.values_mut() {
        bucket.sort_by(|a, b| {
            b.1.end
                .cmp(&a.1.end)
                .then_with(|| style_rank(a.1.style).cmp(&style_rank(b.1.style)))
        });
    }

    // Build link starts
    let mut link_starts: HashMap<usize, Vec<RenderLink>> = HashMap::new();
    if let Some(ref build_link) = options.build_link {
        for link in &ir.links {
            if link.start == link.end {
                continue;
            }
            if let Some(rendered) = build_link(link, text) {
                boundaries.insert(rendered.start);
                boundaries.insert(rendered.end);
                link_starts
                    .entry(rendered.start)
                    .or_default()
                    .push(rendered);
            }
        }
    }

    let points: Vec<usize> = boundaries.into_iter().collect();
    let mut stack: Vec<(&str, usize)> = Vec::new(); // (close_str, end_pos)
    let mut out = String::with_capacity(text.len() * 2);

    for (i, &pos) in points.iter().enumerate() {
        // Close items in LIFO order at this position
        while let Some(&(_, end)) = stack.last() {
            if end == pos {
                let (close, _) = stack.pop().unwrap();
                out.push_str(close);
            } else {
                break;
            }
        }

        // Collect opening items at this position
        enum OpenItem<'a> {
            Link(&'a RenderLink),
            Style(&'a StyleSpan),
        }

        let mut opening: Vec<(usize, OpenItem)> = Vec::new();

        if let Some(links) = link_starts.get(&pos) {
            for (idx, link) in links.iter().enumerate() {
                opening.push((idx, OpenItem::Link(link)));
            }
        }

        if let Some(styles) = starts_at.get(&pos) {
            for &(idx, span) in styles {
                opening.push((idx, OpenItem::Style(span)));
            }
        }

        if !opening.is_empty() {
            // Sort: wider end first, links before styles, then by rank/index
            opening.sort_by(|a, b| {
                let end_a = match &a.1 {
                    OpenItem::Link(l) => l.end,
                    OpenItem::Style(s) => s.end,
                };
                let end_b = match &b.1 {
                    OpenItem::Link(l) => l.end,
                    OpenItem::Style(s) => s.end,
                };
                end_b.cmp(&end_a).then_with(|| {
                    let kind_a = match &a.1 {
                        OpenItem::Link(_) => 0,
                        OpenItem::Style(_) => 1,
                    };
                    let kind_b = match &b.1 {
                        OpenItem::Link(_) => 0,
                        OpenItem::Style(_) => 1,
                    };
                    kind_a.cmp(&kind_b).then_with(|| match (&a.1, &b.1) {
                        (OpenItem::Style(sa), OpenItem::Style(sb)) => {
                            style_rank(sa.style).cmp(&style_rank(sb.style))
                        }
                        _ => a.0.cmp(&b.0),
                    })
                })
            });

            for item in &opening {
                match &item.1 {
                    OpenItem::Link(link) => {
                        out.push_str(&link.open);
                        stack.push((&link.close, link.end));
                    }
                    OpenItem::Style(span) => {
                        let marker = &options.style_markers[&span.style];
                        out.push_str(&marker.open);
                        stack.push((&marker.close, span.end));
                    }
                }
            }
        }

        // Append escaped text segment
        if let Some(&next) = points.get(i + 1) {
            if next > pos && pos < text.len() {
                let end = next.min(text.len());
                let segment = &text[pos..end];
                out.push_str(&(options.escape_text)(segment));
            }
        }
    }

    out
}

// ---------------------------------------------------------------------------
// Convenience: render with simple serde-friendly options
// ---------------------------------------------------------------------------

/// Simplified render options for use via FFI/napi (serde-friendly).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SimpleRenderOptions {
    /// Map of style name to (open, close) marker strings.
    pub style_markers: HashMap<String, (String, String)>,
}

/// Render MarkdownIR with simple options (no link builder, identity escape).
/// Suitable for FFI where closures cannot be passed.
pub fn render_markdown_simple(ir: &MarkdownIR, opts: &SimpleRenderOptions) -> String {
    let mut style_map = RenderStyleMap::new();
    let style_names: &[(&str, MarkdownStyle)] = &[
        ("bold", MarkdownStyle::Bold),
        ("italic", MarkdownStyle::Italic),
        ("strikethrough", MarkdownStyle::Strikethrough),
        ("code", MarkdownStyle::Code),
        ("code_block", MarkdownStyle::CodeBlock),
        ("spoiler", MarkdownStyle::Spoiler),
        ("blockquote", MarkdownStyle::Blockquote),
    ];

    for &(name, style) in style_names {
        if let Some((open, close)) = opts.style_markers.get(name) {
            style_map.insert(
                style,
                RenderStyleMarker {
                    open: open.clone(),
                    close: close.clone(),
                },
            );
        }
    }

    let options = RenderOptions {
        style_markers: style_map,
        escape_text: |s: &str| s.to_string(),
        build_link: None,
    };

    render_markdown_with_markers(ir, &options)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    fn make_markers() -> RenderStyleMap {
        let mut m = RenderStyleMap::new();
        m.insert(
            MarkdownStyle::Bold,
            RenderStyleMarker {
                open: "**".into(),
                close: "**".into(),
            },
        );
        m.insert(
            MarkdownStyle::Italic,
            RenderStyleMarker {
                open: "_".into(),
                close: "_".into(),
            },
        );
        m.insert(
            MarkdownStyle::Code,
            RenderStyleMarker {
                open: "`".into(),
                close: "`".into(),
            },
        );
        m
    }

    #[test]
    fn render_empty() {
        let ir = MarkdownIR {
            text: String::new(),
            styles: vec![],
            links: vec![],
        };
        let opts = RenderOptions {
            style_markers: make_markers(),
            escape_text: |s: &str| s.to_string(),
            build_link: None,
        };
        assert_eq!(render_markdown_with_markers(&ir, &opts), "");
    }

    #[test]
    fn render_plain_text() {
        let ir = MarkdownIR {
            text: "hello world".into(),
            styles: vec![],
            links: vec![],
        };
        let opts = RenderOptions {
            style_markers: make_markers(),
            escape_text: |s: &str| s.to_string(),
            build_link: None,
        };
        assert_eq!(render_markdown_with_markers(&ir, &opts), "hello world");
    }

    #[test]
    fn render_bold() {
        let ir = MarkdownIR {
            text: "hello world".into(),
            styles: vec![StyleSpan {
                start: 0,
                end: 5,
                style: MarkdownStyle::Bold,
            }],
            links: vec![],
        };
        let opts = RenderOptions {
            style_markers: make_markers(),
            escape_text: |s: &str| s.to_string(),
            build_link: None,
        };
        assert_eq!(render_markdown_with_markers(&ir, &opts), "**hello** world");
    }

    #[test]
    fn render_nested_bold_italic() {
        let ir = MarkdownIR {
            text: "hello".into(),
            styles: vec![
                StyleSpan {
                    start: 0,
                    end: 5,
                    style: MarkdownStyle::Bold,
                },
                StyleSpan {
                    start: 0,
                    end: 5,
                    style: MarkdownStyle::Italic,
                },
            ],
            links: vec![],
        };
        let opts = RenderOptions {
            style_markers: make_markers(),
            escape_text: |s: &str| s.to_string(),
            build_link: None,
        };
        let result = render_markdown_with_markers(&ir, &opts);
        assert!(result.contains("**"));
        assert!(result.contains("_"));
        assert!(result.contains("hello"));
    }

    #[test]
    fn render_with_escape() {
        let ir = MarkdownIR {
            text: "a<b".into(),
            styles: vec![],
            links: vec![],
        };
        let opts = RenderOptions {
            style_markers: make_markers(),
            escape_text: |s: &str| s.replace('<', "&lt;"),
            build_link: None,
        };
        assert_eq!(render_markdown_with_markers(&ir, &opts), "a&lt;b");
    }

    #[test]
    fn render_simple_api() {
        let ir = MarkdownIR {
            text: "bold text".into(),
            styles: vec![StyleSpan {
                start: 0,
                end: 4,
                style: MarkdownStyle::Bold,
            }],
            links: vec![],
        };
        let mut markers = HashMap::new();
        markers.insert("bold".to_string(), ("**".to_string(), "**".to_string()));
        let opts = SimpleRenderOptions {
            style_markers: markers,
        };
        assert_eq!(render_markdown_simple(&ir, &opts), "**bold** text");
    }
}
