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
		t.Errorf("got %d, want no styles", len(ir.Styles))
	}
	if len(ir.Links) != 0 {
		t.Errorf("got %d, want no links", len(ir.Links))
	}
}

// ---------------------------------------------------------------------------
// Inline styles
// ---------------------------------------------------------------------------

func TestBold(t *testing.T) {
	ir := parse("**bold**")
	if !strings.Contains(ir.Text, "bold") {
		t.Errorf("got %q, want 'bold' in text", ir.Text)
	}
	assertHasStyle(t, ir, StyleBold)
}

func TestItalic(t *testing.T) {
	ir := parse("*italic*")
	if !strings.Contains(ir.Text, "italic") {
		t.Errorf("got %q, want 'italic' in text", ir.Text)
	}
	assertHasStyle(t, ir, StyleItalic)
}

func TestStrikethrough(t *testing.T) {
	ir := parse("~~strike~~")
	if !strings.Contains(ir.Text, "strike") {
		t.Errorf("got %q, want 'strike' in text", ir.Text)
	}
	assertHasStyle(t, ir, StyleStrikethrough)
}

func TestInlineCode(t *testing.T) {
	ir := parse("use `code` here")
	if !strings.Contains(ir.Text, "code") {
		t.Errorf("got %q, want 'code' in text", ir.Text)
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
		t.Errorf("got %q, want 'code' in text", ir.Text)
	}
	assertHasStyle(t, ir, StyleCodeBlock)
}

// ---------------------------------------------------------------------------
// Links
// ---------------------------------------------------------------------------

func TestLink(t *testing.T) {
	ir := parse("[click](https://example.com)")
	if !strings.Contains(ir.Text, "click") {
		t.Errorf("got %q, want 'click' in text", ir.Text)
	}
	if strings.Contains(ir.Text, "https://") {
		t.Error("URL should not appear in text")
	}
	if len(ir.Links) != 1 {
		t.Fatalf("got %d, want 1 link", len(ir.Links))
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
		t.Errorf("got %q, want 'Title'", ir.Text)
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
		t.Errorf("got %q, want 'Title'", ir.Text)
	}
	assertHasStyle(t, ir, StyleBold)
}

// ---------------------------------------------------------------------------
// Lists
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Blockquote
// ---------------------------------------------------------------------------

func TestBlockquote(t *testing.T) {
	ir := parse("> quoted")
	if !strings.Contains(ir.Text, "quoted") {
		t.Errorf("got %q, want 'quoted'", ir.Text)
	}
	assertHasStyle(t, ir, StyleBlockquote)
}

// ---------------------------------------------------------------------------
// Horizontal rule
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Paragraphs and breaks
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Spoilers
// ---------------------------------------------------------------------------

func TestSpoiler(t *testing.T) {
	opts := DefaultParseOptions()
	opts.EnableSpoilers = true
	ir := parseWith("||hidden||", &opts)
	trimmed := strings.TrimSpace(ir.Text)
	if trimmed != "hidden" {
		t.Errorf("got %q, want 'hidden'", trimmed)
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
		t.Errorf("got %d, want 2 spoiler spans", spoilerCount)
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

// ---------------------------------------------------------------------------
// Tables
// ---------------------------------------------------------------------------

func TestTableBullets(t *testing.T) {
	opts := DefaultParseOptions()
	opts.TableMode = "bullets"
	ir := parseWith("| A | B |\n|---|---|\n| 1 | 2 |", &opts)
	if !strings.Contains(ir.Text, "1") || !strings.Contains(ir.Text, "2") {
		t.Errorf("got %q, want cell content", ir.Text)
	}
}

func TestTableCode(t *testing.T) {
	opts := DefaultParseOptions()
	opts.TableMode = "code"
	ir := parseWith("| A | B |\n|---|---|\n| 1 | 2 |", &opts)
	if !strings.Contains(ir.Text, "|") {
		t.Errorf("got %q, want pipe delimiters", ir.Text)
	}
	if !strings.Contains(ir.Text, "---") {
		t.Errorf("got %q, want divider", ir.Text)
	}
	assertHasStyle(t, ir, StyleCodeBlock)
}

// ---------------------------------------------------------------------------
// Image alt text
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// IROutput JSON marshaling
// ---------------------------------------------------------------------------

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
		t.Fatalf("got %d, want 1 span", len(merged))
	}
	if merged[0].End != 10 {
		t.Errorf("got %d, want end=10", merged[0].End)
	}
}

func TestMergeOverlapping(t *testing.T) {
	spans := []StyleSpan{
		{Start: 0, End: 7, Style: StyleItalic},
		{Start: 5, End: 12, Style: StyleItalic},
	}
	merged := mergeStyleSpans(spans)
	if len(merged) != 1 {
		t.Fatalf("got %d, want 1 span", len(merged))
	}
	if merged[0].End != 12 {
		t.Errorf("got %d, want end=12", merged[0].End)
	}
}

func TestMergeDifferentStylesNotMerged(t *testing.T) {
	spans := []StyleSpan{
		{Start: 0, End: 5, Style: StyleBold},
		{Start: 5, End: 10, Style: StyleItalic},
	}
	merged := mergeStyleSpans(spans)
	if len(merged) != 2 {
		t.Errorf("got %d, want 2 spans", len(merged))
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
		t.Errorf("got %d, want end=10", merged[0].End)
	}
}

func TestClampStyleDropsEmpty(t *testing.T) {
	spans := []StyleSpan{{Start: 15, End: 20, Style: StyleBold}}
	clamped := clampStyleSpans(spans, 10)
	if len(clamped) != 0 {
		t.Errorf("got %d, want 0 spans", len(clamped))
	}
}

func TestClampStyleClips(t *testing.T) {
	spans := []StyleSpan{{Start: 5, End: 20, Style: StyleBold}}
	clamped := clampStyleSpans(spans, 10)
	if len(clamped) != 1 {
		t.Fatalf("got %d, want 1 span", len(clamped))
	}
	if clamped[0].End != 10 {
		t.Errorf("got %d, want end=10", clamped[0].End)
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
