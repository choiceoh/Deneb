//! Markdown-to-IR parser using pulldown-cmark.
//!
//! Mirrors `markdownToIR` / `markdownToIRWithMeta` from `src/markdown/ir.ts`.
//! Converts markdown text into a `MarkdownIR` (plain text + style spans + links).
//!
//! Table rendering is in the sibling `tables` module.
//! Internal render state lives in the sibling `render_state` module.
//! Spoiler preprocessing lives in the sibling `spoilers` module.

// Re-export HeadingStyle and TableMode so external callers and tests can access them
// via `markdown::parser::HeadingStyle` (same path as before the split).
pub use super::render_state::{HeadingStyle, TableMode};
use super::render_state::{ListEntry, RenderState, RenderTarget, TableState};
use super::spans::{
    clamp_link_spans, clamp_style_spans, merge_style_spans, MarkdownIR, MarkdownStyle,
};
use super::spoilers::{handle_spoiler_text, preprocess_spoilers, SPOILER_CLOSE, SPOILER_OPEN};
use super::tables;
use pulldown_cmark::{Event, Options, Parser, Tag, TagEnd};
use serde::{Deserialize, Serialize};

// ---------------------------------------------------------------------------
// Parse options
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ParseOptions {
    #[serde(default = "default_true")]
    pub linkify: bool,
    #[serde(default)]
    pub enable_spoilers: bool,
    #[serde(default = "default_heading_none")]
    pub heading_style: HeadingStyle,
    #[serde(default)]
    pub blockquote_prefix: String,
    #[serde(default = "default_true")]
    pub autolink: bool,
    #[serde(default = "default_table_off")]
    pub table_mode: TableMode,
}

fn default_true() -> bool {
    true
}
fn default_heading_none() -> HeadingStyle {
    HeadingStyle::None
}
fn default_table_off() -> TableMode {
    TableMode::Off
}

impl Default for ParseOptions {
    fn default() -> Self {
        Self {
            linkify: true,
            enable_spoilers: false,
            heading_style: HeadingStyle::None,
            blockquote_prefix: String::new(),
            autolink: true,
            table_mode: TableMode::Off,
        }
    }
}

// ---------------------------------------------------------------------------
// Main parser
// ---------------------------------------------------------------------------

/// Parse markdown into an IR representation.
pub fn markdown_to_ir(markdown: &str, options: &ParseOptions) -> MarkdownIR {
    markdown_to_ir_with_meta(markdown, options).0
}

/// Parse markdown into an IR representation, also indicating if tables were found.
pub fn markdown_to_ir_with_meta(markdown: &str, options: &ParseOptions) -> (MarkdownIR, bool) {
    let input = if options.enable_spoilers {
        preprocess_spoilers(markdown)
    } else {
        markdown.to_string()
    };

    let mut pulldown_opts = Options::empty();
    pulldown_opts.insert(Options::ENABLE_STRIKETHROUGH);
    if options.table_mode != TableMode::Off {
        pulldown_opts.insert(Options::ENABLE_TABLES);
    }

    let parser = Parser::new_ext(&input, pulldown_opts);
    let mut state = RenderState::new(
        options.heading_style,
        options.blockquote_prefix.clone(),
        options.table_mode,
    );

    // Track whether we're in a code block to accumulate text
    let mut in_code_block = false;
    let mut code_block_content = String::new();

    for event in parser {
        match event {
            Event::Start(tag) => match tag {
                Tag::Emphasis => state.open_style(MarkdownStyle::Italic),
                Tag::Strong => state.open_style(MarkdownStyle::Bold),
                Tag::Strikethrough => state.open_style(MarkdownStyle::Strikethrough),
                Tag::Link { dest_url, .. } => {
                    state.handle_link_open(dest_url.to_string());
                }
                Tag::Heading { .. } => {
                    if state.heading_style == HeadingStyle::Bold {
                        state.open_style(MarkdownStyle::Bold);
                    }
                }
                Tag::BlockQuote(_) => {
                    if !state.blockquote_prefix.is_empty() {
                        let prefix = state.blockquote_prefix.clone();
                        state.text.push_str(&prefix);
                    }
                    state.open_style(MarkdownStyle::Blockquote);
                }
                Tag::List(first_item) => {
                    if !state.list_stack.is_empty() {
                        state.text.push('\n');
                    }
                    match first_item {
                        Some(start) => {
                            state.list_stack.push(ListEntry {
                                ordered: true,
                                index: (start as usize).saturating_sub(1),
                            });
                        }
                        None => {
                            state.list_stack.push(ListEntry {
                                ordered: false,
                                index: 0,
                            });
                        }
                    }
                }
                Tag::Item => {
                    state.append_list_prefix();
                }
                Tag::CodeBlock(_) => {
                    in_code_block = true;
                    code_block_content.clear();
                }
                Tag::Table(_) => {
                    if state.table_mode != TableMode::Off {
                        state.table = Some(TableState::new());
                        state.has_tables = true;
                    }
                }
                Tag::TableHead => {
                    if let Some(ref mut table) = state.table {
                        table.in_header = true;
                    }
                }
                Tag::TableRow => {
                    if let Some(ref mut table) = state.table {
                        table.current_row = Vec::new();
                    }
                }
                Tag::TableCell => {
                    if let Some(ref mut table) = state.table {
                        table.current_cell = Some(RenderTarget::new());
                    }
                }
                Tag::Paragraph | Tag::Image { .. } | Tag::HtmlBlock | Tag::MetadataBlock(_) => {}
                _ => {}
            },

            Event::End(tag_end) => match tag_end {
                TagEnd::Emphasis => state.close_style(MarkdownStyle::Italic),
                TagEnd::Strong => state.close_style(MarkdownStyle::Bold),
                TagEnd::Strikethrough => state.close_style(MarkdownStyle::Strikethrough),
                TagEnd::Link => state.handle_link_close(),
                TagEnd::Heading(_) => {
                    if state.heading_style == HeadingStyle::Bold {
                        state.close_style(MarkdownStyle::Bold);
                    }
                    state.append_paragraph_separator();
                }
                TagEnd::BlockQuote(_) => {
                    state.close_style(MarkdownStyle::Blockquote);
                }
                TagEnd::Paragraph => {
                    state.append_paragraph_separator();
                }
                TagEnd::List(_) => {
                    state.list_stack.pop();
                    if state.list_stack.is_empty() {
                        state.text.push('\n');
                    }
                }
                TagEnd::Item => {
                    if !state.text.ends_with('\n') {
                        state.text.push('\n');
                    }
                }
                TagEnd::CodeBlock => {
                    in_code_block = false;
                    let content = std::mem::take(&mut code_block_content);
                    state.render_code_block(&content);
                }
                TagEnd::Table => {
                    if state.table.is_some() {
                        match state.table_mode {
                            TableMode::Bullets => tables::render_table_as_bullets(&mut state),
                            TableMode::Code => tables::render_table_as_code(&mut state),
                            TableMode::Off => {}
                        }
                    }
                    state.table = None;
                }
                TagEnd::TableHead => {
                    if let Some(ref mut table) = state.table {
                        // pulldown-cmark 0.12+: TableHead contains cells directly
                        // without a wrapping TableRow, so flush current_row as headers here.
                        if table.in_header && !table.current_row.is_empty() {
                            table.headers = std::mem::take(&mut table.current_row);
                        }
                        table.in_header = false;
                    }
                }
                TagEnd::TableRow => {
                    if let Some(ref mut table) = state.table {
                        let row = std::mem::take(&mut table.current_row);
                        if table.in_header {
                            table.headers = row;
                        } else {
                            table.rows.push(row);
                        }
                    }
                }
                TagEnd::TableCell => {
                    if let Some(ref mut table) = state.table {
                        if let Some(ref mut cell) = table.current_cell {
                            let finished = tables::finish_table_cell(cell);
                            table.current_row.push(finished);
                        }
                        table.current_cell = None;
                    }
                }
                _ => {}
            },

            Event::Text(text) => {
                if in_code_block {
                    code_block_content.push_str(&text);
                } else {
                    // Check for spoiler sentinels
                    if options.enable_spoilers
                        && (text.contains(SPOILER_OPEN) || text.contains(SPOILER_CLOSE))
                    {
                        handle_spoiler_text(&mut state, &text);
                    } else {
                        state.append_text(&text);
                    }
                }
            }

            Event::Code(code) => {
                state.render_inline_code(&code);
            }

            Event::SoftBreak | Event::HardBreak => {
                state.append_text("\n");
            }

            Event::Rule => {
                state.text.push_str("───\n\n");
            }

            Event::Html(html) | Event::InlineHtml(html) => {
                state.append_text(&html);
            }

            _ => {}
        }
    }

    state.close_remaining_styles();

    // Final trimming (matching TypeScript behavior)
    let trimmed_len = state.text.trim_end().len();
    let mut code_block_end: usize = 0;
    for span in &state.styles {
        if span.style == MarkdownStyle::CodeBlock && span.end > code_block_end {
            code_block_end = span.end;
        }
    }
    let final_length = trimmed_len.max(code_block_end);
    let final_text = if final_length == state.text.len() {
        state.text
    } else {
        state.text[..final_length].to_string()
    };

    let ir = MarkdownIR {
        text: final_text.clone(),
        styles: merge_style_spans(&clamp_style_spans(&state.styles, final_length)),
        links: clamp_link_spans(&state.links, final_length),
    };

    (ir, state.has_tables)
}

// ---------------------------------------------------------------------------
// napi exports
// ---------------------------------------------------------------------------

#[cfg(feature = "napi_binding")]
pub mod napi_exports {
    use super::*;
    use crate::markdown::spans::napi_exports::{JsLinkSpan, JsStyleSpan};
    use napi_derive::napi;

    #[napi(object)]
    pub struct JsParseOptions {
        pub linkify: Option<bool>,
        #[napi(js_name = "enableSpoilers")]
        pub enable_spoilers: Option<bool>,
        #[napi(js_name = "headingStyle")]
        pub heading_style: Option<String>,
        #[napi(js_name = "blockquotePrefix")]
        pub blockquote_prefix: Option<String>,
        pub autolink: Option<bool>,
        #[napi(js_name = "tableMode")]
        pub table_mode: Option<String>,
    }

    #[napi(object)]
    pub struct JsMarkdownIR {
        pub text: String,
        pub styles: Vec<JsStyleSpan>,
        pub links: Vec<JsLinkSpan>,
    }

    #[napi(object)]
    pub struct JsMarkdownIRWithMeta {
        pub ir: JsMarkdownIR,
        #[napi(js_name = "hasTables")]
        pub has_tables: bool,
    }

    fn to_parse_options(opts: Option<JsParseOptions>) -> ParseOptions {
        match opts {
            None => ParseOptions::default(),
            Some(o) => ParseOptions {
                linkify: o.linkify.unwrap_or(true),
                enable_spoilers: o.enable_spoilers.unwrap_or(false),
                heading_style: match o.heading_style.as_deref() {
                    Some("bold") => HeadingStyle::Bold,
                    _ => HeadingStyle::None,
                },
                blockquote_prefix: o.blockquote_prefix.unwrap_or_default(),
                autolink: o.autolink.unwrap_or(true),
                table_mode: match o.table_mode.as_deref() {
                    Some("bullets") => TableMode::Bullets,
                    Some("code") => TableMode::Code,
                    _ => TableMode::Off,
                },
            },
        }
    }

    fn ir_to_js(ir: MarkdownIR) -> JsMarkdownIR {
        JsMarkdownIR {
            text: ir.text,
            styles: ir
                .styles
                .into_iter()
                .map(|s| {
                    let style_str = match s.style {
                        MarkdownStyle::Bold => "bold",
                        MarkdownStyle::Italic => "italic",
                        MarkdownStyle::Strikethrough => "strikethrough",
                        MarkdownStyle::Code => "code",
                        MarkdownStyle::CodeBlock => "code_block",
                        MarkdownStyle::Spoiler => "spoiler",
                        MarkdownStyle::Blockquote => "blockquote",
                    };
                    JsStyleSpan {
                        start: s.start as u32,
                        end: s.end as u32,
                        style: style_str.to_string(),
                    }
                })
                .collect(),
            links: ir
                .links
                .into_iter()
                .map(|l| JsLinkSpan {
                    start: l.start as u32,
                    end: l.end as u32,
                    href: l.href,
                })
                .collect(),
        }
    }

    #[napi]
    pub fn markdown_to_ir(markdown: String, options: Option<JsParseOptions>) -> JsMarkdownIR {
        let opts = to_parse_options(options);
        let ir = super::markdown_to_ir(&markdown, &opts);
        ir_to_js(ir)
    }

    #[napi]
    pub fn markdown_to_ir_with_meta(
        markdown: String,
        options: Option<JsParseOptions>,
    ) -> JsMarkdownIRWithMeta {
        let opts = to_parse_options(options);
        let (ir, has_tables) = super::markdown_to_ir_with_meta(&markdown, &opts);
        JsMarkdownIRWithMeta {
            ir: ir_to_js(ir),
            has_tables,
        }
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(md: &str) -> MarkdownIR {
        markdown_to_ir(md, &ParseOptions::default())
    }

    fn parse_with(md: &str, opts: ParseOptions) -> MarkdownIR {
        markdown_to_ir(md, &opts)
    }

    #[test]
    fn plain_text() {
        let ir = parse("hello world");
        assert_eq!(ir.text, "hello world");
        assert!(ir.styles.is_empty());
        assert!(ir.links.is_empty());
    }

    #[test]
    fn inline_styles() {
        let cases: &[(&str, &str, MarkdownStyle)] = &[
            ("**bold**", "bold", MarkdownStyle::Bold),
            ("*italic*", "italic", MarkdownStyle::Italic),
            ("~~strike~~", "strike", MarkdownStyle::Strikethrough),
        ];
        for (input, want_text, want_style) in cases {
            let ir = parse(input);
            assert_eq!(ir.text, *want_text, "input={input:?}");
            assert_eq!(ir.styles.len(), 1, "input={input:?}");
            assert_eq!(ir.styles[0].style, *want_style, "input={input:?}");
        }
    }

    #[test]
    fn inline_code() {
        let ir = parse("use `code` here");
        assert_eq!(ir.text, "use code here");
        assert_eq!(ir.styles.len(), 1);
        assert_eq!(ir.styles[0].style, MarkdownStyle::Code);
        assert_eq!(ir.styles[0].start, 4);
        assert_eq!(ir.styles[0].end, 8);
    }

    #[test]
    fn code_block() {
        let ir = parse("```\ncode\n```");
        assert!(ir.text.contains("code"));
        assert!(ir
            .styles
            .iter()
            .any(|s| s.style == MarkdownStyle::CodeBlock));
    }

    #[test]
    fn link() {
        let ir = parse("[click](https://example.com)");
        assert_eq!(ir.text, "click");
        assert_eq!(ir.links.len(), 1);
        assert_eq!(ir.links[0].href, "https://example.com");
        assert_eq!(ir.links[0].start, 0);
        assert_eq!(ir.links[0].end, 5);
    }

    #[test]
    fn heading_bold() {
        let ir = parse_with(
            "# Title",
            ParseOptions {
                heading_style: HeadingStyle::Bold,
                ..Default::default()
            },
        );
        assert_eq!(ir.text.trim(), "Title");
        assert!(ir.styles.iter().any(|s| s.style == MarkdownStyle::Bold));
    }

    #[test]
    fn heading_none() {
        let ir = parse("# Title");
        assert_eq!(ir.text.trim(), "Title");
        assert!(ir.styles.is_empty());
    }

    #[test]
    fn bullet_list() {
        let ir = parse("- a\n- b");
        assert!(ir.text.contains("• a"));
        assert!(ir.text.contains("• b"));
    }

    #[test]
    fn ordered_list() {
        let ir = parse("1. first\n2. second");
        assert!(ir.text.contains("1. first"));
        assert!(ir.text.contains("2. second"));
    }

    #[test]
    fn horizontal_rule() {
        let ir = parse("---");
        assert!(ir.text.contains("───"));
    }

    #[test]
    fn blockquote() {
        let ir = parse("> quoted");
        assert_eq!(ir.text.trim(), "quoted");
        assert!(ir
            .styles
            .iter()
            .any(|s| s.style == MarkdownStyle::Blockquote));
    }

    #[test]
    fn blockquote_prefix() {
        let ir = parse_with(
            "> text",
            ParseOptions {
                blockquote_prefix: "> ".to_string(),
                ..Default::default()
            },
        );
        assert!(ir.text.starts_with("> "));
    }

    #[test]
    fn spoiler() {
        let ir = parse_with(
            "||hidden||",
            ParseOptions {
                enable_spoilers: true,
                ..Default::default()
            },
        );
        assert_eq!(ir.text.trim(), "hidden");
        assert!(ir.styles.iter().any(|s| s.style == MarkdownStyle::Spoiler));
    }

    #[test]
    fn nested_styles() {
        let ir = parse("**bold *and italic***");
        assert!(ir.styles.iter().any(|s| s.style == MarkdownStyle::Bold));
        assert!(ir.styles.iter().any(|s| s.style == MarkdownStyle::Italic));
    }

    #[test]
    fn paragraphs_separated() {
        let ir = parse("first\n\nsecond");
        assert!(ir.text.contains("\n\n"));
    }

    #[test]
    fn soft_break() {
        let ir = parse("line1\nline2");
        // In standard markdown, soft break within a paragraph becomes a space or newline
        assert!(ir.text.contains("line1") && ir.text.contains("line2"));
    }

    #[test]
    fn table_bullets() {
        let ir = parse_with(
            "| A | B |\n|---|---|\n| 1 | 2 |",
            ParseOptions {
                table_mode: TableMode::Bullets,
                ..Default::default()
            },
        );
        assert!(ir.text.contains("1"));
        assert!(ir.text.contains("2"));
    }

    #[test]
    fn table_bullets_header_value_format() {
        let ir = parse_with(
            "| Name | Value |\n|------|-------|\n| A | 1 |\n| B | 2 |",
            ParseOptions {
                table_mode: TableMode::Bullets,
                ..Default::default()
            },
        );
        // First column is label (bold), other columns as "Header: Value" bullets
        assert!(
            ir.text.contains("Value: 1"),
            "expected 'Value: 1' in {:?}",
            ir.text
        );
        assert!(
            ir.text.contains("Value: 2"),
            "expected 'Value: 2' in {:?}",
            ir.text
        );
        assert!(ir.text.contains("A"), "expected label 'A' in {:?}", ir.text);
        assert!(ir.text.contains("B"), "expected label 'B' in {:?}", ir.text);
    }

    #[test]
    fn table_code() {
        let ir = parse_with(
            "| A | B |\n|---|---|\n| 1 | 2 |",
            ParseOptions {
                table_mode: TableMode::Code,
                ..Default::default()
            },
        );
        assert!(ir.text.contains("|"));
        assert!(ir.text.contains("---"));
        assert!(ir
            .styles
            .iter()
            .any(|s| s.style == MarkdownStyle::CodeBlock));
    }

    #[test]
    fn table_bullets_trimmed_cells() {
        let ir = parse_with(
            "| Name  | Value |\n|-------|-------|\n|  A    |   1   |",
            ParseOptions {
                table_mode: TableMode::Bullets,
                ..Default::default()
            },
        );
        assert!(
            ir.text.contains("Value: 1"),
            "expected trimmed value output, got {:?}",
            ir.text
        );
        assert!(!ir.text.contains("Value:   1"), "text={:?}", ir.text);
    }

    #[test]
    fn table_bullets_skip_empty_value_cells() {
        let ir = parse_with(
            "| Name | Value |\n|------|-------|\n| A | |\n| B | 2 |",
            ParseOptions {
                table_mode: TableMode::Bullets,
                ..Default::default()
            },
        );
        assert!(ir.text.contains("B"));
        assert!(ir.text.contains("Value: 2"));
        assert!(!ir.text.contains("Value: \n"), "text={:?}", ir.text);
    }

    #[test]
    fn table_bullets_fallback_column_name_when_header_empty() {
        let ir = parse_with(
            "| Name | |\n|------|--|\n| A | 1 |",
            ParseOptions {
                table_mode: TableMode::Bullets,
                ..Default::default()
            },
        );
        assert!(
            ir.text.contains("Column 1: 1"),
            "expected fallback column label, got {:?}",
            ir.text
        );
    }

    #[test]
    fn table_bullets_preserve_links_and_styles() {
        let ir = parse_with(
            "| Name | Notes |\n|------|-------|\n| A | **[site](https://example.com)** |",
            ParseOptions {
                table_mode: TableMode::Bullets,
                ..Default::default()
            },
        );
        assert!(ir.text.contains("site"));
        assert_eq!(ir.links.len(), 1);
        assert_eq!(ir.links[0].href, "https://example.com");
        assert!(
            ir.styles.iter().any(|s| s.style == MarkdownStyle::Bold),
            "expected bold style spans for row labels or cell styles"
        );
    }

    #[test]
    fn table_code_trims_and_aligns_columns() {
        let ir = parse_with(
            "| Name | Value |\n|------|-------|\n|  A   |   1   |\n| B | 22 |",
            ParseOptions {
                table_mode: TableMode::Code,
                ..Default::default()
            },
        );
        let expected = "| Name | Value |\n| ---- | ----- |\n| A    | 1     |\n| B    | 22    |\n";
        assert!(
            ir.text.contains(expected),
            "expected aligned table block, got {:?}",
            ir.text
        );
        assert!(ir
            .styles
            .iter()
            .any(|s| s.style == MarkdownStyle::CodeBlock));
    }

    #[test]
    fn has_tables_flag() {
        let (_, has_tables) = markdown_to_ir_with_meta(
            "| A |\n|---|\n| 1 |",
            &ParseOptions {
                table_mode: TableMode::Bullets,
                ..Default::default()
            },
        );
        assert!(has_tables);
    }

    #[test]
    fn no_tables_flag() {
        let (_, has_tables) = markdown_to_ir_with_meta("just text", &ParseOptions::default());
        assert!(!has_tables);
    }

    #[test]
    fn table_mode_off_does_not_report_table_meta() {
        let (_, has_tables) =
            markdown_to_ir_with_meta("| A |\n|---|\n| 1 |", &ParseOptions::default());
        assert!(!has_tables);
    }

    #[test]
    fn has_tables_flag_in_code_mode() {
        let (_, has_tables) = markdown_to_ir_with_meta(
            "| A |\n|---|\n| 1 |",
            &ParseOptions {
                table_mode: TableMode::Code,
                ..Default::default()
            },
        );
        assert!(has_tables);
    }

    #[test]
    fn image_alt_text() {
        let ir = parse("![alt text](img.png)");
        // pulldown-cmark emits alt text
        assert!(ir.text.contains("alt text"));
    }

    #[test]
    fn empty_input() {
        let ir = parse("");
        assert!(ir.text.is_empty());
    }

    #[test]
    fn nested_list() {
        let ir = parse("- a\n  - b\n  - c\n- d");
        assert!(ir.text.contains("• a"));
        assert!(ir.text.contains("• b"));
        assert!(ir.text.contains("• d"));
    }
}
