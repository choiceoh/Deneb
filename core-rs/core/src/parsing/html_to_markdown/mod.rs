//! HTML to Markdown conversion.
//!
//! Converts HTML to a Markdown-like plain text representation using a
//! two-pass architecture:
//! 1. **Tokenize** — single linear scan producing a `Vec<Token>`.
//! 2. **Emit** — walk tokens and produce Markdown into a single `String`.
//!
//! This replaces the previous 14-pass sequential string transformation
//! approach with O(n×2) scans and ~3 allocations instead of O(n×14)
//! scans and ~30 allocations.

mod attrs;
mod emitter;
mod entities;
mod tokenizer;

#[cfg(test)]
mod tests;

use serde::Serialize;

/// Result of HTML → Markdown conversion.
#[derive(Debug, Serialize)]
pub struct HtmlToMarkdownResult {
    pub text: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub title: Option<String>,
}

/// Convert HTML to a Markdown-like plain text representation.
pub fn html_to_markdown(html: &str) -> HtmlToMarkdownResult {
    // Wrap the entire pipeline in catch_unwind so a panic doesn't
    // abort the FFI layer. Returns empty result on panic.
    match std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| convert(html))) {
        Ok(result) => result,
        Err(_) => HtmlToMarkdownResult {
            text: String::new(),
            title: None,
        },
    }
}

/// Internal conversion pipeline.
fn convert(html: &str) -> HtmlToMarkdownResult {
    // Pass 1: tokenize.
    let tokens = tokenizer::tokenize(html);

    // Pass 2: emit Markdown.
    let (raw_text, title) = emitter::emit(&tokens, html.len());

    // Pass 3: normalize whitespace.
    let text = normalize_whitespace(&raw_text);

    HtmlToMarkdownResult { text, title }
}

/// Normalize whitespace: collapse runs, trim, etc.
fn normalize_whitespace(input: &str) -> String {
    let mut result = String::with_capacity(input.len());

    // Remove \r.
    for ch in input.chars() {
        if ch != '\r' {
            result.push(ch);
        }
    }

    // Collapse trailing whitespace on lines: `[ \t]+\n` → `\n`.
    let mut cleaned = String::with_capacity(result.len());
    let mut trailing_ws = String::new();
    for ch in result.chars() {
        if ch == ' ' || ch == '\t' {
            trailing_ws.push(ch);
        } else if ch == '\n' {
            trailing_ws.clear();
            cleaned.push('\n');
        } else {
            cleaned.push_str(&trailing_ws);
            trailing_ws.clear();
            cleaned.push(ch);
        }
    }
    // Don't append trailing whitespace at end.

    // Collapse 3+ newlines to 2.
    let mut final_result = String::with_capacity(cleaned.len());
    let mut newline_count = 0;
    for ch in cleaned.chars() {
        if ch == '\n' {
            newline_count += 1;
            if newline_count <= 2 {
                final_result.push('\n');
            }
        } else {
            newline_count = 0;
            final_result.push(ch);
        }
    }

    // Collapse multiple spaces/tabs to single space.
    let mut collapsed = String::with_capacity(final_result.len());
    let mut prev_space = false;
    for ch in final_result.chars() {
        if ch == ' ' || ch == '\t' {
            if !prev_space {
                collapsed.push(' ');
            }
            prev_space = true;
        } else {
            prev_space = false;
            collapsed.push(ch);
        }
    }

    collapsed.trim().to_string()
}
