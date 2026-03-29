//! HTML to Markdown conversion.
//!
//! Ports `src/agents/tools/web-fetch-utils.ts:htmlToMarkdown`.
//! Strips script/style/noscript, converts structural tags to markdown,
//! decodes HTML entities, and normalizes whitespace.

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
    // Wrap extract_title in catch_unwind so a panic in title extraction
    // doesn't abort the entire conversion via the FFI layer.
    let title = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| extract_title(html)))
        .unwrap_or(None);

    let mut text = String::with_capacity(html.len());
    text.push_str(html);

    // Each step is wrapped in catch_unwind so a panic in one step
    // degrades gracefully instead of aborting the entire conversion.
    let steps: &[fn(&str) -> String] = &[
        // 1. Strip <script>, <style>, <noscript> blocks.
        |s| {
            let s = strip_tag_block(s, "script");
            let s = strip_tag_block(&s, "style");
            strip_tag_block(&s, "noscript")
        },
        // 2. Convert <a href="X">label</a> → [label](X).
        convert_links,
        // 3. Convert <strong>/<b> → **bold**, <em>/<i> → *italic*.
        convert_emphasis,
        // 4. Convert <pre><code> → fenced blocks, <code> → `inline`.
        convert_code,
        // 5. Convert <s>/<del>/<strike> → ~~strikethrough~~.
        convert_strikethrough,
        // 6. Convert <h1-6>...</h1-6> → # prefix.
        convert_headings,
        // 7. Convert <img src="X" alt="Y"> → [Y](X).
        convert_images,
        // 8. Convert <blockquote> → > prefixed lines.
        convert_blockquotes,
        // 9. Convert <table> → markdown pipe table.
        convert_tables,
        // 10. Convert <li> → "- " or "1. " (ordered/unordered).
        convert_list_items,
        // 11. <br>, <hr> → newline; closing block tags → newline.
        convert_breaks,
        // 12. Strip all remaining HTML tags.
        strip_tags,
        // 13. Decode HTML entities.
        decode_entities,
        // 14. Normalize whitespace.
        normalize_whitespace,
    ];

    for step in steps {
        // Pass a reference to avoid cloning the entire HTML string at each step.
        // After catch_unwind returns the closure (and its borrow of `text`) is
        // dropped, so `text = output` below is safe under NLL.
        match std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| step(&text))) {
            Ok(output) => text = output,
            Err(_) => {
                // Skip this step and continue with the current text.
                // The FFI layer will still return a result instead of a panic error.
            }
        }
    }

    HtmlToMarkdownResult { text, title }
}

/// Extract content from `<title>...</title>` tag.
fn extract_title(html: &str) -> Option<String> {
    let lower = html.to_ascii_lowercase();
    let start_tag = "<title";
    let start_idx = lower.find(start_tag)?;
    // Find the closing > of the opening tag.
    let after_tag = lower.get(start_idx..)?.find('>')? + start_idx + 1;
    let end_tag = "</title>";
    let end_idx = lower.get(after_tag..)?.find(end_tag)? + after_tag;
    let raw = html.get(after_tag..end_idx)?;
    let stripped = strip_tags(raw);
    let decoded = decode_entities(&stripped);
    let normalized = normalize_whitespace(&decoded);
    if normalized.is_empty() {
        None
    } else {
        Some(normalized)
    }
}

/// Strip a paired tag block: `<tag ...>...</tag>` (case-insensitive).
fn strip_tag_block(input: &str, tag: &str) -> String {
    let lower = input.to_ascii_lowercase();
    let open_prefix = format!("<{tag}");
    let close_tag = format!("</{tag}>");

    let mut result = String::with_capacity(input.len());
    let mut cursor = 0;

    while let Some(rel_start) = lower.get(cursor..).and_then(|s| s.find(&open_prefix)) {
        let start = cursor + rel_start;
        // Make sure it's actually a tag boundary (next char is whitespace, > or end).
        let after = start + open_prefix.len();
        if after < input.len() {
            let next = input.as_bytes()[after];
            if next != b'>' && next != b' ' && next != b'\t' && next != b'\n' && next != b'\r' {
                if let Some(s) = input.get(cursor..after) {
                    result.push_str(s);
                }
                cursor = after;
                continue;
            }
        }
        if let Some(s) = input.get(cursor..start) {
            result.push_str(s);
        }
        // Find closing tag.
        if let Some(rel_end) = lower.get(start..).and_then(|s| s.find(&close_tag)) {
            cursor = start + rel_end + close_tag.len();
        } else {
            // No closing tag — strip to end.
            cursor = input.len();
        }
    }
    if let Some(s) = input.get(cursor..) {
        result.push_str(s);
    }
    result
}

/// Convert `<a href="X">label</a>` to `[label](X)`.
fn convert_links(input: &str) -> String {
    let lower = input.to_ascii_lowercase();
    let mut result = String::with_capacity(input.len());
    let mut cursor = 0;

    while let Some(rel_start) = lower.get(cursor..).and_then(|s| s.find("<a ")) {
        let start = cursor + rel_start;
        if let Some(s) = input.get(cursor..start) {
            result.push_str(s);
        }

        // Find the closing > of the <a> tag.
        let tag_end = match input.get(start..).and_then(|s| s.find('>')) {
            Some(e) => start + e + 1,
            None => {
                if let Some(s) = input.get(start..start + 3) {
                    result.push_str(s);
                }
                cursor = start + 3;
                continue;
            }
        };

        // Extract href from the tag.
        let tag_content = match input.get(start..tag_end) {
            Some(s) => s,
            None => {
                cursor = tag_end;
                continue;
            }
        };
        let href = extract_attr(tag_content, "href");

        // Find </a>.
        let close = "</a>";
        let body_end = match lower.get(tag_end..).and_then(|s| s.find(close)) {
            Some(e) => tag_end + e,
            None => {
                if let Some(h) = &href {
                    result.push_str(h);
                }
                cursor = tag_end;
                continue;
            }
        };

        let body = input.get(tag_end..body_end).unwrap_or("");
        let label = normalize_whitespace(&strip_tags(body));

        if let Some(h) = href {
            if label.is_empty() {
                result.push_str(&h);
            } else {
                result.push('[');
                result.push_str(&label);
                result.push_str("](");
                result.push_str(&h);
                result.push(')');
            }
        } else {
            result.push_str(&label);
        }

        cursor = body_end + close.len();
    }
    if let Some(s) = input.get(cursor..) {
        result.push_str(s);
    }
    result
}

/// Extract an attribute value from a tag string like `<a href="value">`.
fn extract_attr(tag: &str, attr: &str) -> Option<String> {
    let lower = tag.to_ascii_lowercase();
    let pattern = format!("{attr}=");
    let idx = lower.find(&pattern)?;
    let after_eq = idx + pattern.len();
    let bytes = tag.as_bytes();
    if after_eq >= bytes.len() {
        return None;
    }
    let quote = bytes[after_eq];
    if quote == b'"' || quote == b'\'' {
        let start = after_eq + 1;
        let end = tag.get(start..)?.find(quote as char).map(|e| start + e)?;
        Some(tag.get(start..end)?.to_string())
    } else {
        // Unquoted attribute: read until whitespace or >.
        let start = after_eq;
        let rest = tag.get(start..)?;
        let end = rest
            .find(|c: char| c.is_ascii_whitespace() || c == '>')
            .map_or(tag.len(), |e| start + e);
        Some(tag.get(start..end)?.to_string())
    }
}

/// Check if the character after a short tag name is a valid tag boundary.
/// Prevents `<b>` from matching `<br>`, `<body>`, `<base>`, etc.
fn is_tag_boundary(input: &str, pos: usize) -> bool {
    if pos >= input.len() {
        return true; // end of input counts as boundary
    }
    let b = input.as_bytes()[pos];
    b == b'>' || b == b' ' || b == b'\t' || b == b'\n' || b == b'\r' || b == b'/'
}

/// Convert `<strong>`/`<b>` → `**...**` and `<em>`/`<i>` → `*...*`.
fn convert_emphasis(input: &str) -> String {
    // Process in two passes: first bold (strong/b), then italic (em/i).
    let after_bold = convert_paired_inline(input, &["strong", "b"], "**");
    convert_paired_inline(&after_bold, &["em", "i"], "*")
}

/// Generic converter for paired inline tags to markdown wrappers.
/// `tags` is a list of tag names to match; `wrapper` is the markdown delimiter.
fn convert_paired_inline(input: &str, tags: &[&str], wrapper: &str) -> String {
    let lower = input.to_ascii_lowercase();
    let mut result = String::with_capacity(input.len());
    let mut cursor = 0;

    while cursor < input.len() {
        // Find the next `<` character.
        let lt = match lower.get(cursor..).and_then(|s| s.find('<')) {
            Some(pos) => cursor + pos,
            None => break,
        };

        let mut matched_tag: Option<&str> = None;
        for &tag in tags {
            let open = format!("<{tag}");
            if let Some(rest) = lower.get(lt..) {
                if rest.starts_with(&open) && is_tag_boundary(input, lt + open.len()) {
                    matched_tag = Some(tag);
                    break;
                }
            }
        }

        let tag = match matched_tag {
            Some(t) => t,
            None => {
                // Advance past this `<` to avoid infinite loop.
                if let Some(s) = input.get(cursor..lt + 1) {
                    result.push_str(s);
                }
                cursor = lt + 1;
                continue;
            }
        };

        // Emit text before the tag.
        if let Some(s) = input.get(cursor..lt) {
            result.push_str(s);
        }

        // Find closing > of opening tag.
        let tag_end = match input.get(lt..).and_then(|s| s.find('>')) {
            Some(e) => lt + e + 1,
            None => {
                cursor = lt + 1;
                continue;
            }
        };

        // Find the closing tag (try all variants: </strong>, </b>).
        let close = format!("</{tag}>");
        let body_end = match lower.get(tag_end..).and_then(|s| s.find(&close)) {
            Some(e) => tag_end + e,
            None => {
                cursor = tag_end;
                continue;
            }
        };

        let body = input.get(tag_end..body_end).unwrap_or("");
        if !body.trim().is_empty() {
            result.push_str(wrapper);
            result.push_str(body);
            result.push_str(wrapper);
        }
        cursor = body_end + close.len();
    }

    if let Some(s) = input.get(cursor..) {
        result.push_str(s);
    }
    result
}

/// Convert `<pre><code>` → fenced code blocks, `<code>` → inline backticks.
fn convert_code(input: &str) -> String {
    // Pass 1: Convert <pre> blocks (may contain <code>).
    let after_pre = convert_pre_blocks(input);
    // Pass 2: Convert remaining inline <code> tags.
    convert_inline_code(&after_pre)
}

/// Convert `<pre>` blocks to fenced code blocks.
fn convert_pre_blocks(input: &str) -> String {
    let lower = input.to_ascii_lowercase();
    let mut result = String::with_capacity(input.len());
    let mut cursor = 0;

    while let Some(rel) = lower.get(cursor..).and_then(|s| s.find("<pre")) {
        let start = cursor + rel;
        if !is_tag_boundary(input, start + 4) {
            if let Some(s) = input.get(cursor..start + 1) {
                result.push_str(s);
            }
            cursor = start + 1;
            continue;
        }

        if let Some(s) = input.get(cursor..start) {
            result.push_str(s);
        }

        // Find closing > of <pre>.
        let pre_tag_end = match input.get(start..).and_then(|s| s.find('>')) {
            Some(e) => start + e + 1,
            None => {
                cursor = start + 4;
                continue;
            }
        };

        // Find </pre>.
        let close_pre = "</pre>";
        let pre_end = match lower.get(pre_tag_end..).and_then(|s| s.find(close_pre)) {
            Some(e) => pre_tag_end + e,
            None => {
                cursor = pre_tag_end;
                continue;
            }
        };

        let body = input.get(pre_tag_end..pre_end).unwrap_or("");

        // Check if body starts with <code> — extract language from class.
        let body_lower = body.to_ascii_lowercase();
        let (lang, code_body) = if body_lower.starts_with("<code") {
            let lang = extract_code_language(body);
            let code_tag_end = body.find('>').map_or(0, |e| e + 1);
            let code_body = body.get(code_tag_end..).unwrap_or("");
            // Strip </code> from end if present.
            let code_body = code_body
                .to_ascii_lowercase()
                .rfind("</code>")
                .map_or(code_body, |idx| code_body.get(..idx).unwrap_or(code_body));
            (lang, code_body.to_string())
        } else {
            (String::new(), body.to_string())
        };

        result.push_str("\n```");
        result.push_str(&lang);
        result.push('\n');
        result.push_str(&strip_tags(&code_body));
        result.push_str("\n```\n");

        cursor = pre_end + close_pre.len();
    }

    if let Some(s) = input.get(cursor..) {
        result.push_str(s);
    }
    result
}

/// Extract language from `<code class="language-X">` or `class="lang-X"`.
fn extract_code_language(tag: &str) -> String {
    let class = match extract_attr(tag, "class") {
        Some(c) => c,
        None => return String::new(),
    };
    for prefix in &["language-", "lang-"] {
        if let Some(rest) = class.strip_prefix(prefix) {
            let lang = rest
                .split(|c: char| c.is_ascii_whitespace())
                .next()
                .unwrap_or("");
            if !lang.is_empty() {
                return lang.to_string();
            }
        }
    }
    String::new()
}

/// Convert inline `<code>` to backtick-wrapped text.
fn convert_inline_code(input: &str) -> String {
    let lower = input.to_ascii_lowercase();
    let mut result = String::with_capacity(input.len());
    let mut cursor = 0;

    while let Some(rel) = lower.get(cursor..).and_then(|s| s.find("<code")) {
        let start = cursor + rel;
        if !is_tag_boundary(input, start + 5) {
            if let Some(s) = input.get(cursor..start + 1) {
                result.push_str(s);
            }
            cursor = start + 1;
            continue;
        }

        if let Some(s) = input.get(cursor..start) {
            result.push_str(s);
        }

        let tag_end = match input.get(start..).and_then(|s| s.find('>')) {
            Some(e) => start + e + 1,
            None => {
                cursor = start + 5;
                continue;
            }
        };

        let close = "</code>";
        let body_end = match lower.get(tag_end..).and_then(|s| s.find(close)) {
            Some(e) => tag_end + e,
            None => {
                cursor = tag_end;
                continue;
            }
        };

        let body = input.get(tag_end..body_end).unwrap_or("");
        let text = strip_tags(body);
        if !text.is_empty() {
            result.push('`');
            result.push_str(&text);
            result.push('`');
        }
        cursor = body_end + close.len();
    }

    if let Some(s) = input.get(cursor..) {
        result.push_str(s);
    }
    result
}

/// Convert `<s>`, `<del>`, `<strike>` → `~~...~~`.
fn convert_strikethrough(input: &str) -> String {
    convert_paired_inline(input, &["s", "del", "strike"], "~~")
}

/// Convert `<img src="X" alt="Y">` → `[Y](X)`.
fn convert_images(input: &str) -> String {
    let lower = input.to_ascii_lowercase();
    let mut result = String::with_capacity(input.len());
    let mut cursor = 0;

    while let Some(rel) = lower.get(cursor..).and_then(|s| s.find("<img")) {
        let start = cursor + rel;
        if !is_tag_boundary(input, start + 4) {
            if let Some(s) = input.get(cursor..start + 1) {
                result.push_str(s);
            }
            cursor = start + 1;
            continue;
        }

        if let Some(s) = input.get(cursor..start) {
            result.push_str(s);
        }

        // Find the end of the <img> tag (self-closing).
        let tag_end = match input.get(start..).and_then(|s| s.find('>')) {
            Some(e) => start + e + 1,
            None => {
                cursor = start + 4;
                continue;
            }
        };

        let tag = input.get(start..tag_end).unwrap_or("");
        let src = extract_attr(tag, "src");
        let alt = extract_attr(tag, "alt");

        if let Some(src) = src {
            let label = alt
                .filter(|a| !a.is_empty())
                .unwrap_or_else(|| filename_from_url(&src));
            result.push('[');
            result.push_str(&label);
            result.push_str("](");
            result.push_str(&src);
            result.push(')');
        }
        cursor = tag_end;
    }

    if let Some(s) = input.get(cursor..) {
        result.push_str(s);
    }
    result
}

/// Extract a filename from a URL path for use as an image label.
fn filename_from_url(url: &str) -> String {
    url.rsplit('/')
        .next()
        .and_then(|s| s.split('?').next())
        .filter(|s| !s.is_empty())
        .unwrap_or("image")
        .to_string()
}

/// Convert `<blockquote>...</blockquote>` → `> ` prefixed lines.
fn convert_blockquotes(input: &str) -> String {
    let lower = input.to_ascii_lowercase();
    let mut result = String::with_capacity(input.len());
    let mut cursor = 0;

    while let Some(rel) = lower.get(cursor..).and_then(|s| s.find("<blockquote")) {
        let start = cursor + rel;
        if !is_tag_boundary(input, start + 11) {
            if let Some(s) = input.get(cursor..start + 1) {
                result.push_str(s);
            }
            cursor = start + 1;
            continue;
        }

        if let Some(s) = input.get(cursor..start) {
            result.push_str(s);
        }

        let tag_end = match input.get(start..).and_then(|s| s.find('>')) {
            Some(e) => start + e + 1,
            None => {
                cursor = start + 11;
                continue;
            }
        };

        let close = "</blockquote>";
        let body_end = match lower.get(tag_end..).and_then(|s| s.find(close)) {
            Some(e) => tag_end + e,
            None => {
                cursor = tag_end;
                continue;
            }
        };

        let body = input.get(tag_end..body_end).unwrap_or("");
        let text = normalize_whitespace(&strip_tags(body));
        if !text.is_empty() {
            result.push('\n');
            for line in text.lines() {
                result.push_str("> ");
                result.push_str(line);
                result.push('\n');
            }
        }
        cursor = body_end + close.len();
    }

    if let Some(s) = input.get(cursor..) {
        result.push_str(s);
    }
    result
}

/// Convert `<table>` → markdown pipe table.
fn convert_tables(input: &str) -> String {
    let lower = input.to_ascii_lowercase();
    let mut result = String::with_capacity(input.len());
    let mut cursor = 0;

    while let Some(rel) = lower.get(cursor..).and_then(|s| s.find("<table")) {
        let start = cursor + rel;
        if !is_tag_boundary(input, start + 6) {
            if let Some(s) = input.get(cursor..start + 1) {
                result.push_str(s);
            }
            cursor = start + 1;
            continue;
        }

        if let Some(s) = input.get(cursor..start) {
            result.push_str(s);
        }

        let close = "</table>";
        let table_end = match lower.get(start..).and_then(|s| s.find(close)) {
            Some(e) => start + e,
            None => {
                cursor = start + 6;
                continue;
            }
        };

        let table_html = input.get(start..table_end + close.len()).unwrap_or("");
        let md_table = table_to_markdown(table_html);
        result.push('\n');
        result.push_str(&md_table);
        result.push('\n');

        cursor = table_end + close.len();
    }

    if let Some(s) = input.get(cursor..) {
        result.push_str(s);
    }
    result
}

/// Parse an HTML table into a markdown pipe table.
fn table_to_markdown(table: &str) -> String {
    let lower = table.to_ascii_lowercase();
    let mut rows: Vec<(Vec<String>, bool)> = Vec::new(); // (cells, is_header)

    let mut cursor = 0;
    while let Some(rel) = lower.get(cursor..).and_then(|s| s.find("<tr")) {
        let tr_start = cursor + rel;
        let close_tr = "</tr>";
        let tr_end = match lower.get(tr_start..).and_then(|s| s.find(close_tr)) {
            Some(e) => tr_start + e,
            None => break,
        };

        let row_html = table.get(tr_start..tr_end + close_tr.len()).unwrap_or("");
        let row_lower = row_html.to_ascii_lowercase();
        let has_th = row_lower.contains("<th");

        let mut cells = Vec::new();
        let mut cell_cursor = 0;
        while cell_cursor < row_html.len() {
            // Find next <th or <td.
            let th_pos = row_lower.get(cell_cursor..).and_then(|s| s.find("<th"));
            let td_pos = row_lower.get(cell_cursor..).and_then(|s| s.find("<td"));

            let (cell_start, is_th) = match (th_pos, td_pos) {
                (Some(th), Some(td)) => {
                    if th < td {
                        (cell_cursor + th, true)
                    } else {
                        (cell_cursor + td, false)
                    }
                }
                (Some(th), None) => (cell_cursor + th, true),
                (None, Some(td)) => (cell_cursor + td, false),
                (None, None) => break,
            };

            let close_cell = if is_th { "</th>" } else { "</td>" };
            let tag_end = match row_html.get(cell_start..).and_then(|s| s.find('>')) {
                Some(e) => cell_start + e + 1,
                None => break,
            };

            let cell_end = match row_lower.get(tag_end..).and_then(|s| s.find(close_cell)) {
                Some(e) => tag_end + e,
                None => {
                    cell_cursor = tag_end;
                    continue;
                }
            };

            let body = row_html.get(tag_end..cell_end).unwrap_or("");
            let text = normalize_whitespace(&strip_tags(body));
            // Escape pipe characters in cell content.
            cells.push(text.replace('|', "\\|"));
            cell_cursor = cell_end + close_cell.len();
        }

        if !cells.is_empty() {
            rows.push((cells, has_th));
        }
        cursor = tr_end + close_tr.len();
    }

    if rows.is_empty() {
        return String::new();
    }

    // Build markdown table.
    let mut md = String::new();
    let mut separator_added = false;

    for (i, (cells, is_header)) in rows.iter().enumerate() {
        md.push_str("| ");
        md.push_str(&cells.join(" | "));
        md.push_str(" |\n");

        // Add separator after header row, or after first row if no explicit headers.
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

/// Convert `<h1>...</h1>` through `<h6>...</h6>` to markdown headings.
fn convert_headings(input: &str) -> String {
    let lower = input.to_ascii_lowercase();
    let mut result = String::with_capacity(input.len());
    let mut cursor = 0;

    while cursor < input.len() {
        if let Some(rel) = lower.get(cursor..).and_then(|s| s.find("<h")) {
            let start = cursor + rel;
            let after_h = start + 2;
            if after_h < input.len() {
                let level_byte = input.as_bytes()[after_h];
                if (b'1'..=b'6').contains(&level_byte) {
                    let level = (level_byte - b'0') as usize;
                    // Find closing > of opening tag.
                    if let Some(gt) = input.get(after_h..).and_then(|s| s.find('>')) {
                        let body_start = after_h + gt + 1;
                        let close = format!("</h{level}>");
                        if let Some(rel_close) =
                            lower.get(body_start..).and_then(|s| s.find(&close))
                        {
                            let body_end = body_start + rel_close;
                            if let Some(s) = input.get(cursor..start) {
                                result.push_str(s);
                            }
                            let body = input.get(body_start..body_end).unwrap_or("");
                            let label = normalize_whitespace(&strip_tags(body));
                            let prefix = "#".repeat(level.clamp(1, 6));
                            result.push('\n');
                            result.push_str(&prefix);
                            result.push(' ');
                            result.push_str(&label);
                            result.push('\n');
                            cursor = body_end + close.len();
                            continue;
                        }
                    }
                }
            }
            if let Some(s) = input.get(cursor..start + 1) {
                result.push_str(s);
            }
            cursor = start + 1;
        } else {
            break;
        }
    }
    if let Some(s) = input.get(cursor..) {
        result.push_str(s);
    }
    result
}

/// Convert `<li>...</li>` to markdown list items with ordered/unordered awareness.
fn convert_list_items(input: &str) -> String {
    let lower = input.to_ascii_lowercase();
    let mut result = String::with_capacity(input.len());
    let mut cursor = 0;
    let mut in_ol = false;
    let mut ol_counter: usize = 0;

    while cursor < input.len() {
        let rest = match lower.get(cursor..) {
            Some(s) => s,
            None => break,
        };

        // Find next interesting tag: <ol, </ol, <ul, </ul, or <li.
        let next_lt = match rest.find('<') {
            Some(pos) => cursor + pos,
            None => break,
        };

        let lower_from_lt = match lower.get(next_lt..) {
            Some(s) => s,
            None => break,
        };

        if lower_from_lt.starts_with("<ol") && is_tag_boundary(input, next_lt + 3) {
            if let Some(s) = input.get(cursor..next_lt) {
                result.push_str(s);
            }
            in_ol = true;
            ol_counter = 0;
            // Skip the <ol> tag itself.
            cursor = input
                .get(next_lt..)
                .and_then(|s| s.find('>'))
                .map_or(next_lt + 3, |e| next_lt + e + 1);
            continue;
        }

        if lower_from_lt.starts_with("</ol>") {
            if let Some(s) = input.get(cursor..next_lt) {
                result.push_str(s);
            }
            in_ol = false;
            ol_counter = 0;
            cursor = next_lt + 5;
            continue;
        }

        if lower_from_lt.starts_with("<ul") && is_tag_boundary(input, next_lt + 3) {
            if let Some(s) = input.get(cursor..next_lt) {
                result.push_str(s);
            }
            in_ol = false;
            ol_counter = 0;
            cursor = input
                .get(next_lt..)
                .and_then(|s| s.find('>'))
                .map_or(next_lt + 3, |e| next_lt + e + 1);
            continue;
        }

        if lower_from_lt.starts_with("</ul>") {
            if let Some(s) = input.get(cursor..next_lt) {
                result.push_str(s);
            }
            cursor = next_lt + 5;
            continue;
        }

        if lower_from_lt.starts_with("<li") && is_tag_boundary(input, next_lt + 3) {
            if let Some(s) = input.get(cursor..next_lt) {
                result.push_str(s);
            }

            let tag_end = match input.get(next_lt..).and_then(|s| s.find('>')) {
                Some(e) => next_lt + e + 1,
                None => {
                    cursor = next_lt + 3;
                    continue;
                }
            };

            let close = "</li>";
            let body_end = match lower.get(tag_end..).and_then(|s| s.find(close)) {
                Some(e) => tag_end + e,
                None => {
                    cursor = tag_end;
                    continue;
                }
            };

            let body = input.get(tag_end..body_end).unwrap_or("");
            let label = normalize_whitespace(&strip_tags(body));
            if !label.is_empty() {
                if in_ol {
                    ol_counter += 1;
                    result.push('\n');
                    result.push_str(&ol_counter.to_string());
                    result.push_str(". ");
                } else {
                    result.push_str("\n- ");
                }
                result.push_str(&label);
            }
            cursor = body_end + close.len();
            continue;
        }

        // Not a list-related tag — advance past this `<`.
        if let Some(s) = input.get(cursor..next_lt + 1) {
            result.push_str(s);
        }
        cursor = next_lt + 1;
    }

    if let Some(s) = input.get(cursor..) {
        result.push_str(s);
    }
    result
}

/// Convert `<br>`, `<hr>` to newlines; closing block tags to newlines.
fn convert_breaks(input: &str) -> String {
    let lower = input.to_ascii_lowercase();
    let mut result = String::with_capacity(input.len());
    let mut cursor = 0;

    let br_hr_tags = ["<br>", "<br/>", "<br />", "<hr>", "<hr/>", "<hr />"];
    let block_close_tags = [
        "</p>",
        "</div>",
        "</section>",
        "</article>",
        "</header>",
        "</footer>",
        "</table>",
        "</tr>",
        "</ul>",
        "</ol>",
    ];

    while cursor < input.len() {
        let lower_rest = match lower.get(cursor..) {
            Some(s) => s,
            None => break,
        };

        let mut matched = false;

        // Check br/hr tags.
        for tag in &br_hr_tags {
            if lower_rest.starts_with(tag) {
                result.push('\n');
                cursor += tag.len();
                matched = true;
                break;
            }
        }
        if matched {
            continue;
        }

        // Check closing block tags.
        for tag in &block_close_tags {
            if lower_rest.starts_with(tag) {
                result.push('\n');
                cursor += tag.len();
                matched = true;
                break;
            }
        }
        if matched {
            continue;
        }

        // Advance by full UTF-8 character.
        match input.get(cursor..) {
            Some(rest) => match rest.chars().next() {
                Some(ch) => {
                    result.push(ch);
                    cursor += ch.len_utf8();
                }
                None => break,
            },
            None => break,
        }
    }
    result
}

/// Strip all HTML tags while preserving standalone `>` characters
/// (e.g., markdown blockquote `> ` prefixes produced by earlier steps).
fn strip_tags(input: &str) -> String {
    let mut result = String::with_capacity(input.len());
    let mut in_tag = false;
    for ch in input.chars() {
        if ch == '<' {
            in_tag = true;
        } else if ch == '>' && in_tag {
            in_tag = false;
        } else if !in_tag {
            result.push(ch);
        }
    }
    result
}

/// Decode common HTML entities.
fn decode_entities(input: &str) -> String {
    let mut result = String::with_capacity(input.len());
    let bytes = input.as_bytes();
    let len = bytes.len();
    let mut i = 0;

    while i < len {
        if bytes[i] != b'&' {
            // Advance by full UTF-8 character to preserve multi-byte chars.
            match input.get(i..) {
                Some(rest) => match rest.chars().next() {
                    Some(ch) => {
                        result.push(ch);
                        i += ch.len_utf8();
                    }
                    None => break,
                },
                None => break,
            }
            continue;
        }

        // Try to match an entity.
        if let Some((ch, advance)) = try_decode_entity(input, i) {
            result.push(ch);
            i += advance;
        } else {
            result.push('&');
            i += 1;
        }
    }
    result
}

fn try_decode_entity(input: &str, pos: usize) -> Option<(char, usize)> {
    let rest = input.get(pos..)?;

    // Named entities (case-insensitive).
    let named: &[(&str, char)] = &[
        ("&nbsp;", ' '),
        ("&amp;", '&'),
        ("&quot;", '"'),
        ("&lt;", '<'),
        ("&gt;", '>'),
        ("&#39;", '\''),
        ("&apos;", '\''),
    ];

    // Only lowercase a small bounded prefix — never the entire remaining input.
    // Find the nearest valid char boundary at or before byte 10.
    let prefix_end = bounded_char_boundary(rest, 10);
    let rest_lower = rest.get(..prefix_end)?.to_ascii_lowercase();

    for &(entity, ch) in named {
        if rest_lower.starts_with(entity) {
            return Some((ch, entity.len()));
        }
    }

    // Hex numeric: &#xHH; — cap search to first 12 bytes (covers realistic entities).
    if rest_lower.starts_with("&#x") {
        let after = rest.get(3..)?;
        // Only search for ';' within a reasonable range to avoid scanning megabytes.
        let search_limit = bounded_char_boundary(after, 12);
        if let Some(semi) = after.get(..search_limit)?.find(';') {
            let hex_str = after.get(..semi)?;
            if let Ok(code) = u32::from_str_radix(hex_str, 16) {
                if let Some(ch) = char::from_u32(code) {
                    return Some((ch, 3 + semi + 1));
                }
            }
        }
        return None;
    }

    // Decimal numeric: &#DDD;
    if rest_lower.starts_with("&#") {
        let after = rest.get(2..)?;
        let search_limit = bounded_char_boundary(after, 12);
        if let Some(semi) = after.get(..search_limit)?.find(';') {
            let dec_str = after.get(..semi)?;
            if let Ok(code) = dec_str.parse::<u32>() {
                if let Some(ch) = char::from_u32(code) {
                    return Some((ch, 2 + semi + 1));
                }
            }
        }
    }

    None
}

/// Find the largest valid char boundary at or before `max_byte`.
/// Returns 0 if the string is empty.
fn bounded_char_boundary(s: &str, max_byte: usize) -> usize {
    let mut end = max_byte.min(s.len());
    while end > 0 && !s.is_char_boundary(end) {
        end -= 1;
    }
    end
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn basic_html() {
        let result = html_to_markdown("<p>Hello <b>world</b></p>");
        assert_eq!(result.text, "Hello **world**");
    }

    #[test]
    fn strips_script_style() {
        let html = "<p>before</p><script>alert(1)</script><style>.x{}</style><p>after</p>";
        let result = html_to_markdown(html);
        assert!(result.text.contains("before"));
        assert!(result.text.contains("after"));
        assert!(!result.text.contains("alert"));
        assert!(!result.text.contains(".x"));
    }

    #[test]
    fn extracts_title() {
        let html = "<html><head><title>My Page</title></head><body>content</body></html>";
        let result = html_to_markdown(html);
        assert_eq!(result.title.as_deref(), Some("My Page"));
    }

    #[test]
    fn converts_links() {
        let html = r#"<a href="https://example.com">Click here</a>"#;
        let result = html_to_markdown(html);
        assert!(result.text.contains("[Click here](https://example.com)"));
    }

    #[test]
    fn converts_headings() {
        let html = "<h1>Title</h1><h2>Subtitle</h2><p>text</p>";
        let result = html_to_markdown(html);
        assert!(result.text.contains("# Title"));
        assert!(result.text.contains("## Subtitle"));
    }

    #[test]
    fn converts_list_items() {
        let html = "<ul><li>one</li><li>two</li></ul>";
        let result = html_to_markdown(html);
        assert!(result.text.contains("- one"));
        assert!(result.text.contains("- two"));
    }

    #[test]
    fn decodes_entities() {
        let html = "<p>&amp; &lt; &gt; &quot; &#39; &#x41; &#65;</p>";
        let result = html_to_markdown(html);
        assert!(result.text.contains("& < > \" ' A A"));
    }

    #[test]
    fn normalizes_whitespace() {
        let html = "<p>  hello   world  </p>";
        let result = html_to_markdown(html);
        assert_eq!(result.text, "hello world");
    }

    #[test]
    fn multibyte_utf8() {
        // Korean + emoji: must not panic on multi-byte characters.
        let html = "<p>안녕하세요 🌍</p><br><p>세계</p>";
        let result = html_to_markdown(html);
        assert!(result.text.contains("안녕하세요"));
        assert!(result.text.contains("🌍"));
        assert!(result.text.contains("세계"));
    }

    #[test]
    fn multibyte_entities_mixed() {
        // Multi-byte chars mixed with HTML entities.
        let html = "<p>한국어 &amp; 日本語</p>";
        let result = html_to_markdown(html);
        assert_eq!(result.text, "한국어 & 日本語");
    }

    #[test]
    fn empty_input() {
        let result = html_to_markdown("");
        assert_eq!(result.text, "");
        assert!(result.title.is_none());
    }

    #[test]
    fn truncated_tags() {
        // Truncated tags at end of input must not panic.
        let cases = [
            "<h",
            "<h1",
            "<h1>",
            "<a ",
            "<a href=",
            "<li",
            "<li>",
            "<br",
            "<hr",
            "</p",
            "<script",
            "<style",
            "<noscript",
            "&",
            "&amp",
            "&#",
            "&#x",
            "&#x4",
            "&#39",
        ];
        for case in cases {
            let result = html_to_markdown(case);
            // Just ensure no panic; content correctness is secondary.
            let _ = result.text;
        }
    }

    #[test]
    fn multibyte_near_entity_boundary() {
        // Multi-byte chars near entity decode boundaries (byte 10 mid-char).
        let html = "<p>&amp;한국어텍스트</p>";
        let result = html_to_markdown(html);
        assert!(result.text.contains("&"));
        assert!(result.text.contains("한국어텍스트"));
    }

    #[test]
    fn many_ampersands_with_multibyte() {
        // Many & chars interleaved with multi-byte text.
        let html = "&한&국&어&amp;테스트";
        let result = html_to_markdown(html);
        assert!(result.text.contains("&테스트"));
    }

    #[test]
    fn deeply_nested_tags() {
        let html = "<div>".repeat(100) + "content" + &"</div>".repeat(100);
        let result = html_to_markdown(&html);
        assert!(result.text.contains("content"));
    }

    #[test]
    fn unbalanced_tags() {
        let html = "<h1>Title</h2><a href=\"x\">link<p>text</li></a>";
        let result = html_to_markdown(html);
        let _ = result.text; // no panic
    }

    #[test]
    fn script_with_angle_brackets() {
        let html = "<script>if (a < b && c > d) { alert('</script>test'); }</script>after";
        let result = html_to_markdown(html);
        assert!(result.text.contains("after"));
    }

    #[test]
    fn emoji_sequences() {
        // Complex emoji (multi-codepoint, ZWJ sequences).
        let html = "<p>👨‍👩‍👧‍👦 family 🏳️‍🌈 flag</p>";
        let result = html_to_markdown(html);
        assert!(result.text.contains("family"));
        assert!(result.text.contains("flag"));
    }

    #[test]
    fn null_bytes() {
        let html = "<p>before\0after</p>";
        let result = html_to_markdown(html);
        // Should not panic. The null byte may or may not survive.
        let _ = result.text;
    }

    #[test]
    fn only_tags_no_content() {
        let html = "<div><p><span></span></p></div>";
        let result = html_to_markdown(html);
        assert!(result.text.is_empty() || result.text.trim().is_empty());
    }

    #[test]
    fn numeric_entity_edge_cases() {
        // Invalid code point, huge number, zero.
        let html = "&#0; &#xFFFFFF; &#999999999; &#xD800; normal";
        let result = html_to_markdown(html);
        assert!(result.text.contains("normal"));
    }

    #[test]
    fn href_with_multibyte() {
        let html = r#"<a href="https://example.com/한국">한국어 링크</a>"#;
        let result = html_to_markdown(html);
        assert!(result
            .text
            .contains("[한국어 링크](https://example.com/한국)"));
    }

    #[test]
    fn large_input_no_panic() {
        // ~1MB of repetitive HTML.
        let chunk = "<p>Hello &amp; world 한국어 🌍</p><br><hr>\n";
        let html: String = chunk.repeat(20_000);
        let result = html_to_markdown(&html);
        assert!(result.text.contains("Hello"));
    }

    #[test]
    fn curly_quotes_in_html() {
        // RIGHT SINGLE QUOTATION MARK (U+2019, 3 bytes: E2 80 99) near tag boundaries.
        // This is the pattern from YouTube Korean HTML that triggered panics.
        let html = "<!doctype html><html lang=\"ko-kr\"><head><title>YouTube\u{2019}s Best</title></head><body><p>It\u{2019}s a video about \u{2018}coding\u{2019} &amp; stuff</p><li>Item with \u{2019}quotes\u{2019}</li><a href=\"https://example.com\">Link\u{2019}s text</a><h1>Heading with \u{2019}curly\u{2019}</h1></body></html>";
        let result = html_to_markdown(html);
        // Must not panic. Content should preserve the curly quotes.
        assert!(result.text.contains("\u{2019}"));
    }

    #[test]
    fn curly_quotes_at_every_byte_alignment() {
        // Place multi-byte \u{2019} at different byte offsets to hit all alignments.
        for padding in 0..4 {
            let prefix = "x".repeat(padding);
            let html = format!(
                "<p>{prefix}\u{2019}</p><li>{prefix}\u{2019}item</li><a href=\"u\">{prefix}\u{2019}link</a><h2>{prefix}\u{2019}head</h2>"
            );
            let result = html_to_markdown(&html);
            assert!(
                result.text.contains("\u{2019}"),
                "failed at padding={padding}"
            );
        }
    }

    #[test]
    fn entity_adjacent_to_multibyte() {
        // Entity decoding right next to multi-byte chars.
        let html = "\u{2019}&amp;\u{2019}&lt;\u{2019}&#8217;\u{2019}&#x2019;\u{2019}";
        let result = html_to_markdown(html);
        assert!(result.text.contains("&"));
        assert!(result.text.contains("<"));
    }

    #[test]
    fn youtube_like_korean_html() {
        // Simulate the YouTube HTML pattern: large doc with Korean + curly quotes.
        let mut html = String::from(
            r#"<!doctype html><html style="font-size: 10px;font-family: roboto, arial, sans-serif;" lang="ko-kr"><head><title>테스트 동영상</title></head><body>"#,
        );
        // Pad to push curly quotes near byte ~900.
        html.push_str(&"<div>패딩텍스트</div>".repeat(30));
        html.push_str("<p>It\u{2019}s a test \u{2018}video\u{2019} for Korean users</p>");
        html.push_str("<script>var x = 'don\u{2019}t';</script>");
        html.push_str("<li>\u{2019}목록 항목\u{2019}</li>");
        html.push_str("</body></html>");
        let result = html_to_markdown(&html);
        assert!(result.title.as_deref() == Some("테스트 동영상"));
        assert!(!result.text.contains("var x"));
    }

    // --- New conversion tests ---

    #[test]
    fn converts_bold() {
        let html = "<p><strong>bold text</strong> and <b>also bold</b></p>";
        let result = html_to_markdown(html);
        assert!(
            result.text.contains("**bold text**"),
            "got: {}",
            result.text
        );
        assert!(
            result.text.contains("**also bold**"),
            "got: {}",
            result.text
        );
    }

    #[test]
    fn converts_italic() {
        let html = "<p><em>italic text</em> and <i>also italic</i></p>";
        let result = html_to_markdown(html);
        assert!(
            result.text.contains("*italic text*"),
            "got: {}",
            result.text
        );
        assert!(
            result.text.contains("*also italic*"),
            "got: {}",
            result.text
        );
    }

    #[test]
    fn b_tag_boundary() {
        // <b> should not match <br>, <body>, <base>, <button>.
        let html = "<body><br><base href='x'><button>click</button><b>bold</b></body>";
        let result = html_to_markdown(html);
        assert!(result.text.contains("**bold**"), "got: {}", result.text);
        assert!(!result.text.contains("**utton"), "got: {}", result.text);
        assert!(!result.text.contains("**ody"), "got: {}", result.text);
    }

    #[test]
    fn i_tag_boundary() {
        // <i> should not match <img>, <iframe>, <input>.
        let html = "<p><input type='text'><i>italic</i></p>";
        let result = html_to_markdown(html);
        assert!(result.text.contains("*italic*"), "got: {}", result.text);
    }

    #[test]
    fn nested_emphasis() {
        let html = "<strong><em>bold italic</em></strong>";
        let result = html_to_markdown(html);
        // Should contain both markers (order may vary).
        assert!(result.text.contains("bold italic"), "got: {}", result.text);
        assert!(
            result.text.contains("***") || result.text.contains("**"),
            "got: {}",
            result.text
        );
    }

    #[test]
    fn converts_inline_code() {
        let html = "<p>Use <code>println!</code> to print</p>";
        let result = html_to_markdown(html);
        assert!(result.text.contains("`println!`"), "got: {}", result.text);
    }

    #[test]
    fn converts_code_block() {
        let html = "<pre><code>fn main() {\n    println!(\"hello\");\n}</code></pre>";
        let result = html_to_markdown(html);
        assert!(result.text.contains("```"), "got: {}", result.text);
        assert!(result.text.contains("fn main()"), "got: {}", result.text);
    }

    #[test]
    fn converts_code_block_with_language() {
        let html = r#"<pre><code class="language-rust">let x = 42;</code></pre>"#;
        let result = html_to_markdown(html);
        assert!(result.text.contains("```rust"), "got: {}", result.text);
        assert!(result.text.contains("let x = 42;"), "got: {}", result.text);
    }

    #[test]
    fn converts_strikethrough() {
        let html = "<p><del>deleted</del> and <s>struck</s> and <strike>old</strike></p>";
        let result = html_to_markdown(html);
        assert!(result.text.contains("~~deleted~~"), "got: {}", result.text);
        assert!(result.text.contains("~~struck~~"), "got: {}", result.text);
        assert!(result.text.contains("~~old~~"), "got: {}", result.text);
    }

    #[test]
    fn converts_image() {
        let html = r#"<img src="https://example.com/pic.jpg" alt="A photo">"#;
        let result = html_to_markdown(html);
        assert!(
            result
                .text
                .contains("[A photo](https://example.com/pic.jpg)"),
            "got: {}",
            result.text
        );
    }

    #[test]
    fn converts_image_no_alt() {
        let html = r#"<img src="https://example.com/photo.png">"#;
        let result = html_to_markdown(html);
        assert!(
            result
                .text
                .contains("[photo.png](https://example.com/photo.png)"),
            "got: {}",
            result.text
        );
    }

    #[test]
    fn converts_blockquote() {
        let html = "<blockquote>This is quoted text</blockquote>";
        let result = html_to_markdown(html);
        assert!(
            result.text.contains("> This is quoted text"),
            "got: {}",
            result.text
        );
    }

    #[test]
    fn converts_simple_table() {
        let html = "<table><tr><td>A</td><td>B</td></tr><tr><td>1</td><td>2</td></tr></table>";
        let result = html_to_markdown(html);
        assert!(result.text.contains("| A | B |"), "got: {}", result.text);
        assert!(result.text.contains("| 1 | 2 |"), "got: {}", result.text);
        assert!(result.text.contains("| --- |"), "got: {}", result.text);
    }

    #[test]
    fn converts_table_with_headers() {
        let html =
            "<table><tr><th>Name</th><th>Age</th></tr><tr><td>Alice</td><td>30</td></tr></table>";
        let result = html_to_markdown(html);
        assert!(
            result.text.contains("| Name | Age |"),
            "got: {}",
            result.text
        );
        assert!(
            result.text.contains("| --- | --- |"),
            "got: {}",
            result.text
        );
        assert!(
            result.text.contains("| Alice | 30 |"),
            "got: {}",
            result.text
        );
    }

    #[test]
    fn converts_table_pipe_escape() {
        let html = "<table><tr><td>a|b</td><td>c</td></tr></table>";
        let result = html_to_markdown(html);
        assert!(
            result.text.contains(r"a\|b"),
            "pipe should be escaped, got: {}",
            result.text
        );
    }

    #[test]
    fn converts_ordered_list() {
        let html = "<ol><li>first</li><li>second</li><li>third</li></ol>";
        let result = html_to_markdown(html);
        assert!(result.text.contains("1. first"), "got: {}", result.text);
        assert!(result.text.contains("2. second"), "got: {}", result.text);
        assert!(result.text.contains("3. third"), "got: {}", result.text);
    }

    #[test]
    fn mixed_ol_ul() {
        let html = "<ul><li>bullet</li></ul><ol><li>one</li><li>two</li></ol><ul><li>dot</li></ul>";
        let result = html_to_markdown(html);
        assert!(result.text.contains("- bullet"), "got: {}", result.text);
        assert!(result.text.contains("1. one"), "got: {}", result.text);
        assert!(result.text.contains("2. two"), "got: {}", result.text);
        assert!(result.text.contains("- dot"), "got: {}", result.text);
    }

    #[test]
    fn s_tag_boundary() {
        // <s> should not match <script>, <style>, <section>, <span>, <strong>.
        let html = "<p><span>text</span><s>struck</s><strong>bold</strong></p>";
        let result = html_to_markdown(html);
        assert!(result.text.contains("~~struck~~"), "got: {}", result.text);
        assert!(result.text.contains("**bold**"), "got: {}", result.text);
    }
}
