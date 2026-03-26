//! Table rendering helpers for the markdown-to-IR parser.
//!
//! Converts collected table state (headers + rows of cells) into either
//! bullet-list or fenced-code-block representations, appending to the
//! main `RenderState`.

use super::parser::{RenderState, TableCell, TableState};
use super::spans::{LinkSpan, MarkdownStyle, StyleSpan};

/// Close remaining open styles in a cell render target and return the finished cell.
pub(crate) fn finish_table_cell(target: &mut super::parser::RenderTarget) -> TableCell {
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

fn trim_cell(cell: &TableCell) -> TableCell {
    let text = &cell.text;
    let start = text
        .find(|c: char| !c.is_whitespace())
        .unwrap_or(text.len());
    let end = text
        .rfind(|c: char| !c.is_whitespace())
        .and_then(|i| text[i..].chars().next().map(|ch| i + ch.len_utf8()))
        .unwrap_or(0);

    if start == 0 && end == text.len() {
        return cell.clone();
    }

    // Guard against start > end (e.g., all-whitespace cell where
    // find returns text.len() but rfind-based end returns 0).
    if start >= end {
        return TableCell {
            text: String::new(),
            styles: Vec::new(),
            links: Vec::new(),
        };
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

pub(crate) fn render_table_as_bullets(state: &mut RenderState) {
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

pub(crate) fn render_table_as_code(state: &mut RenderState) {
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
        for (i, width) in widths.iter().enumerate().take(column_count) {
            state.text.push(' ');
            if let Some(cell) = cells.get(i) {
                append_cell_text_only(state, cell);
            }
            let cell_len = cells.get(i).map(|c| c.text.len()).unwrap_or(0);
            let pad = width.saturating_sub(cell_len);
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
