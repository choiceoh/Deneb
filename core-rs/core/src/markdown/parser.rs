//! Markdown-to-IR parser using pulldown-cmark.
//!
//! Mirrors `markdownToIR` / `markdownToIRWithMeta` from `src/markdown/ir.ts`.
//! Converts markdown text into a `MarkdownIR` (plain text + style spans + links).

use super::spans::{
    clamp_link_spans, clamp_style_spans, merge_style_spans, LinkSpan, MarkdownIR, MarkdownStyle,
    StyleSpan,
};
use pulldown_cmark::{Event, Options, Parser, Tag, TagEnd};
use serde::{Deserialize, Serialize};

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum HeadingStyle {
    None,
    Bold,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum TableMode {
    Off,
    Bullets,
    Code,
}

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
// Internal state
// ---------------------------------------------------------------------------

#[derive(Debug, Clone)]
struct OpenStyle {
    style: MarkdownStyle,
    start: usize,
}

#[derive(Debug, Clone)]
struct LinkState {
    href: String,
    label_start: usize,
}

#[derive(Debug, Clone)]
struct ListEntry {
    ordered: bool,
    index: usize,
}

#[derive(Debug, Clone)]
struct TableCell {
    text: String,
    styles: Vec<StyleSpan>,
    links: Vec<LinkSpan>,
}

#[derive(Debug, Clone)]
struct RenderTarget {
    text: String,
    styles: Vec<StyleSpan>,
    open_styles: Vec<OpenStyle>,
    links: Vec<LinkSpan>,
    link_stack: Vec<LinkState>,
}

impl RenderTarget {
    fn new() -> Self {
        Self {
            text: String::new(),
            styles: Vec::new(),
            open_styles: Vec::new(),
            links: Vec::new(),
            link_stack: Vec::new(),
        }
    }
}

#[derive(Debug, Clone)]
struct TableState {
    headers: Vec<TableCell>,
    rows: Vec<Vec<TableCell>>,
    current_row: Vec<TableCell>,
    current_cell: Option<RenderTarget>,
    in_header: bool,
}

impl TableState {
    fn new() -> Self {
        Self {
            headers: Vec::new(),
            rows: Vec::new(),
            current_row: Vec::new(),
            current_cell: None,
            in_header: false,
        }
    }
}

struct RenderState {
    // Main render target
    text: String,
    styles: Vec<StyleSpan>,
    open_styles: Vec<OpenStyle>,
    links: Vec<LinkSpan>,
    link_stack: Vec<LinkState>,
    // Environment
    list_stack: Vec<ListEntry>,
    heading_style: HeadingStyle,
    blockquote_prefix: String,
    table_mode: TableMode,
    table: Option<TableState>,
    has_tables: bool,
}

impl RenderState {
    fn new(options: &ParseOptions) -> Self {
        Self {
            text: String::new(),
            styles: Vec::new(),
            open_styles: Vec::new(),
            links: Vec::new(),
            link_stack: Vec::new(),
            list_stack: Vec::new(),
            heading_style: options.heading_style,
            blockquote_prefix: options.blockquote_prefix.clone(),
            table_mode: options.table_mode,
            table: None,
            has_tables: false,
        }
    }

    /// Get the active text buffer (table cell or main).
    fn text_mut(&mut self) -> &mut String {
        if let Some(ref mut table) = self.table {
            if let Some(ref mut cell) = table.current_cell {
                return &mut cell.text;
            }
        }
        &mut self.text
    }

    fn styles_mut(&mut self) -> &mut Vec<StyleSpan> {
        if let Some(ref mut table) = self.table {
            if let Some(ref mut cell) = table.current_cell {
                return &mut cell.styles;
            }
        }
        &mut self.styles
    }

    fn open_styles_mut(&mut self) -> &mut Vec<OpenStyle> {
        if let Some(ref mut table) = self.table {
            if let Some(ref mut cell) = table.current_cell {
                return &mut cell.open_styles;
            }
        }
        &mut self.open_styles
    }

    fn links_mut(&mut self) -> &mut Vec<LinkSpan> {
        if let Some(ref mut table) = self.table {
            if let Some(ref mut cell) = table.current_cell {
                return &mut cell.links;
            }
        }
        &mut self.links
    }

    fn link_stack_mut(&mut self) -> &mut Vec<LinkState> {
        if let Some(ref mut table) = self.table {
            if let Some(ref mut cell) = table.current_cell {
                return &mut cell.link_stack;
            }
        }
        &mut self.link_stack
    }

    fn text_len(&self) -> usize {
        if let Some(ref table) = self.table {
            if let Some(ref cell) = table.current_cell {
                return cell.text.len();
            }
        }
        self.text.len()
    }

    fn append_text(&mut self, value: &str) {
        if value.is_empty() {
            return;
        }
        self.text_mut().push_str(value);
    }

    fn open_style(&mut self, style: MarkdownStyle) {
        let start = self.text_len();
        self.open_styles_mut().push(OpenStyle { style, start });
    }

    fn close_style(&mut self, style: MarkdownStyle) {
        let open_styles = self.open_styles_mut();
        for i in (0..open_styles.len()).rev() {
            if open_styles[i].style == style {
                let start = open_styles[i].start;
                open_styles.remove(i);
                let end = self.text_len();
                if end > start {
                    self.styles_mut().push(StyleSpan { start, end, style });
                }
                return;
            }
        }
    }

    fn append_paragraph_separator(&mut self) {
        if !self.list_stack.is_empty() {
            return;
        }
        if self.table.is_some() {
            return;
        }
        self.text.push_str("\n\n");
    }

    fn append_list_prefix(&mut self) {
        let depth = self.list_stack.len();
        if let Some(top) = self.list_stack.last_mut() {
            top.index += 1;
            let indent = "  ".repeat(depth.saturating_sub(1));
            let prefix = if top.ordered {
                format!("{}. ", top.index)
            } else {
                "• ".to_string()
            };
            self.text.push_str(&indent);
            self.text.push_str(&prefix);
        }
    }

    fn render_inline_code(&mut self, content: &str) {
        if content.is_empty() {
            return;
        }
        let start = self.text_len();
        self.text_mut().push_str(content);
        let end = self.text_len();
        self.styles_mut().push(StyleSpan {
            start,
            end,
            style: MarkdownStyle::Code,
        });
    }

    fn render_code_block(&mut self, content: &str) {
        let mut code = content.to_string();
        if !code.ends_with('\n') {
            code.push('\n');
        }
        let start = self.text_len();
        self.text_mut().push_str(&code);
        let end = self.text_len();
        self.styles_mut().push(StyleSpan {
            start,
            end,
            style: MarkdownStyle::CodeBlock,
        });
        if self.list_stack.is_empty() {
            self.text_mut().push('\n');
        }
    }

    fn handle_link_open(&mut self, href: String) {
        let label_start = self.text_len();
        self.link_stack_mut().push(LinkState { href, label_start });
    }

    fn handle_link_close(&mut self) {
        let link = match self.link_stack_mut().pop() {
            Some(l) => l,
            None => return,
        };
        let href = link.href.trim().to_string();
        if href.is_empty() {
            return;
        }
        let start = link.label_start;
        let end = self.text_len();
        self.links_mut().push(LinkSpan {
            start,
            end,
            href,
        });
    }

    fn close_remaining_styles(&mut self) {
        let end = self.text.len();
        for i in (0..self.open_styles.len()).rev() {
            let open = &self.open_styles[i];
            if end > open.start {
                self.styles.push(StyleSpan {
                    start: open.start,
                    end,
                    style: open.style,
                });
            }
        }
        self.open_styles.clear();
    }
}

// ---------------------------------------------------------------------------
// Spoiler preprocessing
// ---------------------------------------------------------------------------

/// Preprocess markdown text to convert `||text||` into placeholder markers
/// that pulldown-cmark won't strip. We use zero-width chars as sentinels.
const SPOILER_OPEN: &str = "\u{200B}\u{FEFF}SPOILER_OPEN\u{200B}";
const SPOILER_CLOSE: &str = "\u{200B}\u{FEFF}SPOILER_CLOSE\u{200B}";

fn preprocess_spoilers(text: &str) -> String {
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
            let ch = text[idx..].chars().next().unwrap();
            result.push(ch);
            idx += ch.len_utf8();
        }
    }

    result
}

// ---------------------------------------------------------------------------
// Table rendering helpers
// ---------------------------------------------------------------------------

fn trim_cell(cell: &TableCell) -> TableCell {
    let text = &cell.text;
    let start = text.find(|c: char| !c.is_whitespace()).unwrap_or(text.len());
    let end = text
        .rfind(|c: char| !c.is_whitespace())
        .map(|i| i + text[i..].chars().next().unwrap().len_utf8())
        .unwrap_or(0);

    if start == 0 && end == text.len() {
        return cell.clone();
    }

    let trimmed_text = &text[start..end];
    let trimmed_len = trimmed_text.len();

    let styles = cell
        .styles
        .iter()
        .filter_map(|span| {
            let s = span.start.saturating_sub(start).min(trimmed_len);
            let e = span.end.saturating_sub(start).min(trimmed_len);
            if e > s {
                Some(StyleSpan {
                    start: s,
                    end: e,
                    style: span.style,
                })
            } else {
                None
            }
        })
        .collect();

    let links = cell
        .links
        .iter()
        .filter_map(|span| {
            let s = span.start.saturating_sub(start).min(trimmed_len);
            let e = span.end.saturating_sub(start).min(trimmed_len);
            if e > s {
                Some(LinkSpan {
                    start: s,
                    end: e,
                    href: span.href.clone(),
                })
            } else {
                None
            }
        })
        .collect();

    TableCell {
        text: trimmed_text.to_string(),
        styles,
        links,
    }
}

fn append_cell_with_styles(state: &mut RenderState, cell: &TableCell) {
    if cell.text.is_empty() {
        return;
    }
    let base = state.text.len();
    state.text.push_str(&cell.text);
    for span in &cell.styles {
        state.styles.push(StyleSpan {
            start: base + span.start,
            end: base + span.end,
            style: span.style,
        });
    }
    for link in &cell.links {
        state.links.push(LinkSpan {
            start: base + link.start,
            end: base + link.end,
            href: link.href.clone(),
        });
    }
}

fn append_cell_text_only(state: &mut RenderState, cell: &TableCell) {
    if !cell.text.is_empty() {
        state.text.push_str(&cell.text);
    }
}

fn render_table_bullet_value(
    state: &mut RenderState,
    header: Option<&TableCell>,
    value: Option<&TableCell>,
    column_index: usize,
    include_column_fallback: bool,
) {
    let val = match value {
        Some(v) if !v.text.is_empty() => v,
        _ => return,
    };
    state.text.push_str("• ");
    if let Some(h) = header {
        if !h.text.is_empty() {
            append_cell_with_styles(state, h);
            state.text.push_str(": ");
        } else if include_column_fallback {
            state.text.push_str(&format!("Column {column_index}: "));
        }
    } else if include_column_fallback {
        state.text.push_str(&format!("Column {column_index}: "));
    }
    append_cell_with_styles(state, val);
    state.text.push('\n');
}

fn render_table_as_bullets(state: &mut RenderState) {
    let table = match state.table.take() {
        Some(t) => t,
        None => return,
    };

    let headers: Vec<TableCell> = table.headers.iter().map(trim_cell).collect();
    let rows: Vec<Vec<TableCell>> = table
        .rows
        .iter()
        .map(|row| row.iter().map(trim_cell).collect())
        .collect();

    if headers.is_empty() && rows.is_empty() {
        return;
    }

    let use_first_col_as_label = headers.len() > 1 && !rows.is_empty();

    if use_first_col_as_label {
        for row in &rows {
            if row.is_empty() {
                continue;
            }
            if let Some(row_label) = row.first() {
                if !row_label.text.is_empty() {
                    let label_start = state.text.len();
                    append_cell_with_styles(state, row_label);
                    let label_end = state.text.len();
                    if label_end > label_start {
                        state.styles.push(StyleSpan {
                            start: label_start,
                            end: label_end,
                            style: MarkdownStyle::Bold,
                        });
                    }
                    state.text.push('\n');
                }
            }
            for i in 1..row.len() {
                render_table_bullet_value(state, headers.get(i), row.get(i), i, true);
            }
            state.text.push('\n');
        }
    } else {
        for row in &rows {
            for (i, cell) in row.iter().enumerate() {
                render_table_bullet_value(state, headers.get(i), Some(cell), i, false);
            }
            state.text.push('\n');
        }
    }
}

fn render_table_as_code(state: &mut RenderState) {
    let table = match state.table.take() {
        Some(t) => t,
        None => return,
    };

    let headers: Vec<TableCell> = table.headers.iter().map(trim_cell).collect();
    let rows: Vec<Vec<TableCell>> = table
        .rows
        .iter()
        .map(|row| row.iter().map(trim_cell).collect())
        .collect();

    let column_count = headers
        .len()
        .max(rows.iter().map(|r| r.len()).max().unwrap_or(0));
    if column_count == 0 {
        return;
    }

    let mut widths = vec![0usize; column_count];
    let update_widths = |widths: &mut Vec<usize>, cells: &[TableCell]| {
        for (i, w) in widths.iter_mut().enumerate().take(column_count) {
            let cell_width = cells.get(i).map(|c| c.text.len()).unwrap_or(0);
            if cell_width > *w {
                *w = cell_width;
            }
        }
    };
    update_widths(&mut widths, &headers);
    for row in &rows {
        update_widths(&mut widths, row);
    }

    let code_start = state.text.len();

    let append_row = |state: &mut RenderState, cells: &[TableCell], widths: &[usize]| {
        state.text.push('|');
        for i in 0..column_count {
            state.text.push(' ');
            if let Some(cell) = cells.get(i) {
                append_cell_text_only(state, cell);
            }
            let cell_len = cells.get(i).map(|c| c.text.len()).unwrap_or(0);
            let pad = widths[i].saturating_sub(cell_len);
            if pad > 0 {
                state.text.extend(std::iter::repeat_n(' ', pad));
            }
            state.text.push_str(" |");
        }
        state.text.push('\n');
    };

    let append_divider = |state: &mut RenderState, widths: &[usize]| {
        state.text.push('|');
        for w in widths.iter().take(column_count) {
            let dash_count = (*w).max(3);
            state.text.push(' ');
            state.text.extend(std::iter::repeat_n('-', dash_count));
            state.text.push_str(" |");
        }
        state.text.push('\n');
    };

    append_row(state, &headers, &widths);
    append_divider(state, &widths);
    for row in &rows {
        append_row(state, row, &widths);
    }

    let code_end = state.text.len();
    if code_end > code_start {
        state.styles.push(StyleSpan {
            start: code_start,
            end: code_end,
            style: MarkdownStyle::CodeBlock,
        });
    }
    if state.list_stack.is_empty() {
        state.text.push('\n');
    }
}

fn finish_table_cell(target: &mut RenderTarget) -> TableCell {
    // Close remaining open styles in the cell
    let end = target.text.len();
    for i in (0..target.open_styles.len()).rev() {
        let open = &target.open_styles[i];
        if end > open.start {
            target.styles.push(StyleSpan {
                start: open.start,
                end,
                style: open.style,
            });
        }
    }
    target.open_styles.clear();

    TableCell {
        text: std::mem::take(&mut target.text),
        styles: std::mem::take(&mut target.styles),
        links: std::mem::take(&mut target.links),
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
    let mut state = RenderState::new(options);

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
                            TableMode::Bullets => render_table_as_bullets(&mut state),
                            TableMode::Code => render_table_as_code(&mut state),
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
                            let finished = finish_table_cell(cell);
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

/// Handle text that contains spoiler sentinel markers.
fn handle_spoiler_text(state: &mut RenderState, text: &str) {
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
    fn bold_text() {
        let ir = parse("**bold**");
        assert_eq!(ir.text, "bold");
        assert_eq!(ir.styles.len(), 1);
        assert_eq!(ir.styles[0].style, MarkdownStyle::Bold);
        assert_eq!(ir.styles[0].start, 0);
        assert_eq!(ir.styles[0].end, 4);
    }

    #[test]
    fn italic_text() {
        let ir = parse("*italic*");
        assert_eq!(ir.text, "italic");
        assert_eq!(ir.styles.len(), 1);
        assert_eq!(ir.styles[0].style, MarkdownStyle::Italic);
    }

    #[test]
    fn strikethrough_text() {
        let ir = parse("~~strike~~");
        assert_eq!(ir.text, "strike");
        assert_eq!(ir.styles.len(), 1);
        assert_eq!(ir.styles[0].style, MarkdownStyle::Strikethrough);
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
        assert!(ir.styles.iter().any(|s| s.style == MarkdownStyle::CodeBlock));
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
        assert!(ir
            .styles
            .iter()
            .any(|s| s.style == MarkdownStyle::Spoiler));
    }

    #[test]
    fn nested_styles() {
        let ir = parse("**bold *and italic***");
        assert!(ir
            .styles
            .iter()
            .any(|s| s.style == MarkdownStyle::Bold));
        assert!(ir
            .styles
            .iter()
            .any(|s| s.style == MarkdownStyle::Italic));
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
        assert!(ir.text.contains("Value: 1"), "expected 'Value: 1' in {:?}", ir.text);
        assert!(ir.text.contains("Value: 2"), "expected 'Value: 2' in {:?}", ir.text);
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
