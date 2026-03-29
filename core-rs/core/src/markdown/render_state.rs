//! Internal render state types for the markdown-to-IR parser.
//!
//! Contains the `RenderState` struct and all supporting types used during
//! event-driven parsing. Shared between the main `parser` module and the
//! sibling `tables` module.
//!
//! The `HeadingStyle` and `TableMode` enums live here because `RenderState`
//! needs them directly; they are re-exported from `parser` for external use.

use super::spans::{LinkSpan, MarkdownStyle, StyleSpan};
use serde::{Deserialize, Serialize};

// ---------------------------------------------------------------------------
// Heading and table configuration enums
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

// ---------------------------------------------------------------------------
// Internal state types (pub(crate) for tables module access)
// ---------------------------------------------------------------------------

#[derive(Debug, Clone)]
pub(crate) struct OpenStyle {
    pub(crate) style: MarkdownStyle,
    pub(crate) start: usize,
}

#[derive(Debug, Clone)]
struct LinkState {
    href: String,
    label_start: usize,
}

#[derive(Debug, Clone)]
pub(crate) struct ListEntry {
    pub(crate) ordered: bool,
    pub(crate) index: usize,
}

#[derive(Debug, Clone)]
pub(crate) struct TableCell {
    pub(crate) text: String,
    pub(crate) styles: Vec<StyleSpan>,
    pub(crate) links: Vec<LinkSpan>,
}

#[derive(Debug, Clone)]
pub(crate) struct RenderTarget {
    pub(crate) text: String,
    pub(crate) styles: Vec<StyleSpan>,
    pub(crate) open_styles: Vec<OpenStyle>,
    pub(crate) links: Vec<LinkSpan>,
    // Private: only accessed via RenderState::link_stack_mut().
    link_stack: Vec<LinkState>,
}

impl RenderTarget {
    pub(crate) fn new() -> Self {
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
pub(crate) struct TableState {
    pub(crate) headers: Vec<TableCell>,
    pub(crate) rows: Vec<Vec<TableCell>>,
    pub(crate) current_row: Vec<TableCell>,
    pub(crate) current_cell: Option<RenderTarget>,
    pub(crate) in_header: bool,
}

impl TableState {
    pub(crate) fn new() -> Self {
        Self {
            headers: Vec::new(),
            rows: Vec::new(),
            current_row: Vec::new(),
            current_cell: None,
            in_header: false,
        }
    }
}

pub(crate) struct RenderState {
    // Main render target
    pub(crate) text: String,
    pub(crate) styles: Vec<StyleSpan>,
    pub(crate) open_styles: Vec<OpenStyle>,
    pub(crate) links: Vec<LinkSpan>,
    link_stack: Vec<LinkState>,
    // Environment
    pub(crate) list_stack: Vec<ListEntry>,
    pub(crate) heading_style: HeadingStyle,
    pub(crate) blockquote_prefix: String,
    pub(crate) table_mode: TableMode,
    pub(crate) table: Option<TableState>,
    pub(crate) has_tables: bool,
}

impl RenderState {
    pub(crate) fn new(
        heading_style: HeadingStyle,
        blockquote_prefix: String,
        table_mode: TableMode,
    ) -> Self {
        Self {
            text: String::new(),
            styles: Vec::new(),
            open_styles: Vec::new(),
            links: Vec::new(),
            link_stack: Vec::new(),
            list_stack: Vec::new(),
            heading_style,
            blockquote_prefix,
            table_mode,
            table: None,
            has_tables: false,
        }
    }

    /// Get the active text buffer (table cell or main).
    pub(crate) fn text_mut(&mut self) -> &mut String {
        if let Some(ref mut table) = self.table {
            if let Some(ref mut cell) = table.current_cell {
                return &mut cell.text;
            }
        }
        &mut self.text
    }

    pub(crate) fn styles_mut(&mut self) -> &mut Vec<StyleSpan> {
        if let Some(ref mut table) = self.table {
            if let Some(ref mut cell) = table.current_cell {
                return &mut cell.styles;
            }
        }
        &mut self.styles
    }

    pub(crate) fn open_styles_mut(&mut self) -> &mut Vec<OpenStyle> {
        if let Some(ref mut table) = self.table {
            if let Some(ref mut cell) = table.current_cell {
                return &mut cell.open_styles;
            }
        }
        &mut self.open_styles
    }

    pub(crate) fn links_mut(&mut self) -> &mut Vec<LinkSpan> {
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

    pub(crate) fn text_len(&self) -> usize {
        if let Some(ref table) = self.table {
            if let Some(ref cell) = table.current_cell {
                return cell.text.len();
            }
        }
        self.text.len()
    }

    pub(crate) fn append_text(&mut self, value: &str) {
        if value.is_empty() {
            return;
        }
        self.text_mut().push_str(value);
    }

    pub(crate) fn open_style(&mut self, style: MarkdownStyle) {
        let start = self.text_len();
        self.open_styles_mut().push(OpenStyle { style, start });
    }

    pub(crate) fn close_style(&mut self, style: MarkdownStyle) {
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

    pub(crate) fn append_paragraph_separator(&mut self) {
        if !self.list_stack.is_empty() {
            return;
        }
        if self.table.is_some() {
            return;
        }
        self.text.push_str("\n\n");
    }

    pub(crate) fn append_list_prefix(&mut self) {
        let depth = self.list_stack.len();
        if let Some(top) = self.list_stack.last_mut() {
            top.index += 1;
            // Write indent and prefix directly into self.text to avoid heap allocs.
            // depth is practically ≤10, so this loop is at most ~9 iterations.
            for _ in 0..depth.saturating_sub(1) {
                self.text.push_str("  ");
            }
            if top.ordered {
                use std::fmt::Write as _;
                // write! on String uses fmt::Write — no heap allocation.
                let _ = write!(self.text, "{}. ", top.index);
            } else {
                self.text.push_str("• ");
            }
        }
    }

    pub(crate) fn render_inline_code(&mut self, content: &str) {
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

    pub(crate) fn render_code_block(&mut self, content: &str) {
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

    pub(crate) fn handle_link_open(&mut self, href: String) {
        let label_start = self.text_len();
        self.link_stack_mut().push(LinkState { href, label_start });
    }

    pub(crate) fn handle_link_close(&mut self) {
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
        self.links_mut().push(LinkSpan { start, end, href });
    }

    pub(crate) fn close_remaining_styles(&mut self) {
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
