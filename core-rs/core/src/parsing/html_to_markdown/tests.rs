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
fn decodes_extended_entities() {
    let html = "<p>&mdash; &ndash; &hellip; &laquo; &raquo; &copy; &reg; &trade; &bull; &middot;</p>";
    let result = html_to_markdown(html);
    assert!(result.text.contains('—'), "mdash: {}", result.text);
    assert!(result.text.contains('–'), "ndash: {}", result.text);
    assert!(result.text.contains('…'), "hellip: {}", result.text);
    assert!(result.text.contains('«'), "laquo: {}", result.text);
    assert!(result.text.contains('»'), "raquo: {}", result.text);
    assert!(result.text.contains('©'), "copy: {}", result.text);
    assert!(result.text.contains('®'), "reg: {}", result.text);
    assert!(result.text.contains('™'), "trade: {}", result.text);
    assert!(result.text.contains('•'), "bull: {}", result.text);
    assert!(result.text.contains('·'), "middot: {}", result.text);
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

/// Regression test for the X.com/Twitter HTML panic.
///
/// Production logs showed:
///   panicked at core/src/parsing/html_to_markdown.rs:295:21:
///   byte index 1374 is not a char boundary; it is inside '…' (bytes 1373..1376)
///
/// The input is Korean X.com HTML where the U+2026 ELLIPSIS character (3-byte
/// UTF-8: E2 80 A6) sits at a byte offset that the old 14-pass code tried to
/// slice through.
#[test]
fn xcom_korean_html_with_ellipsis_no_panic() {
    // Build HTML that places '…' (U+2026) near byte offset 1373, mimicking the
    // X.com production input that triggered the panic.
    let prefix = r#"<!doctype html><html dir="ltr" lang="ko"><head><meta charset="utf-8" /><meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1,user-scalable=0,viewport-fit=cover" />"#;
    // Pad with realistic-looking HTML to push the ellipsis near byte 1373.
    let padding_unit = r#"<meta http-equiv="origin-trial" content="AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" />"#;
    let mut html = String::from(prefix);
    while html.len() < 1370 {
        html.push_str(padding_unit);
    }
    // Place the multi-byte ellipsis character right around byte 1373.
    html.push_str("<title>AI 연구\u{2026}최신 뉴스</title>");
    html.push_str("</head><body>");
    html.push_str("<p>Anthropic\u{2026}Claude 연구</p>");
    html.push_str("<a href=\"https://example.com\">링크\u{2026}텍스트</a>");
    html.push_str("<li>\u{2026}목록</li>");
    html.push_str("</body></html>");

    let result = html_to_markdown(&html);
    // Must not panic. Verify content is extracted correctly.
    assert!(
        result.title.is_some(),
        "title should be extracted, got None"
    );
    assert!(
        result.text.contains("\u{2026}"),
        "ellipsis should be preserved, got: {}",
        result.text
    );
    assert!(
        result.text.contains("Claude"),
        "content should survive, got: {}",
        result.text
    );
}

/// Verify that the ellipsis character at EVERY possible byte alignment within a
/// tag does not cause panics or incorrect slicing.
#[test]
fn ellipsis_at_every_byte_alignment() {
    for padding in 0..4 {
        let prefix = "x".repeat(padding);
        let html = format!(
            "<p>{prefix}\u{2026}</p><li>{prefix}\u{2026}item</li><a href=\"u\">{prefix}\u{2026}link</a><h2>{prefix}\u{2026}head</h2><code>{prefix}\u{2026}code</code>"
        );
        let result = html_to_markdown(&html);
        assert!(
            result.text.contains("\u{2026}"),
            "failed at padding={padding}, got: {}",
            result.text
        );
    }
}

/// Large X.com-like HTML document with many multi-byte characters throughout.
/// Simulates the realistic size (~50KB) where the panic occurred in production.
#[test]
fn large_xcom_html_with_multibyte() {
    let mut html = String::from(
        r#"<!doctype html><html dir="ltr" lang="ko"><head><meta charset="utf-8" /><title>AI\u{2026}뉴스</title></head><body>"#,
    );
    // Build a large document with multi-byte characters at various positions.
    for i in 0..500 {
        html.push_str(&format!(
            "<div><p>섹션 {i}: 한국어 텍스트\u{2026}더 보기</p><a href=\"https://x.com/{i}\">링크\u{2026}</a></div>"
        ));
    }
    html.push_str("</body></html>");

    assert!(html.len() > 50_000, "document should be large");
    let result = html_to_markdown(&html);
    assert!(
        result.text.contains("섹션 0"),
        "first section should survive"
    );
    assert!(
        result.text.contains("섹션 499"),
        "last section should survive"
    );
    assert!(
        result.text.contains("\u{2026}"),
        "ellipsis should be preserved"
    );
}

// --- Conversion tests ---

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

// --- strip_noise option tests ---

#[test]
fn strip_noise_suppresses_nav() {
    let opts = super::HtmlToMarkdownOptions { strip_noise: true };
    let html = "<p>content</p><nav><a href='/'>Home</a><a href='/about'>About</a></nav><p>more</p>";
    let result = super::html_to_markdown_with_opts(html, &opts);
    assert!(result.text.contains("content"), "got: {}", result.text);
    assert!(result.text.contains("more"), "got: {}", result.text);
    assert!(!result.text.contains("Home"), "nav should be suppressed: {}", result.text);
    assert!(!result.text.contains("About"), "nav should be suppressed: {}", result.text);
}

#[test]
fn strip_noise_suppresses_aside_svg_iframe_form() {
    let opts = super::HtmlToMarkdownOptions { strip_noise: true };
    let html = "<p>keep</p><aside>sidebar</aside><svg><path/></svg><iframe src='x'>frame</iframe><form><input/></form><p>end</p>";
    let result = super::html_to_markdown_with_opts(html, &opts);
    assert!(result.text.contains("keep"), "got: {}", result.text);
    assert!(result.text.contains("end"), "got: {}", result.text);
    assert!(!result.text.contains("sidebar"), "aside suppressed: {}", result.text);
    assert!(!result.text.contains("frame"), "iframe suppressed: {}", result.text);
}

#[test]
fn strip_noise_off_preserves_nav() {
    // Default (strip_noise=false) should preserve nav content.
    let html = "<p>content</p><nav><a href='/'>Home</a></nav>";
    let result = html_to_markdown(html);
    assert!(result.text.contains("Home"), "nav should be preserved by default: {}", result.text);
}

#[test]
fn strip_noise_nested_nav() {
    let opts = super::HtmlToMarkdownOptions { strip_noise: true };
    let html = "<nav><ul><li><a href='/'>Home</a></li><li><a href='/about'>About</a></li></ul></nav><p>article content</p>";
    let result = super::html_to_markdown_with_opts(html, &opts);
    assert!(result.text.contains("article content"), "got: {}", result.text);
    assert!(!result.text.contains("Home"), "nested nav suppressed: {}", result.text);
}

// --- Extended entity tests ---

#[test]
fn decodes_typography_entities() {
    let html = "<p>&mdash; &ndash; &hellip; &laquo;text&raquo; &lsquo;x&rsquo; &ldquo;y&rdquo;</p>";
    let result = html_to_markdown(html);
    assert!(result.text.contains('\u{2014}'), "mdash missing: {}", result.text);
    assert!(result.text.contains('\u{2013}'), "ndash missing: {}", result.text);
    assert!(result.text.contains('\u{2026}'), "hellip missing: {}", result.text);
    assert!(result.text.contains('\u{00AB}'), "laquo missing: {}", result.text);
    assert!(result.text.contains('\u{00BB}'), "raquo missing: {}", result.text);
    assert!(result.text.contains('\u{2018}'), "lsquo missing: {}", result.text);
    assert!(result.text.contains('\u{2019}'), "rsquo missing: {}", result.text);
    assert!(result.text.contains('\u{201C}'), "ldquo missing: {}", result.text);
    assert!(result.text.contains('\u{201D}'), "rdquo missing: {}", result.text);
}

#[test]
fn decodes_symbol_entities() {
    let html = "<p>&copy; &reg; &trade; &deg; &euro; &pound;</p>";
    let result = html_to_markdown(html);
    assert!(result.text.contains('\u{00A9}'), "copy: {}", result.text);
    assert!(result.text.contains('\u{00AE}'), "reg: {}", result.text);
    assert!(result.text.contains('\u{2122}'), "trade: {}", result.text);
    assert!(result.text.contains('\u{00B0}'), "deg: {}", result.text);
    assert!(result.text.contains('\u{20AC}'), "euro: {}", result.text);
    assert!(result.text.contains('\u{00A3}'), "pound: {}", result.text);
}

#[test]
fn table_escapes_backslash() {
    let html = "<table><tr><td>a\\b</td><td>c</td></tr></table>";
    let result = html_to_markdown(html);
    assert!(
        result.text.contains(r"a\\b"),
        "backslash should be escaped, got: {}",
        result.text
    );
}
