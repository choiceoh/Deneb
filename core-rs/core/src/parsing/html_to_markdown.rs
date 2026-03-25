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
    let title = extract_title(html);

    let mut text = String::with_capacity(html.len());
    text.push_str(html);

    // 1. Strip <script>, <style>, <noscript> blocks (case-insensitive).
    text = strip_tag_block(&text, "script");
    text = strip_tag_block(&text, "style");
    text = strip_tag_block(&text, "noscript");

    // 2. Convert <a href="X">label</a> → [label](X).
    text = convert_links(&text);

    // 3. Convert <h1-6>...</h1-6> → # prefix.
    text = convert_headings(&text);

    // 4. Convert <li>...</li> → "- " prefix.
    text = convert_list_items(&text);

    // 5. <br>, <hr> → newline; closing block tags → newline.
    text = convert_breaks(&text);

    // 6. Strip all remaining HTML tags.
    text = strip_tags(&text);

    // 7. Decode HTML entities.
    text = decode_entities(&text);

    // 8. Normalize whitespace.
    text = normalize_whitespace(&text);

    HtmlToMarkdownResult { text, title }
}

/// Extract content from `<title>...</title>` tag.
fn extract_title(html: &str) -> Option<String> {
    let lower = html.to_ascii_lowercase();
    let start_tag = "<title";
    let start_idx = lower.find(start_tag)?;
    // Find the closing > of the opening tag.
    let after_tag = lower[start_idx..].find('>')? + start_idx + 1;
    let end_tag = "</title>";
    let end_idx = lower[after_tag..].find(end_tag)? + after_tag;
    let raw = &html[after_tag..end_idx];
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
    let open_prefix = format!("<{}", tag);
    let close_tag = format!("</{}>", tag);

    let mut result = String::with_capacity(input.len());
    let mut cursor = 0;

    while let Some(rel_start) = lower[cursor..].find(&open_prefix) {
        let start = cursor + rel_start;
        // Make sure it's actually a tag boundary (next char is whitespace, > or end).
        let after = start + open_prefix.len();
        if after < input.len() {
            let next = input.as_bytes()[after];
            if next != b'>' && next != b' ' && next != b'\t' && next != b'\n' && next != b'\r' {
                result.push_str(&input[cursor..after]);
                cursor = after;
                continue;
            }
        }
        result.push_str(&input[cursor..start]);
        // Find closing tag.
        if let Some(rel_end) = lower[start..].find(&close_tag) {
            cursor = start + rel_end + close_tag.len();
        } else {
            // No closing tag — strip to end.
            cursor = input.len();
        }
    }
    result.push_str(&input[cursor..]);
    result
}

/// Convert `<a href="X">label</a>` to `[label](X)`.
fn convert_links(input: &str) -> String {
    let lower = input.to_ascii_lowercase();
    let mut result = String::with_capacity(input.len());
    let mut cursor = 0;

    while let Some(rel_start) = lower[cursor..].find("<a ") {
        let start = cursor + rel_start;
        result.push_str(&input[cursor..start]);

        // Find the closing > of the <a> tag.
        let tag_end = match input[start..].find('>') {
            Some(e) => start + e + 1,
            None => {
                result.push_str(&input[start..start + 3]);
                cursor = start + 3;
                continue;
            }
        };

        // Extract href from the tag.
        let tag_content = &input[start..tag_end];
        let href = extract_attr(tag_content, "href");

        // Find </a>.
        let close = "</a>";
        let body_end = match lower[tag_end..].find(close) {
            Some(e) => tag_end + e,
            None => {
                if let Some(h) = &href {
                    result.push_str(h);
                }
                cursor = tag_end;
                continue;
            }
        };

        let body = &input[tag_end..body_end];
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
    result.push_str(&input[cursor..]);
    result
}

/// Extract an attribute value from a tag string like `<a href="value">`.
fn extract_attr(tag: &str, attr: &str) -> Option<String> {
    let lower = tag.to_ascii_lowercase();
    let pattern = format!("{}=", attr);
    let idx = lower.find(&pattern)?;
    let after_eq = idx + pattern.len();
    let bytes = tag.as_bytes();
    if after_eq >= bytes.len() {
        return None;
    }
    let quote = bytes[after_eq];
    if quote == b'"' || quote == b'\'' {
        let start = after_eq + 1;
        let end = tag[start..].find(quote as char).map(|e| start + e)?;
        Some(tag[start..end].to_string())
    } else {
        // Unquoted attribute: read until whitespace or >.
        let start = after_eq;
        let end = tag[start..]
            .find(|c: char| c.is_ascii_whitespace() || c == '>')
            .map(|e| start + e)
            .unwrap_or(tag.len());
        Some(tag[start..end].to_string())
    }
}

/// Convert `<h1>...</h1>` through `<h6>...</h6>` to markdown headings.
fn convert_headings(input: &str) -> String {
    let lower = input.to_ascii_lowercase();
    let mut result = String::with_capacity(input.len());
    let mut cursor = 0;

    while cursor < input.len() {
        if let Some(rel) = lower[cursor..].find("<h") {
            let start = cursor + rel;
            let after_h = start + 2;
            if after_h < input.len() {
                let level_byte = input.as_bytes()[after_h];
                if level_byte >= b'1' && level_byte <= b'6' {
                    let level = (level_byte - b'0') as usize;
                    // Find closing > of opening tag.
                    if let Some(gt) = input[after_h..].find('>') {
                        let body_start = after_h + gt + 1;
                        let close = format!("</h{}>", level);
                        if let Some(rel_close) = lower[body_start..].find(&close) {
                            let body_end = body_start + rel_close;
                            result.push_str(&input[cursor..start]);
                            let body = &input[body_start..body_end];
                            let label = normalize_whitespace(&strip_tags(body));
                            let prefix = "#".repeat(level.min(6).max(1));
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
            result.push_str(&input[cursor..start + 1]);
            cursor = start + 1;
        } else {
            break;
        }
    }
    result.push_str(&input[cursor..]);
    result
}

/// Convert `<li>...</li>` to markdown list items.
fn convert_list_items(input: &str) -> String {
    let lower = input.to_ascii_lowercase();
    let mut result = String::with_capacity(input.len());
    let mut cursor = 0;

    while let Some(rel) = lower[cursor..].find("<li") {
        let start = cursor + rel;
        result.push_str(&input[cursor..start]);

        // Find closing > of <li> tag.
        let tag_end = match input[start..].find('>') {
            Some(e) => start + e + 1,
            None => {
                cursor = start + 3;
                continue;
            }
        };

        let close = "</li>";
        let body_end = match lower[tag_end..].find(close) {
            Some(e) => tag_end + e,
            None => {
                cursor = tag_end;
                continue;
            }
        };

        let body = &input[tag_end..body_end];
        let label = normalize_whitespace(&strip_tags(body));
        if !label.is_empty() {
            result.push_str("\n- ");
            result.push_str(&label);
        }
        cursor = body_end + close.len();
    }
    result.push_str(&input[cursor..]);
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
        let mut matched = false;

        // Check br/hr tags.
        for tag in &br_hr_tags {
            if lower[cursor..].starts_with(tag) {
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
            if lower[cursor..].starts_with(tag) {
                result.push('\n');
                cursor += tag.len();
                matched = true;
                break;
            }
        }
        if matched {
            continue;
        }

        result.push(input.as_bytes()[cursor] as char);
        cursor += 1;
    }
    result
}

/// Strip all HTML tags.
fn strip_tags(input: &str) -> String {
    let mut result = String::with_capacity(input.len());
    let mut in_tag = false;
    for ch in input.chars() {
        if ch == '<' {
            in_tag = true;
        } else if ch == '>' {
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
            result.push(bytes[i] as char);
            i += 1;
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
    let rest = &input[pos..];

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
    let rest_lower = rest.get(..10).unwrap_or(rest).to_ascii_lowercase();
    for &(entity, ch) in named {
        if rest_lower.starts_with(entity) {
            return Some((ch, entity.len()));
        }
    }

    // Hex numeric: &#xHH;
    if rest_lower.starts_with("&#x") {
        let after = &rest[3..];
        if let Some(semi) = after.find(';') {
            let hex_str = &after[..semi];
            if let Ok(code) = u32::from_str_radix(hex_str, 16) {
                if let Some(ch) = char::from_u32(code) {
                    return Some((ch, 3 + semi + 1));
                }
            }
        }
    }

    // Decimal numeric: &#DDD;
    if rest.starts_with("&#") {
        let after = &rest[2..];
        if let Some(semi) = after.find(';') {
            let dec_str = &after[..semi];
            if let Ok(code) = dec_str.parse::<u32>() {
                if let Some(ch) = char::from_u32(code) {
                    return Some((ch, 2 + semi + 1));
                }
            }
        }
    }

    None
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
        assert_eq!(result.text, "Hello world");
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
    fn empty_input() {
        let result = html_to_markdown("");
        assert_eq!(result.text, "");
        assert!(result.title.is_none());
    }
}
