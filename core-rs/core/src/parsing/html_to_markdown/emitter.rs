//! Markdown emitter — walks a token stream and produces Markdown output.
//!
//! Handles all HTML tag conversions in a single pass over the token
//! vector, tracking state (list context, suppression, pre-blocks, etc.)
//! to produce correct Markdown.

use super::attrs::{extract_attr, extract_code_language, filename_from_url};
use super::tokenizer::{TagName, Token};

// ---------------------------------------------------------------------------
// List context tracking
// ---------------------------------------------------------------------------

#[derive(Debug)]
enum ListCtx {
    Ordered(usize),
    Unordered,
}

// ---------------------------------------------------------------------------
// Table building
// ---------------------------------------------------------------------------

#[derive(Debug, Default)]
struct TableBuilder {
    rows: Vec<(Vec<String>, bool)>, // (cells, is_header_row)
    current_cells: Vec<String>,
    current_has_th: bool,
    in_cell: bool,
    cell_buf: String,
    cell_is_th: bool,
}

impl TableBuilder {
    fn start_row(&mut self) {
        self.current_cells.clear();
        self.current_has_th = false;
    }

    fn end_row(&mut self) {
        if !self.current_cells.is_empty() {
            self.rows
                .push((self.current_cells.clone(), self.current_has_th));
        }
        self.current_cells.clear();
    }

    fn start_cell(&mut self, is_th: bool) {
        self.in_cell = true;
        self.cell_buf.clear();
        self.cell_is_th = is_th;
        if is_th {
            self.current_has_th = true;
        }
    }

    fn end_cell(&mut self) {
        if self.in_cell {
            let text = escape_table_cell(self.cell_buf.trim());
            self.current_cells.push(text);
            self.cell_buf.clear();
            self.in_cell = false;
        }
    }

    fn push_text(&mut self, s: &str) {
        if self.in_cell {
            self.cell_buf.push_str(s);
        }
    }

    fn push_char(&mut self, ch: char) {
        if self.in_cell {
            self.cell_buf.push(ch);
        }
    }

    fn to_markdown(&self) -> String {
        if self.rows.is_empty() {
            return String::new();
        }

        let mut md = String::new();
        let mut separator_added = false;

        for (i, (cells, is_header)) in self.rows.iter().enumerate() {
            md.push_str("| ");
            md.push_str(&cells.join(" | "));
            md.push_str(" |\n");

            if !separator_added && (*is_header || i == 0) {
                md.push('|');
                for _ in 0..cells.len() {
                    md.push_str(" --- |");
                }
                md.push('\n');
                separator_added = true;
            }
        }

        md
    }
}

// ---------------------------------------------------------------------------
// EmitCtx — centralized output routing for nested HTML contexts
// ---------------------------------------------------------------------------

/// Tracks which nested HTML context is active and routes text output
/// to the correct buffer (main output, link, blockquote, table cell, or title).
struct EmitCtx {
    out: String,
    title: Option<String>,

    // Suppression (script/style/noscript)
    suppress_depth: usize,

    // Block state
    list_stack: Vec<ListCtx>,
    in_pre: bool,
    in_code_in_pre: bool,

    // Compound element buffers
    in_title: bool,
    title_buf: String,
    in_link: bool,
    link_href: Option<String>,
    link_buf: String,
    in_blockquote: bool,
    blockquote_buf: String,
    in_table: bool,
    table_builder: TableBuilder,
}

impl EmitCtx {
    fn new(capacity: usize) -> Self {
        Self {
            out: String::with_capacity(capacity),
            title: None,
            suppress_depth: 0,
            list_stack: Vec::new(),
            in_pre: false,
            in_code_in_pre: false,
            in_title: false,
            title_buf: String::new(),
            in_link: false,
            link_href: None,
            link_buf: String::new(),
            in_blockquote: false,
            blockquote_buf: String::new(),
            in_table: false,
            table_builder: TableBuilder::default(),
        }
    }

    /// Push a string to whichever buffer is currently active.
    fn push(&mut self, s: &str) {
        if self.in_title {
            self.title_buf.push_str(s);
        } else if self.in_link {
            self.link_buf.push_str(s);
        } else if self.in_blockquote {
            self.blockquote_buf.push_str(s);
        } else if self.in_table && self.table_builder.in_cell {
            self.table_builder.cell_buf.push_str(s);
        } else {
            self.out.push_str(s);
        }
    }

    /// Push a single character to the active buffer.
    fn push_char(&mut self, ch: char) {
        if self.in_title {
            self.title_buf.push(ch);
        } else if self.in_link {
            self.link_buf.push(ch);
        } else if self.in_blockquote {
            self.blockquote_buf.push(ch);
        } else if self.in_table {
            self.table_builder.push_char(ch);
        } else {
            self.out.push(ch);
        }
    }

    /// Get the active output buffer for compound element emission (link close, image).
    fn active_buf(&mut self) -> &mut String {
        if self.in_blockquote {
            &mut self.blockquote_buf
        } else if self.in_table && self.table_builder.in_cell {
            &mut self.table_builder.cell_buf
        } else {
            &mut self.out
        }
    }
}

// ---------------------------------------------------------------------------
// Emitter
// ---------------------------------------------------------------------------

/// Emit Markdown from a token stream. Returns `(text, title)`.
/// When `strip_noise` is true, nav/aside/svg/iframe/form content is suppressed.
pub(crate) fn emit(
    tokens: &[Token<'_>],
    input_len: usize,
    strip_noise: bool,
) -> (String, Option<String>) {
    let mut ctx = EmitCtx::new(input_len);

    for token in tokens {
        // --- Suppressed content (script/style/noscript + noise tags) ---
        if ctx.suppress_depth > 0 {
            if let Token::TagClose(name) = token {
                let is_always_suppressed =
                    matches!(name, TagName::Script | TagName::Style | TagName::Noscript);
                let is_noise_suppressed = strip_noise
                    && matches!(
                        name,
                        TagName::Nav
                            | TagName::Aside
                            | TagName::Svg
                            | TagName::Iframe
                            | TagName::Form
                    );
                if is_always_suppressed || is_noise_suppressed {
                    ctx.suppress_depth = ctx.suppress_depth.saturating_sub(1);
                }
            }
            continue;
        }

        match token {
            // --- Suppression start ---
            Token::TagOpen {
                name: name @ (TagName::Script | TagName::Style | TagName::Noscript),
                ..
            } => {
                ctx.suppress_depth += 1;
                let _ = name;
            }

            // --- Noise suppression (when strip_noise enabled) ---
            Token::TagOpen {
                name:
                    TagName::Nav | TagName::Aside | TagName::Svg | TagName::Iframe | TagName::Form,
                ..
            } if strip_noise => {
                ctx.suppress_depth += 1;
            }

            // --- Title extraction ---
            Token::TagOpen {
                name: TagName::Title,
                ..
            } => {
                ctx.in_title = true;
                ctx.title_buf.clear();
            }
            Token::TagClose(TagName::Title) => {
                ctx.in_title = false;
                let t = normalize_inline(&ctx.title_buf);
                if !t.is_empty() {
                    ctx.title = Some(t);
                }
            }

            // --- Links ---
            Token::TagOpen {
                name: TagName::A,
                raw,
            } => {
                ctx.in_link = true;
                ctx.link_href = extract_attr(raw, "href");
                ctx.link_buf.clear();
            }
            Token::TagClose(TagName::A) => {
                if ctx.in_link {
                    let label = normalize_inline(&ctx.link_buf);
                    // Take href before borrowing ctx.active_buf() to satisfy borrow checker.
                    let href = ctx.link_href.take();
                    let target = ctx.active_buf();
                    if let Some(h) = href {
                        if label.is_empty() {
                            target.push_str(&h);
                        } else {
                            target.push('[');
                            target.push_str(&label);
                            target.push_str("](");
                            target.push_str(&h);
                            target.push(')');
                        }
                    } else {
                        target.push_str(&label);
                    }
                    ctx.in_link = false;
                }
            }

            // --- Emphasis (bold) ---
            Token::TagOpen {
                name: TagName::Strong | TagName::B,
                ..
            }
            | Token::TagClose(TagName::Strong | TagName::B) => ctx.push("**"),

            // --- Emphasis (italic) ---
            Token::TagOpen {
                name: TagName::Em | TagName::I,
                ..
            }
            | Token::TagClose(TagName::Em | TagName::I) => ctx.push("*"),

            // --- Strikethrough ---
            Token::TagOpen {
                name: TagName::S | TagName::Del | TagName::Strike,
                ..
            }
            | Token::TagClose(TagName::S | TagName::Del | TagName::Strike) => ctx.push("~~"),

            // --- Pre blocks ---
            Token::TagOpen {
                name: TagName::Pre, ..
            } => {
                ctx.in_pre = true;
            }
            Token::TagClose(TagName::Pre) => {
                // If code-in-pre wasn't started (bare <pre> without <code>),
                // close the fenced block.
                if ctx.in_pre && !ctx.in_code_in_pre {
                    ctx.out.push_str("\n```\n");
                }
                ctx.in_pre = false;
                ctx.in_code_in_pre = false;
            }

            // --- Code ---
            Token::TagOpen {
                name: TagName::Code,
                raw,
            } => {
                if ctx.in_pre {
                    ctx.in_code_in_pre = true;
                    let lang = extract_code_language(raw);
                    ctx.out.push_str("\n```");
                    ctx.out.push_str(&lang);
                    ctx.out.push('\n');
                } else {
                    ctx.push("`");
                }
            }
            Token::TagClose(TagName::Code) => {
                if ctx.in_code_in_pre {
                    ctx.out.push_str("\n```\n");
                    ctx.in_code_in_pre = false;
                } else {
                    ctx.push("`");
                }
            }

            // --- Headings ---
            Token::TagOpen {
                name:
                    name @ (TagName::H1
                    | TagName::H2
                    | TagName::H3
                    | TagName::H4
                    | TagName::H5
                    | TagName::H6),
                ..
            } => {
                let level = heading_level(*name);
                let prefix = "#".repeat(level);
                ctx.out.push('\n');
                ctx.out.push_str(&prefix);
                ctx.out.push(' ');
            }
            Token::TagClose(
                TagName::H1 | TagName::H2 | TagName::H3 | TagName::H4 | TagName::H5 | TagName::H6,
            ) => {
                ctx.out.push('\n');
            }

            // --- Images ---
            Token::SelfClosing {
                name: TagName::Img,
                raw,
            }
            | Token::TagOpen {
                name: TagName::Img,
                raw,
            } => {
                let src = extract_attr(raw, "src");
                let alt = extract_attr(raw, "alt");
                if let Some(ref src) = src {
                    let label = alt
                        .filter(|a| !a.is_empty())
                        .unwrap_or_else(|| filename_from_url(src));
                    let target = ctx.active_buf();
                    target.push('[');
                    target.push_str(&label);
                    target.push_str("](");
                    target.push_str(src);
                    target.push(')');
                }
            }

            // --- Blockquotes ---
            Token::TagOpen {
                name: TagName::Blockquote,
                ..
            } => {
                ctx.in_blockquote = true;
                ctx.blockquote_buf.clear();
            }
            Token::TagClose(TagName::Blockquote) => {
                if ctx.in_blockquote {
                    let text = normalize_inline(&ctx.blockquote_buf);
                    if !text.is_empty() {
                        ctx.out.push('\n');
                        for line in text.lines() {
                            ctx.out.push_str("> ");
                            ctx.out.push_str(line);
                            ctx.out.push('\n');
                        }
                    }
                    ctx.in_blockquote = false;
                }
            }

            // --- Tables ---
            Token::TagOpen {
                name: TagName::Table,
                ..
            } => {
                ctx.in_table = true;
                ctx.table_builder = TableBuilder::default();
            }
            Token::TagClose(TagName::Table) => {
                if ctx.in_table {
                    let md = ctx.table_builder.to_markdown();
                    if !md.is_empty() {
                        ctx.out.push('\n');
                        ctx.out.push_str(&md);
                    }
                    ctx.in_table = false;
                }
            }
            Token::TagOpen {
                name: TagName::Tr, ..
            } => {
                if ctx.in_table {
                    ctx.table_builder.start_row();
                }
            }
            Token::TagClose(TagName::Tr) => {
                if ctx.in_table {
                    ctx.table_builder.end_cell();
                    ctx.table_builder.end_row();
                }
            }
            Token::TagOpen {
                name: TagName::Th, ..
            } => {
                if ctx.in_table {
                    ctx.table_builder.end_cell();
                    ctx.table_builder.start_cell(true);
                }
            }
            Token::TagOpen {
                name: TagName::Td, ..
            } => {
                if ctx.in_table {
                    ctx.table_builder.end_cell();
                    ctx.table_builder.start_cell(false);
                }
            }
            Token::TagClose(TagName::Th | TagName::Td) => {
                if ctx.in_table {
                    ctx.table_builder.end_cell();
                }
            }

            // --- Lists ---
            Token::TagOpen {
                name: TagName::Ol, ..
            } => {
                ctx.list_stack.push(ListCtx::Ordered(0));
            }
            Token::TagOpen {
                name: TagName::Ul, ..
            } => {
                ctx.list_stack.push(ListCtx::Unordered);
            }
            Token::TagClose(TagName::Ol | TagName::Ul) => {
                ctx.list_stack.pop();
            }
            Token::TagOpen {
                name: TagName::Li, ..
            } => {
                let prefix = match ctx.list_stack.last_mut() {
                    Some(ListCtx::Ordered(counter)) => {
                        *counter += 1;
                        format!("\n{counter}. ")
                    }
                    Some(ListCtx::Unordered) | None => "\n- ".to_string(),
                };
                ctx.out.push_str(&prefix);
            }
            Token::TagClose(TagName::Li) => {
                // No action needed — content already emitted.
            }

            // --- Line breaks ---
            Token::SelfClosing {
                name: TagName::Br | TagName::Hr,
                ..
            }
            | Token::TagOpen {
                name: TagName::Br | TagName::Hr,
                ..
            } => {
                ctx.out.push('\n');
            }

            // --- Block-closing tags → newline ---
            Token::TagClose(
                TagName::P
                | TagName::Div
                | TagName::Section
                | TagName::Article
                | TagName::Header
                | TagName::Footer,
            ) => {
                ctx.out.push('\n');
            }

            // --- Text content ---
            Token::Text(s) => {
                if ctx.in_title {
                    ctx.title_buf.push_str(s);
                } else if ctx.in_link {
                    ctx.link_buf.push_str(s);
                } else if ctx.in_blockquote {
                    ctx.blockquote_buf.push_str(s);
                } else if ctx.in_table {
                    ctx.table_builder.push_text(s);
                } else {
                    ctx.out.push_str(s);
                }
            }
            Token::Entity(ch) => ctx.push_char(*ch),
            Token::AmpersandLiteral => ctx.push_char('&'),

            // --- All other tags: skip (strip) ---
            // Includes TagName::Other and noise tags (Nav, Aside, Svg, Iframe, Form)
            // when strip_noise is disabled — their content flows through as text.
            Token::TagOpen {
                name:
                    TagName::Other
                    | TagName::Nav
                    | TagName::Aside
                    | TagName::Svg
                    | TagName::Iframe
                    | TagName::Form,
                ..
            }
            | Token::TagClose(
                TagName::Other
                | TagName::Nav
                | TagName::Aside
                | TagName::Svg
                | TagName::Iframe
                | TagName::Form,
            )
            | Token::SelfClosing {
                name:
                    TagName::Other
                    | TagName::Nav
                    | TagName::Aside
                    | TagName::Svg
                    | TagName::Iframe
                    | TagName::Form,
                ..
            } => {}

            // Opening tags for block elements that just emit a newline.
            Token::TagOpen {
                name:
                    TagName::P
                    | TagName::Div
                    | TagName::Section
                    | TagName::Article
                    | TagName::Header
                    | TagName::Footer,
                ..
            } => {
                // Opening block tags don't need special output — the closing
                // tag will emit the newline separator.
            }

            // Catch-all for any remaining unhandled close tags.
            Token::TagClose(_) | Token::SelfClosing { .. } => {}
        }
    }

    (ctx.out, ctx.title)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Escape markdown-special characters inside a table cell.
/// Pipes, backslashes, and inline formatting chars are escaped so the
/// Markdown table structure is preserved.
fn escape_table_cell(text: &str) -> String {
    let mut out = String::with_capacity(text.len() + 4);
    for ch in text.chars() {
        match ch {
            '|' | '\\' => {
                out.push('\\');
                out.push(ch);
            }
            _ => out.push(ch),
        }
    }
    out
}

fn heading_level(name: TagName) -> usize {
    match name {
        TagName::H1 => 1,
        TagName::H2 => 2,
        TagName::H3 => 3,
        TagName::H4 => 4,
        TagName::H5 => 5,
        TagName::H6 => 6,
        _ => 1,
    }
}

/// Simple inline normalization: collapse whitespace, trim.
fn normalize_inline(s: &str) -> String {
    let mut result = String::with_capacity(s.len());
    let mut prev_space = false;
    for ch in s.chars() {
        if ch == '\r' {
            continue;
        }
        if ch.is_ascii_whitespace() {
            if !prev_space && !result.is_empty() {
                result.push(' ');
            }
            prev_space = true;
        } else {
            prev_space = false;
            result.push(ch);
        }
    }
    // Trim trailing space.
    if result.ends_with(' ') {
        result.pop();
    }
    result
}
