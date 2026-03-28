package telegram

import (
	"strings"
	"testing"
)

func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"a < b & c > d", "a &lt; b &amp; c &gt; d"},
		{"<script>alert('xss')</script>", "&lt;script&gt;alert('xss')&lt;/script&gt;"},
		{"", ""},
	}
	for _, tt := range tests {
		got := escapeHTML(tt.in)
		if got != tt.want {
			t.Errorf("escapeHTML(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatHTML_Bold(t *testing.T) {
	got := FormatHTML("hello **world**")
	if got != "hello <b>world</b>" {
		t.Errorf("got %q", got)
	}
}

func TestFormatHTML_Italic(t *testing.T) {
	got := FormatHTML("hello *world*")
	if got != "hello <i>world</i>" {
		t.Errorf("got %q", got)
	}
}

func TestFormatHTML_InlineCode(t *testing.T) {
	got := FormatHTML("use `fmt.Println`")
	if got != "use <code>fmt.Println</code>" {
		t.Errorf("got %q", got)
	}
}

func TestFormatHTML_Strikethrough(t *testing.T) {
	got := FormatHTML("~~deleted~~")
	if got != "<s>deleted</s>" {
		t.Errorf("got %q", got)
	}
}

func TestFormatHTML_Spoiler(t *testing.T) {
	got := FormatHTML("||hidden||")
	if got != "<tg-spoiler>hidden</tg-spoiler>" {
		t.Errorf("got %q", got)
	}
}

func TestFormatHTML_Link(t *testing.T) {
	got := FormatHTML("[click](https://example.com)")
	want := `<a href="https://example.com">click</a>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatHTML_EscapesSpecialChars(t *testing.T) {
	got := FormatHTML("x < y & a > b")
	if got != "x &lt; y &amp; a &gt; b" {
		t.Errorf("got %q", got)
	}
}

// --- MarkdownToTelegramHTML line-level tests ---

func TestMarkdownToTelegramHTML_CodeBlock(t *testing.T) {
	md := "```go\nfmt.Println(\"hello\")\n```"
	got := MarkdownToTelegramHTML(md)
	want := "<pre><code>fmt.Println(&quot;hello&quot;)</code></pre>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownToTelegramHTML_Heading(t *testing.T) {
	got := MarkdownToTelegramHTML("## Section Title")
	if got != "<b>Section Title</b>" {
		t.Errorf("got %q", got)
	}
}

func TestMarkdownToTelegramHTML_Blockquote(t *testing.T) {
	got := MarkdownToTelegramHTML("> quoted text")
	if got != "<blockquote>quoted text</blockquote>" {
		t.Errorf("got %q", got)
	}
}

func TestMarkdownToTelegramHTML_Mixed(t *testing.T) {
	md := "# Title\n\nhello **bold** and *italic*\n\n```\ncode\n```"
	got := MarkdownToTelegramHTML(md)
	if !strings.Contains(got, "<b>Title</b>") {
		t.Errorf("missing heading: %q", got)
	}
	if !strings.Contains(got, "<b>bold</b>") {
		t.Errorf("missing bold: %q", got)
	}
	if !strings.Contains(got, "<i>italic</i>") {
		t.Errorf("missing italic: %q", got)
	}
	if !strings.Contains(got, "<pre><code>code</code></pre>") {
		t.Errorf("missing code block: %q", got)
	}
}

// --- Table tests ---

func TestIsTableLine(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"| a | b |", true},
		{"  | a | b |  ", true},
		{"| --- | --- |", true},
		{"| single |", true},
		{"not a table", false},
		{"|", false},
		{"| a | b", false},  // no trailing pipe
		{"a | b |", false},  // no leading pipe
		{"", false},
	}
	for _, tt := range tests {
		got := isTableLine(tt.line)
		if got != tt.want {
			t.Errorf("isTableLine(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestMarkdownToTelegramHTML_Table(t *testing.T) {
	md := "| Name | Age |\n| --- | --- |\n| Alice | 30 |\n| Bob | 25 |"
	got := MarkdownToTelegramHTML(md)
	want := "<pre>| Name | Age |\n| --- | --- |\n| Alice | 30 |\n| Bob | 25 |</pre>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownToTelegramHTML_TableWithSpecialChars(t *testing.T) {
	md := "| Expr | Result |\n| --- | --- |\n| a < b | true |\n| x & y | false |"
	got := MarkdownToTelegramHTML(md)
	want := "<pre>| Expr | Result |\n| --- | --- |\n| a &lt; b | true |\n| x &amp; y | false |</pre>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownToTelegramHTML_TableWithSurroundingText(t *testing.T) {
	md := "Here is a table:\n| A | B |\n| - | - |\n| 1 | 2 |\nAnd more text."
	got := MarkdownToTelegramHTML(md)
	if !strings.Contains(got, "Here is a table:") {
		t.Errorf("missing leading text: %q", got)
	}
	if !strings.Contains(got, "<pre>| A | B |\n| - | - |\n| 1 | 2 |</pre>") {
		t.Errorf("missing table pre block: %q", got)
	}
	if !strings.Contains(got, "And more text.") {
		t.Errorf("missing trailing text: %q", got)
	}
}

func TestMarkdownToTelegramHTML_TableAtEnd(t *testing.T) {
	md := "Summary:\n| X | Y |\n| - | - |\n| a | b |"
	got := MarkdownToTelegramHTML(md)
	if !strings.HasSuffix(got, "| a | b |</pre>") {
		t.Errorf("table at end not flushed: %q", got)
	}
}

func TestMarkdownToTelegramHTML_TableWithEmoji(t *testing.T) {
	md := "| 카테고리 | PR |\n| --- | --- |\n| 🧠 메모리 | #484 |\n| ⚡ 성능 | #469 |"
	got := MarkdownToTelegramHTML(md)
	want := "<pre>| 카테고리 | PR |\n| --- | --- |\n| 🧠 메모리 | #484 |\n| ⚡ 성능 | #469 |</pre>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// --- Chunking tests ---

func TestChunkText_Short(t *testing.T) {
	chunks := ChunkText("hello", 100)
	if len(chunks) != 1 || chunks[0] != "hello" {
		t.Errorf("got %v", chunks)
	}
}

func TestChunkText_SplitsAtNewline(t *testing.T) {
	text := "line one\nline two\nline three"
	chunks := ChunkText(text, 15)
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks, got %d: %v", len(chunks), chunks)
	}
}

func TestChunkHTML_NoSplitNeeded(t *testing.T) {
	html := "<b>short</b>"
	chunks := ChunkHTML(html, 100)
	if len(chunks) != 1 || chunks[0] != html {
		t.Errorf("expected single chunk %q, got %v", html, chunks)
	}
}

func TestChunkHTML_SplitsLongText(t *testing.T) {
	html := strings.Repeat("a", 100)
	chunks := ChunkHTML(html, 30)
	if len(chunks) < 3 {
		t.Errorf("expected at least 3 chunks, got %d", len(chunks))
	}
}

// --- SplitCaptionAndBody tests ---

func TestSplitCaptionAndBody_Short(t *testing.T) {
	caption, body := SplitCaptionAndBody("short text", MaxCaptionLength, MaxTextLength)
	if caption != "short text" {
		t.Errorf("expected caption 'short text', got %q", caption)
	}
	if body != nil {
		t.Errorf("expected nil body, got %v", body)
	}
}

func TestSplitCaptionAndBody_Long(t *testing.T) {
	text := strings.Repeat("word ", 300)
	caption, body := SplitCaptionAndBody(text, MaxCaptionLength, MaxTextLength)
	if len(caption) > MaxCaptionLength {
		t.Errorf("caption too long: %d", len(caption))
	}
	if body == nil {
		t.Error("expected body chunks")
	}
}

// --- MarkdownToTelegramChunks integration ---

func TestMarkdownToTelegramChunks_Short(t *testing.T) {
	chunks := MarkdownToTelegramChunks("hello **world**", TextChunkLimit)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != "hello <b>world</b>" {
		t.Errorf("got %q", chunks[0])
	}
}

func TestMarkdownToTelegramChunks_Long(t *testing.T) {
	md := strings.Repeat("word ", 1000)
	chunks := MarkdownToTelegramChunks(md, TextChunkLimit)
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks, got %d", len(chunks))
	}
}

// --- UTF16Len tests ---

func TestUTF16Len(t *testing.T) {
	if got := UTF16Len("hello"); got != 5 {
		t.Errorf("UTF16Len('hello') = %d, want 5", got)
	}
	// Emoji (surrogate pair).
	if got := UTF16Len("😀"); got != 2 {
		t.Errorf("UTF16Len('😀') = %d, want 2", got)
	}
}

func TestChunkByNewline(t *testing.T) {
	// Short text — no chunking.
	chunks := ChunkByNewline("hello\nworld", 100)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != "hello\nworld" {
		t.Errorf("expected original text, got %q", chunks[0])
	}

	// Lines that fit in separate chunks.
	chunks = ChunkByNewline("aaa\nbbb\nccc", 5)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %v", len(chunks), chunks)
	}

	// Lines that can be merged.
	chunks = ChunkByNewline("aa\nbb\ncc\ndd", 6)
	// "aa\nbb" = 5 chars, "cc\ndd" = 5 chars → 2 chunks.
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %v", len(chunks), chunks)
	}

	// Single line exceeding maxLen falls back to length-based chunking.
	long := strings.Repeat("x", 20)
	chunks = ChunkByNewline(long, 10)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks for long line, got %d", len(chunks))
	}
}
