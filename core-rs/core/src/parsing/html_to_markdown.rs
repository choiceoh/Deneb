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
    let title = {
        let h = html.to_owned();
        std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| extract_title(&h)))
            .unwrap_or(None)
    };

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
        // 3. Convert <h1-6>...</h1-6> → # prefix.
        convert_headings,
        // 4. Convert <li>...</li> → "- " prefix.
        convert_list_items,
        // 5. <br>, <hr> → newline; closing block tags → newline.
        convert_breaks,
        // 6. Strip all remaining HTML tags.
        strip_tags,
        // 7. Decode HTML entities.
        decode_entities,
        // 8. Normalize whitespace.
        normalize_whitespace,
    ];

    for step in steps {
        let input = text.clone();
        match std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| step(&input))) {
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
    let open_prefix = format!("<{}", tag);
    let close_tag = format!("</{}>", tag);

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
        let end = tag.get(start..)?.find(quote as char).map(|e| start + e)?;
        Some(tag.get(start..end)?.to_string())
    } else {
        // Unquoted attribute: read until whitespace or >.
        let start = after_eq;
        let rest = tag.get(start..)?;
        let end = rest
            .find(|c: char| c.is_ascii_whitespace() || c == '>')
            .map(|e| start + e)
            .unwrap_or(tag.len());
        Some(tag.get(start..end)?.to_string())
    }
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
                        let close = format!("</h{}>", level);
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

/// Convert `<li>...</li>` to markdown list items.
fn convert_list_items(input: &str) -> String {
    let lower = input.to_ascii_lowercase();
    let mut result = String::with_capacity(input.len());
    let mut cursor = 0;

    while let Some(rel) = lower.get(cursor..).and_then(|s| s.find("<li")) {
        let start = cursor + rel;
        if let Some(s) = input.get(cursor..start) {
            result.push_str(s);
        }

        // Find closing > of <li> tag.
        let tag_end = match input.get(start..).and_then(|s| s.find('>')) {
            Some(e) => start + e + 1,
            None => {
                cursor = start + 3;
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
            result.push_str("\n- ");
            result.push_str(&label);
        }
        cursor = body_end + close.len();
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
    let rest = match input.get(pos..) {
        Some(s) => s,
        None => return None,
    };

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
            "<h", "<h1", "<h1>", "<a ", "<a href=", "<li", "<li>",
            "<br", "<hr", "</p", "<script", "<style", "<noscript",
            "&", "&amp", "&#", "&#x", "&#x4", "&#39",
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
        assert!(result.text.contains("[한국어 링크](https://example.com/한국)"));
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
            assert!(result.text.contains("\u{2019}"), "failed at padding={padding}");
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
        let mut html = String::from(r#"<!doctype html><html style="font-size: 10px;font-family: roboto, arial, sans-serif;" lang="ko-kr"><head><title>테스트 동영상</title></head><body>"#);
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
}
