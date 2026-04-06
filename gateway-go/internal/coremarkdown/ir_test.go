package coremarkdown

import (
	"strings"
	"testing"
)

func parse(md string) MarkdownIR {
	return MarkdownToIR(md, nil)
}

func parseWith(md string, opts *ParseOptions) MarkdownIR {
	return MarkdownToIR(md, opts)
}

// ---------------------------------------------------------------------------
// Basic text
// ---------------------------------------------------------------------------

func TestPlainText(t *testing.T) {
	ir := parse("hello world")
	if ir.Text != "hello world" {
		t.Errorf("got %q", ir.Text)
	}
	if len(ir.Styles) != 0 {
		t.Errorf("expected no styles, got %d", len(ir.Styles))
	}
	if len(ir.Links) != 0 {
		t.Errorf("expected no links, got %d", len(ir.Links))
	}
}

func TestEmptyInput(t *testing.T) {
	ir := parse("")
	if ir.Text != "" {
		t.Errorf("expected empty text, got %q", ir.Text)
	}
}

// ---------------------------------------------------------------------------
// Inline styles
// ---------------------------------------------------------------------------

func TestBold(t *testing.T) {
	ir := parse("**bold**")
	if !strings.Contains(ir.Text, "bold") {
		t.Errorf("expected 'bold' in text, got %q", ir.Text)
	}
	assertHasStyle(t, ir, StyleBold)
}

func TestItalic(t *testing.T) {
	ir := parse("*italic*")
	if !strings.Contains(ir.Text, "italic") {
		t.Errorf("expected 'italic' in text, got %q", ir.Text)
	}
	assertHasStyle(t, ir, StyleItalic)
}

func TestStrikethrough(t *testing.T) {
	ir := parse("~~strike~~")
	if !strings.Contains(ir.Text, "strike") {
		t.Errorf("expected 'strike' in text, got %q", ir.Text)
	}
	assertHasStyle(t, ir, StyleStrikethrough)
}

func TestInlineCode(t *testing.T) {
	ir := parse("use `code` here")
	if !strings.Contains(ir.Text, "code") {
		t.Errorf("expected 'code' in text, got %q", ir.Text)
	}
	found := false
	for _, s := range ir.Styles {
		if s.Style == StyleCode {
			if ir.Text[s.Start:s.End] != "code" {
				t.Errorf("code span text = %q, want 'code'", ir.Text[s.Start:s.End])
			}
			found = true
		}
	}
	if !found {
		t.Error("expected Code style span")
	}
}

func TestNestedStyles(t *testing.T) {
	ir := parse("**bold *and italic***")
	assertHasStyle(t, ir, StyleBold)
	assertHasStyle(t, ir, StyleItalic)
}

// ---------------------------------------------------------------------------
// Code blocks
// ---------------------------------------------------------------------------

func TestCodeBlock(t *testing.T) {
	ir := parse("```\ncode\n```")
	if !strings.Contains(ir.Text, "code") {
		t.Errorf("expected 'code' in text, got %q", ir.Text)
	}
	assertHasStyle(t, ir, StyleCodeBlock)
}

func TestCodeBlockWithLanguage(t *testing.T) {
	ir := parse("```go\nfmt.Println()\n```")
	if !strings.Contains(ir.Text, "fmt.Println()") {
		t.Errorf("expected code content, got %q", ir.Text)
	}
	assertHasStyle(t, ir, StyleCodeBlock)
}

// ---------------------------------------------------------------------------
// Links
// ---------------------------------------------------------------------------

func TestLink(t *testing.T) {
	ir := parse("[click](https://example.com)")
	if !strings.Contains(ir.Text, "click") {
		t.Errorf("expected 'click' in text, got %q", ir.Text)
	}
	if strings.Contains(ir.Text, "https://") {
		t.Error("URL should not appear in text")
	}
	if len(ir.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(ir.Links))
	}
	if ir.Links[0].Href != "https://example.com" {
		t.Errorf("href = %q", ir.Links[0].Href)
	}
	if ir.Links[0].Start != 0 || ir.Links[0].End != 5 {
		t.Errorf("link span = [%d,%d), want [0,5)", ir.Links[0].Start, ir.Links[0].End)
	}
}

// ---------------------------------------------------------------------------
// Headings
// ---------------------------------------------------------------------------

func TestHeadingNone(t *testing.T) {
	ir := parse("# Title")
	if !strings.Contains(strings.TrimSpace(ir.Text), "Title") {
		t.Errorf("expected 'Title', got %q", ir.Text)
	}
	if len(ir.Styles) != 0 {
		t.Error("heading_style=none should produce no styles")
	}
}

func TestHeadingBold(t *testing.T) {
	opts := DefaultParseOptions()
	opts.HeadingStyle = "bold"
	ir := parseWith("# Title", &opts)
	if !strings.Contains(ir.Text, "Title") {
		t.Errorf("expected 'Title', got %q", ir.Text)
	}
	assertHasStyle(t, ir, StyleBold)
}

func TestHeadingStripped(t *testing.T) {
	ir := parse("# Heading 1\n## Heading 2\n### Heading 3")
	if strings.Contains(ir.Text, "# ") {
		t.Errorf("heading markers should be stripped, got %q", ir.Text)
	}
}

// ---------------------------------------------------------------------------
// Lists
// ---------------------------------------------------------------------------

func TestBulletList(t *testing.T) {
	ir := parse("- a\n- b")
	if !strings.Contains(ir.Text, "• a") {
		t.Errorf("expected '• a', got %q", ir.Text)
	}
	if !strings.Contains(ir.Text, "• b") {
		t.Errorf("expected '• b', got %q", ir.Text)
	}
}

func TestOrderedList(t *testing.T) {
	ir := parse("1. first\n2. second")
	if !strings.Contains(ir.Text, "1. first") {
		t.Errorf("expected '1. first', got %q", ir.Text)
	}
	if !strings.Contains(ir.Text, "2. second") {
		t.Errorf("expected '2. second', got %q", ir.Text)
	}
}

func TestNestedList(t *testing.T) {
	ir := parse("- a\n  - b\n  - c\n- d")
	if !strings.Contains(ir.Text, "• a") {
		t.Errorf("expected '• a', got %q", ir.Text)
	}
	if !strings.Contains(ir.Text, "• b") {
		t.Errorf("expected '• b', got %q", ir.Text)
	}
	if !strings.Contains(ir.Text, "• d") {
		t.Errorf("expected '• d', got %q", ir.Text)
	}
}

// ---------------------------------------------------------------------------
// Blockquote
// ---------------------------------------------------------------------------

func TestBlockquote(t *testing.T) {
	ir := parse("> quoted")
	if !strings.Contains(ir.Text, "quoted") {
		t.Errorf("expected 'quoted', got %q", ir.Text)
	}
	assertHasStyle(t, ir, StyleBlockquote)
}

func TestBlockquotePrefix(t *testing.T) {
	opts := DefaultParseOptions()
	opts.BlockquotePrefix = "> "
	ir := parseWith("> text", &opts)
	if !strings.HasPrefix(ir.Text, "> ") {
		t.Errorf("expected prefix '> ', got %q", ir.Text)
	}
}

// ---------------------------------------------------------------------------
// Horizontal rule
// ---------------------------------------------------------------------------

func TestHorizontalRule(t *testing.T) {
	ir := parse("---")
	if !strings.Contains(ir.Text, "───") {
		t.Errorf("expected '───', got %q", ir.Text)
	}
}

// ---------------------------------------------------------------------------
// Paragraphs and breaks
// ---------------------------------------------------------------------------

func TestParagraphsSeparated(t *testing.T) {
	ir := parse("first\n\nsecond")
	if !strings.Contains(ir.Text, "\n\n") {
		t.Error("expected paragraph separator")
	}
}

func TestSoftBreak(t *testing.T) {
	ir := parse("line1\nline2")
	if !strings.Contains(ir.Text, "line1") || !strings.Contains(ir.Text, "line2") {
		t.Errorf("expected both lines, got %q", ir.Text)
	}
}

// ---------------------------------------------------------------------------
// Spoilers
// ---------------------------------------------------------------------------

func TestSpoiler(t *testing.T) {
	opts := DefaultParseOptions()
	opts.EnableSpoilers = true
	ir := parseWith("||hidden||", &opts)
	trimmed := strings.TrimSpace(ir.Text)
	if trimmed != "hidden" {
		t.Errorf("expected 'hidden', got %q", trimmed)
	}
	assertHasStyle(t, ir, StyleSpoiler)
}

func TestSpoilerWithSurroundingText(t *testing.T) {
	opts := DefaultParseOptions()
	opts.EnableSpoilers = true
	ir := parseWith("before ||hidden|| after", &opts)
	if !strings.Contains(ir.Text, "before") {
		t.Error("expected 'before'")
	}
	if !strings.Contains(ir.Text, "hidden") {
		t.Error("expected 'hidden'")
	}
	if !strings.Contains(ir.Text, "after") {
		t.Error("expected 'after'")
	}
	// Should NOT contain sentinel markers.
	if strings.Contains(ir.Text, "SPOILER") {
		t.Errorf("sentinel leaked into text: %q", ir.Text)
	}
	assertHasStyle(t, ir, StyleSpoiler)
}

func TestTwoSpoilers(t *testing.T) {
	opts := DefaultParseOptions()
	opts.EnableSpoilers = true
	ir := parseWith("||a|| and ||b||", &opts)
	spoilerCount := 0
	for _, s := range ir.Styles {
		if s.Style == StyleSpoiler {
			spoilerCount++
		}
	}
	if spoilerCount != 2 {
		t.Errorf("expected 2 spoiler spans, got %d", spoilerCount)
	}
}

func TestSpoilerUnicode(t *testing.T) {
	opts := DefaultParseOptions()
	opts.EnableSpoilers = true
	ir := parseWith("||안녕하세요||", &opts)
	if !strings.Contains(ir.Text, "안녕하세요") {
		t.Error("expected Korean text preserved")
	}
	assertHasStyle(t, ir, StyleSpoiler)
}

func TestSpoilerDisabledByDefault(t *testing.T) {
	ir := parse("||not spoiler||")
	// Without enableSpoilers, || should pass through.
	if strings.Contains(ir.Text, "SPOILER") {
		t.Error("sentinels should not appear without enableSpoilers")
	}
}

// ---------------------------------------------------------------------------
// Tables
// ---------------------------------------------------------------------------

func TestTableBullets(t *testing.T) {
	opts := DefaultParseOptions()
	opts.TableMode = "bullets"
	ir := parseWith("| A | B |\n|---|---|\n| 1 | 2 |", &opts)
	if !strings.Contains(ir.Text, "1") || !strings.Contains(ir.Text, "2") {
		t.Errorf("expected cell content, got %q", ir.Text)
	}
}

func TestTableCode(t *testing.T) {
	opts := DefaultParseOptions()
	opts.TableMode = "code"
	ir := parseWith("| A | B |\n|---|---|\n| 1 | 2 |", &opts)
	if !strings.Contains(ir.Text, "|") {
		t.Errorf("expected pipe delimiters, got %q", ir.Text)
	}
	if !strings.Contains(ir.Text, "---") {
		t.Errorf("expected divider, got %q", ir.Text)
	}
	assertHasStyle(t, ir, StyleCodeBlock)
}

func TestTableBulletsHeaderValue(t *testing.T) {
	opts := DefaultParseOptions()
	opts.TableMode = "bullets"
	ir := parseWith("| Name | Value |\n|------|-------|\n| A | 1 |\n| B | 2 |", &opts)
	if !strings.Contains(ir.Text, "Value: 1") {
		t.Errorf("expected 'Value: 1', got %q", ir.Text)
	}
	if !strings.Contains(ir.Text, "Value: 2") {
		t.Errorf("expected 'Value: 2', got %q", ir.Text)
	}
}

func TestTableModeOff(t *testing.T) {
	_, hasTables := MarkdownToIRWithMeta("| A |\n|---|\n| 1 |", nil)
	if hasTables {
		t.Error("tableMode=off should not report hasTables")
	}
}

func TestHasTablesFlag(t *testing.T) {
	opts := DefaultParseOptions()
	opts.TableMode = "bullets"
	_, hasTables := MarkdownToIRWithMeta("| A |\n|---|\n| 1 |", &opts)
	if !hasTables {
		t.Error("expected hasTables=true")
	}
}

// ---------------------------------------------------------------------------
// Image alt text
// ---------------------------------------------------------------------------

func TestImageAltText(t *testing.T) {
	ir := parse("![alt text](img.png)")
	if !strings.Contains(ir.Text, "alt text") {
		t.Errorf("expected alt text, got %q", ir.Text)
	}
}

// ---------------------------------------------------------------------------
// IROutput JSON marshaling
// ---------------------------------------------------------------------------

func TestMarshalIROutput_NilSlices(t *testing.T) {
	out := &IROutput{Text: "test"}
	data, err := MarshalIROutput(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "null") {
		t.Errorf("nil slices should marshal as [], got %s", s)
	}
}

// ---------------------------------------------------------------------------
// Span utilities
// ---------------------------------------------------------------------------

func TestMergeAdjacentSameStyle(t *testing.T) {
	spans := []StyleSpan{
		{Start: 0, End: 5, Style: StyleBold},
		{Start: 5, End: 10, Style: StyleBold},
	}
	merged := mergeStyleSpans(spans)
	if len(merged) != 1 {
		t.Fatalf("expected 1 span, got %d", len(merged))
	}
	if merged[0].End != 10 {
		t.Errorf("expected end=10, got %d", merged[0].End)
	}
}

func TestMergeOverlapping(t *testing.T) {
	spans := []StyleSpan{
		{Start: 0, End: 7, Style: StyleItalic},
		{Start: 5, End: 12, Style: StyleItalic},
	}
	merged := mergeStyleSpans(spans)
	if len(merged) != 1 {
		t.Fatalf("expected 1 span, got %d", len(merged))
	}
	if merged[0].End != 12 {
		t.Errorf("expected end=12, got %d", merged[0].End)
	}
}

func TestMergeDifferentStylesNotMerged(t *testing.T) {
	spans := []StyleSpan{
		{Start: 0, End: 5, Style: StyleBold},
		{Start: 5, End: 10, Style: StyleItalic},
	}
	merged := mergeStyleSpans(spans)
	if len(merged) != 2 {
		t.Errorf("expected 2 spans, got %d", len(merged))
	}
}

func TestMergeBlockquoteAdjacentNotMerged(t *testing.T) {
	spans := []StyleSpan{
		{Start: 0, End: 5, Style: StyleBlockquote},
		{Start: 5, End: 10, Style: StyleBlockquote},
	}
	merged := mergeStyleSpans(spans)
	if len(merged) != 2 {
		t.Errorf("adjacent blockquotes must not merge, got %d", len(merged))
	}
}

func TestMergeBlockquoteOverlappingMerged(t *testing.T) {
	spans := []StyleSpan{
		{Start: 0, End: 6, Style: StyleBlockquote},
		{Start: 5, End: 10, Style: StyleBlockquote},
	}
	merged := mergeStyleSpans(spans)
	if len(merged) != 1 {
		t.Fatalf("overlapping blockquotes should merge, got %d", len(merged))
	}
	if merged[0].End != 10 {
		t.Errorf("expected end=10, got %d", merged[0].End)
	}
}

func TestClampStyleDropsEmpty(t *testing.T) {
	spans := []StyleSpan{{Start: 15, End: 20, Style: StyleBold}}
	clamped := clampStyleSpans(spans, 10)
	if len(clamped) != 0 {
		t.Errorf("expected 0 spans, got %d", len(clamped))
	}
}

func TestClampStyleClips(t *testing.T) {
	spans := []StyleSpan{{Start: 5, End: 20, Style: StyleBold}}
	clamped := clampStyleSpans(spans, 10)
	if len(clamped) != 1 {
		t.Fatalf("expected 1 span, got %d", len(clamped))
	}
	if clamped[0].End != 10 {
		t.Errorf("expected end=10, got %d", clamped[0].End)
	}
}

// ---------------------------------------------------------------------------
// PlainText convenience
// ---------------------------------------------------------------------------

func TestToPlainText(t *testing.T) {
	result := ToPlainText("**bold** and [link](https://example.com)")
	if !strings.Contains(result, "bold") {
		t.Errorf("expected 'bold', got %q", result)
	}
	if strings.Contains(result, "**") {
		t.Error("bold markers should be stripped")
	}
	if strings.Contains(result, "https://") {
		t.Error("URL should be stripped")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func assertHasStyle(t *testing.T, ir MarkdownIR, style MarkdownStyle) {
	t.Helper()
	for _, s := range ir.Styles {
		if s.Style == style {
			return
		}
	}
	t.Errorf("expected style %q in %v", style, ir.Styles)
}
