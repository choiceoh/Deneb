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

#[cfg(test)]
mod tests;
