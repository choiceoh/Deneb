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
fn inline_styles() {
    let cases: &[(&str, &str, MarkdownStyle)] = &[
        ("**bold**", "bold", MarkdownStyle::Bold),
        ("*italic*", "italic", MarkdownStyle::Italic),
        ("~~strike~~", "strike", MarkdownStyle::Strikethrough),
    ];
    for (input, want_text, want_style) in cases {
        let ir = parse(input);
        assert_eq!(ir.text, *want_text, "input={input:?}");
        assert_eq!(ir.styles.len(), 1, "input={input:?}");
        assert_eq!(ir.styles[0].style, *want_style, "input={input:?}");
    }
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
    assert!(ir
        .styles
        .iter()
        .any(|s| s.style == MarkdownStyle::CodeBlock));
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
    assert!(ir.styles.iter().any(|s| s.style == MarkdownStyle::Spoiler));
}

#[test]
fn nested_styles() {
    let ir = parse("**bold *and italic***");
    assert!(ir.styles.iter().any(|s| s.style == MarkdownStyle::Bold));
    assert!(ir.styles.iter().any(|s| s.style == MarkdownStyle::Italic));
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
    assert!(
        ir.text.contains("Value: 1"),
        "expected 'Value: 1' in {:?}",
        ir.text
    );
    assert!(
        ir.text.contains("Value: 2"),
        "expected 'Value: 2' in {:?}",
        ir.text
    );
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
fn table_bullets_trimmed_cells() {
    let ir = parse_with(
        "| Name  | Value |\n|-------|-------|\n|  A    |   1   |",
        ParseOptions {
            table_mode: TableMode::Bullets,
            ..Default::default()
        },
    );
    assert!(
        ir.text.contains("Value: 1"),
        "expected trimmed value output, got {:?}",
        ir.text
    );
    assert!(!ir.text.contains("Value:   1"), "text={:?}", ir.text);
}

#[test]
fn table_bullets_skip_empty_value_cells() {
    let ir = parse_with(
        "| Name | Value |\n|------|-------|\n| A | |\n| B | 2 |",
        ParseOptions {
            table_mode: TableMode::Bullets,
            ..Default::default()
        },
    );
    assert!(ir.text.contains("B"));
    assert!(ir.text.contains("Value: 2"));
    assert!(!ir.text.contains("Value: \n"), "text={:?}", ir.text);
}

#[test]
fn table_bullets_fallback_column_name_when_header_empty() {
    let ir = parse_with(
        "| Name | |\n|------|--|\n| A | 1 |",
        ParseOptions {
            table_mode: TableMode::Bullets,
            ..Default::default()
        },
    );
    assert!(
        ir.text.contains("Column 1: 1"),
        "expected fallback column label, got {:?}",
        ir.text
    );
}

#[test]
fn table_bullets_preserve_links_and_styles() {
    let ir = parse_with(
        "| Name | Notes |\n|------|-------|\n| A | **[site](https://example.com)** |",
        ParseOptions {
            table_mode: TableMode::Bullets,
            ..Default::default()
        },
    );
    assert!(ir.text.contains("site"));
    assert_eq!(ir.links.len(), 1);
    assert_eq!(ir.links[0].href, "https://example.com");
    assert!(
        ir.styles.iter().any(|s| s.style == MarkdownStyle::Bold),
        "expected bold style spans for row labels or cell styles"
    );
}

#[test]
fn table_code_trims_and_aligns_columns() {
    let ir = parse_with(
        "| Name | Value |\n|------|-------|\n|  A   |   1   |\n| B | 22 |",
        ParseOptions {
            table_mode: TableMode::Code,
            ..Default::default()
        },
    );
    let expected = "| Name | Value |\n| ---- | ----- |\n| A    | 1     |\n| B    | 22    |\n";
    assert!(
        ir.text.contains(expected),
        "expected aligned table block, got {:?}",
        ir.text
    );
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
fn table_mode_off_does_not_report_table_meta() {
    let (_, has_tables) =
        markdown_to_ir_with_meta("| A |\n|---|\n| 1 |", &ParseOptions::default());
    assert!(!has_tables);
}

#[test]
fn has_tables_flag_in_code_mode() {
    let (_, has_tables) = markdown_to_ir_with_meta(
        "| A |\n|---|\n| 1 |",
        &ParseOptions {
            table_mode: TableMode::Code,
            ..Default::default()
        },
    );
    assert!(has_tables);
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
