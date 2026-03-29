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
            let text = self.cell_buf.trim().replace('|', "\\|");
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
// Emitter
// ---------------------------------------------------------------------------

/// Emit Markdown from a token stream. Returns `(text, title)`.
pub(crate) fn emit(tokens: &[Token<'_>], input_len: usize) -> (String, Option<String>) {
    let mut out = String::with_capacity(input_len);
    let mut title: Option<String> = None;

    // State tracking
    let mut suppress_depth: usize = 0; // inside <script>/<style>/<noscript>
    let mut list_stack: Vec<ListCtx> = Vec::new();
    let mut in_pre = false;
    let mut in_code_in_pre = false; // <code> inside <pre>

    // Buffering for compound elements
    let mut in_title = false;
    let mut title_buf = String::new();
    let mut in_link = false;
    let mut link_href: Option<String> = None;
    let mut link_buf = String::new();
    let mut in_blockquote = false;
    let mut blockquote_buf = String::new();
    let mut in_table = false;
    let mut table_builder = TableBuilder::default();

    for token in tokens {
        // --- Suppressed content (script/style/noscript) ---
        if suppress_depth > 0 {
            if let Token::TagClose(name) = token {
                if matches!(name, TagName::Script | TagName::Style | TagName::Noscript) {
                    suppress_depth = suppress_depth.saturating_sub(1);
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
                suppress_depth += 1;
                // Also handle the case where the tokenizer already consumed
                // inner content and pushed TagClose — suppress_depth will be
                // decremented when we see it.
                let _ = name;
            }

            // --- Title extraction ---
            Token::TagOpen {
                name: TagName::Title,
                ..
            } => {
                in_title = true;
                title_buf.clear();
            }
            Token::TagClose(TagName::Title) => {
                in_title = false;
                let t = normalize_inline(&title_buf);
                if !t.is_empty() {
                    title = Some(t);
                }
            }

            // --- Links ---
            Token::TagOpen {
                name: TagName::A,
                raw,
            } => {
                in_link = true;
                link_href = extract_attr(raw, "href");
                link_buf.clear();
            }
            Token::TagClose(TagName::A) => {
                if in_link {
                    let label = normalize_inline(&link_buf);
                    let target = if in_blockquote {
                        &mut blockquote_buf
                    } else if in_table {
                        // Table cell handles differently
                        if table_builder.in_cell {
                            &mut table_builder.cell_buf
                        } else {
                            &mut out
                        }
                    } else {
                        &mut out
                    };
                    if let Some(ref h) = link_href {
                        if label.is_empty() {
                            target.push_str(h);
                        } else {
                            target.push('[');
                            target.push_str(&label);
                            target.push_str("](");
                            target.push_str(h);
                            target.push(')');
                        }
                    } else {
                        target.push_str(&label);
                    }
                    in_link = false;
                    link_href = None;
                }
            }

            // --- Emphasis (bold) ---
            Token::TagOpen {
                name: TagName::Strong | TagName::B,
                ..
            } => push_to_active(
                "**", &mut out, in_link, &mut link_buf, in_blockquote,
                &mut blockquote_buf, in_table, &mut table_builder, in_title, &mut title_buf,
            ),
            Token::TagClose(TagName::Strong | TagName::B) => push_to_active(
                "**", &mut out, in_link, &mut link_buf, in_blockquote,
                &mut blockquote_buf, in_table, &mut table_builder, in_title, &mut title_buf,
            ),

            // --- Emphasis (italic) ---
            Token::TagOpen {
                name: TagName::Em | TagName::I,
                ..
            } => push_to_active(
                "*", &mut out, in_link, &mut link_buf, in_blockquote,
                &mut blockquote_buf, in_table, &mut table_builder, in_title, &mut title_buf,
            ),
            Token::TagClose(TagName::Em | TagName::I) => push_to_active(
                "*", &mut out, in_link, &mut link_buf, in_blockquote,
                &mut blockquote_buf, in_table, &mut table_builder, in_title, &mut title_buf,
            ),

            // --- Strikethrough ---
            Token::TagOpen {
                name: TagName::S | TagName::Del | TagName::Strike,
                ..
            } => push_to_active(
                "~~", &mut out, in_link, &mut link_buf, in_blockquote,
                &mut blockquote_buf, in_table, &mut table_builder, in_title, &mut title_buf,
            ),
            Token::TagClose(TagName::S | TagName::Del | TagName::Strike) => push_to_active(
                "~~", &mut out, in_link, &mut link_buf, in_blockquote,
                &mut blockquote_buf, in_table, &mut table_builder, in_title, &mut title_buf,
            ),

            // --- Pre blocks ---
            Token::TagOpen {
                name: TagName::Pre, ..
            } => {
                in_pre = true;
            }
            Token::TagClose(TagName::Pre) => {
                // If code-in-pre wasn't started (bare <pre> without <code>),
                // close the fenced block.
                if in_pre && !in_code_in_pre {
                    out.push_str("\n```\n");
                }
                in_pre = false;
                in_code_in_pre = false;
            }

            // --- Code ---
            Token::TagOpen {
                name: TagName::Code,
                raw,
            } => {
                if in_pre {
                    in_code_in_pre = true;
                    let lang = extract_code_language(raw);
                    out.push_str("\n```");
                    out.push_str(&lang);
                    out.push('\n');
                } else {
                    push_to_active(
                        "`", &mut out, in_link, &mut link_buf, in_blockquote,
                        &mut blockquote_buf, in_table, &mut table_builder, in_title,
                        &mut title_buf,
                    );
                }
            }
            Token::TagClose(TagName::Code) => {
                if in_code_in_pre {
                    out.push_str("\n```\n");
                    in_code_in_pre = false;
                } else {
                    push_to_active(
                        "`", &mut out, in_link, &mut link_buf, in_blockquote,
                        &mut blockquote_buf, in_table, &mut table_builder, in_title,
                        &mut title_buf,
                    );
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
                out.push('\n');
                out.push_str(&prefix);
                out.push(' ');
            }
            Token::TagClose(
                TagName::H1
                | TagName::H2
                | TagName::H3
                | TagName::H4
                | TagName::H5
                | TagName::H6,
            ) => {
                out.push('\n');
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
                    let target = if in_blockquote {
                        &mut blockquote_buf
                    } else if in_table && table_builder.in_cell {
                        &mut table_builder.cell_buf
                    } else {
                        &mut out
                    };
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
                in_blockquote = true;
                blockquote_buf.clear();
            }
            Token::TagClose(TagName::Blockquote) => {
                if in_blockquote {
                    let text = normalize_inline(&blockquote_buf);
                    if !text.is_empty() {
                        out.push('\n');
                        for line in text.lines() {
                            out.push_str("> ");
                            out.push_str(line);
                            out.push('\n');
                        }
                    }
                    in_blockquote = false;
                }
            }

            // --- Tables ---
            Token::TagOpen {
                name: TagName::Table,
                ..
            } => {
                in_table = true;
                table_builder = TableBuilder::default();
            }
            Token::TagClose(TagName::Table) => {
                if in_table {
                    let md = table_builder.to_markdown();
                    if !md.is_empty() {
                        out.push('\n');
                        out.push_str(&md);
                    }
                    in_table = false;
                }
            }
            Token::TagOpen {
                name: TagName::Tr, ..
            } => {
                if in_table {
                    table_builder.start_row();
                }
            }
            Token::TagClose(TagName::Tr) => {
                if in_table {
                    table_builder.end_cell(); // close any open cell
                    table_builder.end_row();
                }
            }
            Token::TagOpen {
                name: TagName::Th, ..
            } => {
                if in_table {
                    table_builder.end_cell(); // close previous cell if any
                    table_builder.start_cell(true);
                }
            }
            Token::TagOpen {
                name: TagName::Td, ..
            } => {
                if in_table {
                    table_builder.end_cell();
                    table_builder.start_cell(false);
                }
            }
            Token::TagClose(TagName::Th | TagName::Td) => {
                if in_table {
                    table_builder.end_cell();
                }
            }

            // --- Lists ---
            Token::TagOpen {
                name: TagName::Ol, ..
            } => {
                list_stack.push(ListCtx::Ordered(0));
            }
            Token::TagOpen {
                name: TagName::Ul, ..
            } => {
                list_stack.push(ListCtx::Unordered);
            }
            Token::TagClose(TagName::Ol | TagName::Ul) => {
                list_stack.pop();
            }
            Token::TagOpen {
                name: TagName::Li, ..
            } => {
                let prefix = match list_stack.last_mut() {
                    Some(ListCtx::Ordered(counter)) => {
                        *counter += 1;
                        format!("\n{counter}. ")
                    }
                    Some(ListCtx::Unordered) | None => "\n- ".to_string(),
                };
                out.push_str(&prefix);
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
                out.push('\n');
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
                out.push('\n');
            }

            // --- Text content ---
            Token::Text(s) => {
                if in_title {
                    title_buf.push_str(s);
                } else if in_link {
                    link_buf.push_str(s);
                } else if in_blockquote {
                    blockquote_buf.push_str(s);
                } else if in_table {
                    table_builder.push_text(s);
                } else {
                    out.push_str(s);
                }
            }
            Token::Entity(ch) => {
                if in_title {
                    title_buf.push(*ch);
                } else if in_link {
                    link_buf.push(*ch);
                } else if in_blockquote {
                    blockquote_buf.push(*ch);
                } else if in_table {
                    table_builder.push_char(*ch);
                } else {
                    out.push(*ch);
                }
            }
            Token::AmpersandLiteral => {
                if in_title {
                    title_buf.push('&');
                } else if in_link {
                    link_buf.push('&');
                } else if in_blockquote {
                    blockquote_buf.push('&');
                } else if in_table {
                    table_builder.push_char('&');
                } else {
                    out.push('&');
                }
            }

            // --- All other tags: skip (strip) ---
            Token::TagOpen {
                name: TagName::Other,
                ..
            }
            | Token::TagClose(TagName::Other)
            | Token::SelfClosing {
                name: TagName::Other,
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

    (out, title)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Push a string to whichever buffer is currently active.
#[allow(clippy::too_many_arguments)]
fn push_to_active(
    s: &str,
    out: &mut String,
    in_link: bool,
    link_buf: &mut String,
    in_blockquote: bool,
    blockquote_buf: &mut String,
    in_table: bool,
    table_builder: &mut TableBuilder,
    in_title: bool,
    title_buf: &mut String,
) {
    if in_title {
        title_buf.push_str(s);
    } else if in_link {
        link_buf.push_str(s);
    } else if in_blockquote {
        blockquote_buf.push_str(s);
    } else if in_table && table_builder.in_cell {
        table_builder.cell_buf.push_str(s);
    } else {
        out.push_str(s);
    }
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
