package htmlmd

import (
	"strings"
	"testing"
)

func TestBasicHTML(t *testing.T) {
	r := Convert("<p>Hello <b>world</b></p>")
	if r.Text != "Hello **world**" {
		t.Errorf("got %q", r.Text)
	}
}

func TestStripsScriptStyle(t *testing.T) {
	html := "<p>before</p><script>alert(1)</script><style>.x{}</style><p>after</p>"
	r := Convert(html)
	if !strings.Contains(r.Text, "before") {
		t.Error("missing before")
	}
	if !strings.Contains(r.Text, "after") {
		t.Error("missing after")
	}
	if strings.Contains(r.Text, "alert") {
		t.Error("script not stripped")
	}
	if strings.Contains(r.Text, ".x") {
		t.Error("style not stripped")
	}
}

func TestExtractsTitle(t *testing.T) {
	html := "<html><head><title>My Page</title></head><body>content</body></html>"
	r := Convert(html)
	if r.Title != "My Page" {
		t.Errorf("title = %q", r.Title)
	}
}

func TestConvertsLinks(t *testing.T) {
	html := `<a href="https://example.com">Click here</a>`
	r := Convert(html)
	if !strings.Contains(r.Text, "[Click here](https://example.com)") {
		t.Errorf("got %q", r.Text)
	}
}

func TestConvertsHeadings(t *testing.T) {
	html := "<h1>Title</h1><h2>Subtitle</h2><p>text</p>"
	r := Convert(html)
	if !strings.Contains(r.Text, "# Title") {
		t.Errorf("missing h1: %q", r.Text)
	}
	if !strings.Contains(r.Text, "## Subtitle") {
		t.Errorf("missing h2: %q", r.Text)
	}
}

func TestConvertsListItems(t *testing.T) {
	r := Convert("<ul><li>one</li><li>two</li></ul>")
	if !strings.Contains(r.Text, "- one") {
		t.Errorf("missing li: %q", r.Text)
	}
	if !strings.Contains(r.Text, "- two") {
		t.Errorf("missing li: %q", r.Text)
	}
}

func TestDecodesEntities(t *testing.T) {
	html := "<p>&amp; &lt; &gt; &quot; &#39; &#x41; &#65;</p>"
	r := Convert(html)
	if !strings.Contains(r.Text, `& < > " ' A A`) {
		t.Errorf("got %q", r.Text)
	}
}

func TestDecodesExtendedEntities(t *testing.T) {
	html := "<p>&mdash; &ndash; &hellip; &laquo; &raquo; &copy; &reg; &trade; &bull; &middot;</p>"
	r := Convert(html)
	for _, tc := range []struct {
		name string
		ch   rune
	}{
		{"mdash", '\u2014'},
		{"ndash", '\u2013'},
		{"hellip", '\u2026'},
		{"laquo", '\u00AB'},
		{"raquo", '\u00BB'},
		{"copy", '\u00A9'},
		{"reg", '\u00AE'},
		{"trade", '\u2122'},
		{"bull", '\u2022'},
		{"middot", '\u00B7'},
	} {
		if !strings.ContainsRune(r.Text, tc.ch) {
			t.Errorf("%s missing: %q", tc.name, r.Text)
		}
	}
}

func TestNormalizesWhitespace(t *testing.T) {
	r := Convert("<p>  hello   world  </p>")
	if r.Text != "hello world" {
		t.Errorf("got %q", r.Text)
	}
}

func TestMultibyteUTF8(t *testing.T) {
	html := "<p>안녕하세요 🌍</p><br><p>세계</p>"
	r := Convert(html)
	if !strings.Contains(r.Text, "안녕하세요") {
		t.Error("Korean missing")
	}
	if !strings.Contains(r.Text, "🌍") {
		t.Error("emoji missing")
	}
	if !strings.Contains(r.Text, "세계") {
		t.Error("Korean 2 missing")
	}
}

func TestMultibyteEntitiesMixed(t *testing.T) {
	r := Convert("<p>한국어 &amp; 日本語</p>")
	if r.Text != "한국어 & 日本語" {
		t.Errorf("got %q", r.Text)
	}
}

func TestEmptyInput(t *testing.T) {
	r := Convert("")
	if r.Text != "" {
		t.Errorf("got %q", r.Text)
	}
	if r.Title != "" {
		t.Errorf("title = %q", r.Title)
	}
}

func TestTruncatedTags(t *testing.T) {
	cases := []string{
		"<h", "<h1", "<h1>", "<a ", "<a href=", "<li", "<li>",
		"<br", "<hr", "</p", "<script", "<style", "<noscript",
		"&", "&amp", "&#", "&#x", "&#x4", "&#39",
	}
	for _, c := range cases {
		r := Convert(c) // must not panic
		_ = r.Text
	}
}

func TestMultibyteNearEntityBoundary(t *testing.T) {
	r := Convert("<p>&amp;한국어텍스트</p>")
	if !strings.Contains(r.Text, "&") || !strings.Contains(r.Text, "한국어텍스트") {
		t.Errorf("got %q", r.Text)
	}
}

func TestManyAmpersandsWithMultibyte(t *testing.T) {
	r := Convert("&한&국&어&amp;테스트")
	if !strings.Contains(r.Text, "&테스트") {
		t.Errorf("got %q", r.Text)
	}
}

func TestDeeplyNestedTags(t *testing.T) {
	html := strings.Repeat("<div>", 100) + "content" + strings.Repeat("</div>", 100)
	r := Convert(html)
	if !strings.Contains(r.Text, "content") {
		t.Error("content missing")
	}
}

func TestUnbalancedTags(t *testing.T) {
	html := `<h1>Title</h2><a href="x">link<p>text</li></a>`
	r := Convert(html)
	_ = r.Text // no panic
}

func TestScriptWithAngleBrackets(t *testing.T) {
	html := "<script>if (a < b && c > d) { alert('</script>test'); }</script>after"
	r := Convert(html)
	if !strings.Contains(r.Text, "after") {
		t.Errorf("got %q", r.Text)
	}
}

func TestEmojiSequences(t *testing.T) {
	html := "<p>👨\u200d👩\u200d👧\u200d👦 family 🏳️\u200d🌈 flag</p>"
	r := Convert(html)
	if !strings.Contains(r.Text, "family") || !strings.Contains(r.Text, "flag") {
		t.Errorf("got %q", r.Text)
	}
}

func TestNullBytes(t *testing.T) {
	html := "<p>before\x00after</p>"
	r := Convert(html) // must not panic
	_ = r.Text
}

func TestOnlyTagsNoContent(t *testing.T) {
	r := Convert("<div><p><span></span></p></div>")
	if strings.TrimSpace(r.Text) != "" {
		t.Errorf("got %q", r.Text)
	}
}

func TestNumericEntityEdgeCases(t *testing.T) {
	html := "&#0; &#xFFFFFF; &#999999999; &#xD800; normal"
	r := Convert(html)
	if !strings.Contains(r.Text, "normal") {
		t.Errorf("got %q", r.Text)
	}
}

func TestHrefWithMultibyte(t *testing.T) {
	html := `<a href="https://example.com/한국">한국어 링크</a>`
	r := Convert(html)
	if !strings.Contains(r.Text, "[한국어 링크](https://example.com/한국)") {
		t.Errorf("got %q", r.Text)
	}
}

func TestLargeInputNoPanic(t *testing.T) {
	chunk := "<p>Hello &amp; world 한국어 🌍</p><br><hr>\n"
	html := strings.Repeat(chunk, 20_000)
	r := Convert(html)
	if !strings.Contains(r.Text, "Hello") {
		t.Error("content missing")
	}
}

func TestCurlyQuotesInHTML(t *testing.T) {
	html := "<!doctype html><html lang=\"ko-kr\"><head><title>YouTube\u2019s Best</title></head><body><p>It\u2019s a video about \u2018coding\u2019 &amp; stuff</p><li>Item with \u2019quotes\u2019</li><a href=\"https://example.com\">Link\u2019s text</a><h1>Heading with \u2019curly\u2019</h1></body></html>"
	r := Convert(html)
	if !strings.ContainsRune(r.Text, '\u2019') {
		t.Errorf("curly quote missing: %q", r.Text)
	}
}

func TestCurlyQuotesAtEveryByteAlignment(t *testing.T) {
	for padding := range 4 {
		prefix := strings.Repeat("x", padding)
		html := "<p>" + prefix + "\u2019</p><li>" + prefix + "\u2019item</li><a href=\"u\">" + prefix + "\u2019link</a><h2>" + prefix + "\u2019head</h2>"
		r := Convert(html)
		if !strings.ContainsRune(r.Text, '\u2019') {
			t.Errorf("padding=%d: curly quote missing: %q", padding, r.Text)
		}
	}
}

func TestEntityAdjacentToMultibyte(t *testing.T) {
	html := "\u2019&amp;\u2019&lt;\u2019&#8217;\u2019&#x2019;\u2019"
	r := Convert(html)
	if !strings.Contains(r.Text, "&") || !strings.Contains(r.Text, "<") {
		t.Errorf("got %q", r.Text)
	}
}

func TestYoutubeLikeKoreanHTML(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<!doctype html><html style="font-size: 10px;font-family: roboto, arial, sans-serif;" lang="ko-kr"><head><title>테스트 동영상</title></head><body>`)
	for range 30 {
		b.WriteString("<div>패딩텍스트</div>")
	}
	b.WriteString("<p>It\u2019s a test \u2018video\u2019 for Korean users</p>")
	b.WriteString("<script>var x = 'don\u2019t';</script>")
	b.WriteString("<li>\u2019목록 항목\u2019</li>")
	b.WriteString("</body></html>")
	r := Convert(b.String())
	if r.Title != "테스트 동영상" {
		t.Errorf("title = %q", r.Title)
	}
	if strings.Contains(r.Text, "var x") {
		t.Error("script not stripped")
	}
}

func TestXcomKoreanHTMLWithEllipsisNoPanic(t *testing.T) {
	prefix := `<!doctype html><html dir="ltr" lang="ko"><head><meta charset="utf-8" /><meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1,user-scalable=0,viewport-fit=cover" />`
	pad := `<meta http-equiv="origin-trial" content="AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" />`
	var b strings.Builder
	b.WriteString(prefix)
	for b.Len() < 1370 {
		b.WriteString(pad)
	}
	b.WriteString("<title>AI 연구\u2026최신 뉴스</title>")
	b.WriteString("</head><body>")
	b.WriteString("<p>Anthropic\u2026Claude 연구</p>")
	b.WriteString(`<a href="https://example.com">링크` + "\u2026" + `텍스트</a>`)
	b.WriteString("<li>\u2026목록</li>")
	b.WriteString("</body></html>")

	r := Convert(b.String())
	if r.Title == "" {
		t.Error("title should be extracted")
	}
	if !strings.ContainsRune(r.Text, '\u2026') {
		t.Errorf("ellipsis missing: %q", r.Text)
	}
	if !strings.Contains(r.Text, "Claude") {
		t.Errorf("content missing: %q", r.Text)
	}
}

func TestEllipsisAtEveryByteAlignment(t *testing.T) {
	for padding := range 4 {
		prefix := strings.Repeat("x", padding)
		html := "<p>" + prefix + "\u2026</p><li>" + prefix + "\u2026item</li><a href=\"u\">" + prefix + "\u2026link</a><h2>" + prefix + "\u2026head</h2><code>" + prefix + "\u2026code</code>"
		r := Convert(html)
		if !strings.ContainsRune(r.Text, '\u2026') {
			t.Errorf("padding=%d: ellipsis missing: %q", padding, r.Text)
		}
	}
}

func TestLargeXcomHTMLWithMultibyte(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<!doctype html><html dir="ltr" lang="ko"><head><meta charset="utf-8" /><title>AI` + "\u2026" + `뉴스</title></head><body>`)
	for i := range 500 {
		b.WriteString("<div><p>섹션 ")
		b.WriteString(strings.Repeat("0", 0)) // keep it simple
		b.WriteString(itoa(i))
		b.WriteString(": 한국어 텍스트\u2026더 보기</p><a href=\"https://x.com/")
		b.WriteString(itoa(i))
		b.WriteString("\">링크\u2026</a></div>")
	}
	b.WriteString("</body></html>")

	html := b.String()
	if len(html) < 50_000 {
		t.Fatal("document too small")
	}
	r := Convert(html)
	if !strings.Contains(r.Text, "섹션 0") {
		t.Error("first section missing")
	}
	if !strings.Contains(r.Text, "섹션 499") {
		t.Error("last section missing")
	}
	if !strings.ContainsRune(r.Text, '\u2026') {
		t.Error("ellipsis missing")
	}
}

func TestConvertsBold(t *testing.T) {
	r := Convert("<p><strong>bold text</strong> and <b>also bold</b></p>")
	if !strings.Contains(r.Text, "**bold text**") {
		t.Errorf("got %q", r.Text)
	}
	if !strings.Contains(r.Text, "**also bold**") {
		t.Errorf("got %q", r.Text)
	}
}

func TestConvertsItalic(t *testing.T) {
	r := Convert("<p><em>italic text</em> and <i>also italic</i></p>")
	if !strings.Contains(r.Text, "*italic text*") {
		t.Errorf("got %q", r.Text)
	}
	if !strings.Contains(r.Text, "*also italic*") {
		t.Errorf("got %q", r.Text)
	}
}

func TestBTagBoundary(t *testing.T) {
	html := "<body><br><base href='x'><button>click</button><b>bold</b></body>"
	r := Convert(html)
	if !strings.Contains(r.Text, "**bold**") {
		t.Errorf("got %q", r.Text)
	}
	if strings.Contains(r.Text, "**utton") {
		t.Errorf("false bold: %q", r.Text)
	}
}

func TestITagBoundary(t *testing.T) {
	html := "<p><input type='text'><i>italic</i></p>"
	r := Convert(html)
	if !strings.Contains(r.Text, "*italic*") {
		t.Errorf("got %q", r.Text)
	}
}

func TestNestedEmphasis(t *testing.T) {
	r := Convert("<strong><em>bold italic</em></strong>")
	if !strings.Contains(r.Text, "bold italic") {
		t.Errorf("got %q", r.Text)
	}
}

func TestConvertsInlineCode(t *testing.T) {
	r := Convert("<p>Use <code>println!</code> to print</p>")
	if !strings.Contains(r.Text, "`println!`") {
		t.Errorf("got %q", r.Text)
	}
}

func TestConvertsCodeBlock(t *testing.T) {
	html := "<pre><code>fn main() {\n    println!(\"hello\");\n}</code></pre>"
	r := Convert(html)
	if !strings.Contains(r.Text, "```") {
		t.Errorf("no fence: %q", r.Text)
	}
	if !strings.Contains(r.Text, "fn main()") {
		t.Errorf("code missing: %q", r.Text)
	}
}

func TestConvertsCodeBlockWithLanguage(t *testing.T) {
	html := `<pre><code class="language-rust">let x = 42;</code></pre>`
	r := Convert(html)
	if !strings.Contains(r.Text, "```rust") {
		t.Errorf("got %q", r.Text)
	}
	if !strings.Contains(r.Text, "let x = 42;") {
		t.Errorf("code missing: %q", r.Text)
	}
}

func TestConvertsStrikethrough(t *testing.T) {
	html := "<p><del>deleted</del> and <s>struck</s> and <strike>old</strike></p>"
	r := Convert(html)
	if !strings.Contains(r.Text, "~~deleted~~") {
		t.Errorf("got %q", r.Text)
	}
	if !strings.Contains(r.Text, "~~struck~~") {
		t.Errorf("got %q", r.Text)
	}
	if !strings.Contains(r.Text, "~~old~~") {
		t.Errorf("got %q", r.Text)
	}
}

func TestConvertsImage(t *testing.T) {
	html := `<img src="https://example.com/pic.jpg" alt="A photo">`
	r := Convert(html)
	if !strings.Contains(r.Text, "[A photo](https://example.com/pic.jpg)") {
		t.Errorf("got %q", r.Text)
	}
}

func TestConvertsImageNoAlt(t *testing.T) {
	html := `<img src="https://example.com/photo.png">`
	r := Convert(html)
	if !strings.Contains(r.Text, "[photo.png](https://example.com/photo.png)") {
		t.Errorf("got %q", r.Text)
	}
}

func TestConvertsBlockquote(t *testing.T) {
	r := Convert("<blockquote>This is quoted text</blockquote>")
	if !strings.Contains(r.Text, "> This is quoted text") {
		t.Errorf("got %q", r.Text)
	}
}

func TestConvertsSimpleTable(t *testing.T) {
	html := "<table><tr><td>A</td><td>B</td></tr><tr><td>1</td><td>2</td></tr></table>"
	r := Convert(html)
	if !strings.Contains(r.Text, "| A | B |") {
		t.Errorf("got %q", r.Text)
	}
	if !strings.Contains(r.Text, "| 1 | 2 |") {
		t.Errorf("got %q", r.Text)
	}
	if !strings.Contains(r.Text, "| --- |") {
		t.Errorf("got %q", r.Text)
	}
}

func TestConvertsTableWithHeaders(t *testing.T) {
	html := "<table><tr><th>Name</th><th>Age</th></tr><tr><td>Alice</td><td>30</td></tr></table>"
	r := Convert(html)
	if !strings.Contains(r.Text, "| Name | Age |") {
		t.Errorf("got %q", r.Text)
	}
	if !strings.Contains(r.Text, "| --- | --- |") {
		t.Errorf("got %q", r.Text)
	}
	if !strings.Contains(r.Text, "| Alice | 30 |") {
		t.Errorf("got %q", r.Text)
	}
}

func TestConvertsTablePipeEscape(t *testing.T) {
	html := "<table><tr><td>a|b</td><td>c</td></tr></table>"
	r := Convert(html)
	if !strings.Contains(r.Text, `a\|b`) {
		t.Errorf("pipe not escaped: %q", r.Text)
	}
}

func TestConvertsOrderedList(t *testing.T) {
	html := "<ol><li>first</li><li>second</li><li>third</li></ol>"
	r := Convert(html)
	if !strings.Contains(r.Text, "1. first") {
		t.Errorf("got %q", r.Text)
	}
	if !strings.Contains(r.Text, "2. second") {
		t.Errorf("got %q", r.Text)
	}
	if !strings.Contains(r.Text, "3. third") {
		t.Errorf("got %q", r.Text)
	}
}

func TestMixedOlUl(t *testing.T) {
	html := "<ul><li>bullet</li></ul><ol><li>one</li><li>two</li></ol><ul><li>dot</li></ul>"
	r := Convert(html)
	if !strings.Contains(r.Text, "- bullet") {
		t.Errorf("got %q", r.Text)
	}
	if !strings.Contains(r.Text, "1. one") {
		t.Errorf("got %q", r.Text)
	}
	if !strings.Contains(r.Text, "2. two") {
		t.Errorf("got %q", r.Text)
	}
	if !strings.Contains(r.Text, "- dot") {
		t.Errorf("got %q", r.Text)
	}
}

func TestSTagBoundary(t *testing.T) {
	html := "<p><span>text</span><s>struck</s><strong>bold</strong></p>"
	r := Convert(html)
	if !strings.Contains(r.Text, "~~struck~~") {
		t.Errorf("got %q", r.Text)
	}
	if !strings.Contains(r.Text, "**bold**") {
		t.Errorf("got %q", r.Text)
	}
}

// --- strip_noise option tests ---

func TestStripNoiseSuppressesNav(t *testing.T) {
	html := "<p>content</p><nav><a href='/'>Home</a><a href='/about'>About</a></nav><p>more</p>"
	r := ConvertWithOpts(html, Options{StripNoise: true})
	if !strings.Contains(r.Text, "content") {
		t.Errorf("got %q", r.Text)
	}
	if !strings.Contains(r.Text, "more") {
		t.Errorf("got %q", r.Text)
	}
	if strings.Contains(r.Text, "Home") {
		t.Errorf("nav not suppressed: %q", r.Text)
	}
}

func TestStripNoiseSuppressesAsideSvgIframeForm(t *testing.T) {
	html := "<p>keep</p><aside>sidebar</aside><svg><path/></svg><iframe src='x'>frame</iframe><form><input/></form><p>end</p>"
	r := ConvertWithOpts(html, Options{StripNoise: true})
	if !strings.Contains(r.Text, "keep") || !strings.Contains(r.Text, "end") {
		t.Errorf("got %q", r.Text)
	}
	if strings.Contains(r.Text, "sidebar") {
		t.Errorf("aside not suppressed: %q", r.Text)
	}
	if strings.Contains(r.Text, "frame") {
		t.Errorf("iframe not suppressed: %q", r.Text)
	}
}

func TestStripNoiseOffPreservesNav(t *testing.T) {
	html := "<p>content</p><nav><a href='/'>Home</a></nav>"
	r := Convert(html)
	if !strings.Contains(r.Text, "Home") {
		t.Errorf("nav should be preserved: %q", r.Text)
	}
}

func TestStripNoiseNestedNav(t *testing.T) {
	html := "<nav><ul><li><a href='/'>Home</a></li><li><a href='/about'>About</a></li></ul></nav><p>article content</p>"
	r := ConvertWithOpts(html, Options{StripNoise: true})
	if !strings.Contains(r.Text, "article content") {
		t.Errorf("got %q", r.Text)
	}
	if strings.Contains(r.Text, "Home") {
		t.Errorf("nested nav not suppressed: %q", r.Text)
	}
}

// --- Extended entity tests ---

func TestDecodesTypographyEntities(t *testing.T) {
	html := "<p>&mdash; &ndash; &hellip; &laquo;text&raquo; &lsquo;x&rsquo; &ldquo;y&rdquo;</p>"
	r := Convert(html)
	for _, tc := range []struct {
		name string
		ch   rune
	}{
		{"mdash", '\u2014'},
		{"ndash", '\u2013'},
		{"hellip", '\u2026'},
		{"laquo", '\u00AB'},
		{"raquo", '\u00BB'},
		{"lsquo", '\u2018'},
		{"rsquo", '\u2019'},
		{"ldquo", '\u201C'},
		{"rdquo", '\u201D'},
	} {
		if !strings.ContainsRune(r.Text, tc.ch) {
			t.Errorf("%s missing: %q", tc.name, r.Text)
		}
	}
}

func TestDecodesSymbolEntities(t *testing.T) {
	html := "<p>&copy; &reg; &trade; &deg; &euro; &pound;</p>"
	r := Convert(html)
	for _, tc := range []struct {
		name string
		ch   rune
	}{
		{"copy", '\u00A9'},
		{"reg", '\u00AE'},
		{"trade", '\u2122'},
		{"deg", '\u00B0'},
		{"euro", '\u20AC'},
		{"pound", '\u00A3'},
	} {
		if !strings.ContainsRune(r.Text, tc.ch) {
			t.Errorf("%s missing: %q", tc.name, r.Text)
		}
	}
}

func TestTableEscapesBackslash(t *testing.T) {
	html := `<table><tr><td>a\b</td><td>c</td></tr></table>`
	r := Convert(html)
	if !strings.Contains(r.Text, `a\\b`) {
		t.Errorf("backslash not escaped: %q", r.Text)
	}
}

// itoa is a minimal int-to-string helper to avoid importing strconv in tests.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
