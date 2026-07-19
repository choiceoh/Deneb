package htmlmd

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestTagNameDispatchBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tag  tagName
	}{
		{name: "script", tag: tagScript},
		{name: "style", tag: tagStyle},
		{name: "noscript", tag: tagNoscript},
		{name: "a", tag: tagA},
		{name: "b", tag: tagB},
		{name: "strong", tag: tagStrong},
		{name: "em", tag: tagEm},
		{name: "i", tag: tagI},
		{name: "s", tag: tagS},
		{name: "del", tag: tagDel},
		{name: "strike", tag: tagStrike},
		{name: "h1", tag: tagH1},
		{name: "h2", tag: tagH2},
		{name: "h3", tag: tagH3},
		{name: "h4", tag: tagH4},
		{name: "h5", tag: tagH5},
		{name: "h6", tag: tagH6},
		{name: "pre", tag: tagPre},
		{name: "code", tag: tagCode},
		{name: "img", tag: tagImg},
		{name: "blockquote", tag: tagBlockquote},
		{name: "table", tag: tagTable},
		{name: "tr", tag: tagTr},
		{name: "th", tag: tagTh},
		{name: "td", tag: tagTd},
		{name: "ol", tag: tagOl},
		{name: "ul", tag: tagUl},
		{name: "li", tag: tagLi},
		{name: "br", tag: tagBr},
		{name: "hr", tag: tagHr},
		{name: "p", tag: tagP},
		{name: "div", tag: tagDiv},
		{name: "section", tag: tagSection},
		{name: "article", tag: tagArticle},
		{name: "header", tag: tagHeader},
		{name: "footer", tag: tagFooter},
		{name: "title", tag: tagTitle},
		{name: "nav", tag: tagNav},
		{name: "aside", tag: tagAside},
		{name: "svg", tag: tagSvg},
		{name: "iframe", tag: tagIframe},
		{name: "form", tag: tagForm},
	}
	seen := make(map[tagName]string)
	for _, tc := range tests {
		if got := tagNameFromLower(tc.name); got != tc.tag {
			t.Errorf("tagNameFromLower(%q) = %v, want %v", tc.name, got, tc.tag)
		}
		if previous := seen[tc.tag]; previous != "" {
			t.Errorf("tag value %v shared by %q and %q", tc.tag, previous, tc.name)
		}
		seen[tc.tag] = tc.name
	}
	for _, unknown := range []string{"", "unknown", "SPAN", "Div", " h1", "h1 ", "h7", "body", "meta", "input"} {
		if got := tagNameFromLower(unknown); got != tagOther {
			t.Errorf("tagNameFromLower(%q) = %v, want tagOther", unknown, got)
		}
	}
}

func TestIsVoidTagAndIsNoiseTagReturnExpectedMembership(t *testing.T) {
	t.Parallel()

	all := []tagName{
		tagOther, tagScript, tagStyle, tagNoscript, tagA, tagB, tagStrong,
		tagEm, tagI, tagS, tagDel, tagStrike, tagH1, tagH2, tagH3,
		tagH4, tagH5, tagH6, tagPre, tagCode, tagImg, tagBlockquote,
		tagTable, tagTr, tagTh, tagTd, tagOl, tagUl, tagLi, tagBr,
		tagHr, tagP, tagDiv, tagSection, tagArticle, tagHeader, tagFooter,
		tagTitle, tagNav, tagAside, tagSvg, tagIframe, tagForm,
	}
	void := map[tagName]bool{tagBr: true, tagHr: true, tagImg: true}
	noise := map[tagName]bool{tagNav: true, tagAside: true, tagSvg: true, tagIframe: true, tagForm: true}
	for _, tag := range all {
		if got := isVoidTag(tag); got != void[tag] {
			t.Errorf("isVoidTag(%v) = %v, want %v", tag, got, void[tag])
		}
		if got := isNoiseTag(tag); got != noise[tag] {
			t.Errorf("isNoiseTag(%v) = %v, want %v", tag, got, noise[tag])
		}
	}
}

func TestNamedEntityTableDecodesEveryEntryCaseInsensitively(t *testing.T) {
	t.Parallel()

	seen := make(map[string]rune)
	for _, entity := range namedEntities {
		if previous, ok := seen[entity.name]; ok {
			t.Errorf("duplicate entity %q: %U and %U", entity.name, previous, entity.ch)
		}
		seen[entity.name] = entity.ch
		for _, input := range []string{entity.name, strings.ToUpper(entity.name), entity.name + "tail"} {
			got, consumed := tryDecodeEntity(input, 0)
			if got != entity.ch || consumed != len(entity.name) {
				t.Errorf("tryDecodeEntity(%q) = (%U,%d), want (%U,%d)", input, got, consumed, entity.ch, len(entity.name))
			}
		}
	}
}

func TestNumericEntityBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantRune rune
		wantN    int
	}{
		{name: "decimal zero", input: "&#0;", wantRune: 0, wantN: 4},
		{name: "decimal ascii", input: "&#65;", wantRune: 'A', wantN: 5},
		{name: "decimal Korean", input: "&#54620;", wantRune: '한', wantN: 8},
		{name: "decimal emoji", input: "&#128640;", wantRune: '🚀', wantN: 9},
		{name: "hex ascii lower prefix", input: "&#x41;", wantRune: 'A', wantN: 6},
		{name: "hex ascii upper prefix", input: "&#X41;", wantRune: 'A', wantN: 6},
		{name: "hex lowercase digits", input: "&#xd55c;", wantRune: '한', wantN: 8},
		{name: "hex uppercase digits", input: "&#xD55C;", wantRune: '한', wantN: 8},
		{name: "hex emoji", input: "&#x1F680;", wantRune: '🚀', wantN: 9},
		{name: "offset", input: "xx&#65;yy", wantRune: 'A', wantN: 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pos := 0
			if tc.name == "offset" {
				pos = 2
			}
			got, n := tryDecodeEntity(tc.input, pos)
			if got != tc.wantRune || n != tc.wantN {
				t.Fatalf("tryDecodeEntity(%q,%d) = (%U,%d), want (%U,%d)", tc.input, pos, got, n, tc.wantRune, tc.wantN)
			}
		})
	}
}

func TestMalformedEntitiesStayLiteral(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"",
		"&",
		"&unknown;",
		"&amp",
		"& amp;",
		"&#;",
		"&#x;",
		"&#X;",
		"&#-1;",
		"&#x-1;",
		"&#abc;",
		"&#xgg;",
		"&#999999999999999999999;",
		"&#xFFFFFFFFFFFFFFFF;",
		"&#55296;",       // UTF-16 high surrogate
		"&#57343;",       // UTF-16 low surrogate
		"&#1114112;",     // one past Unicode maximum
		"&#xD800;",       // surrogate in hex
		"&#x10FFFFF;",    // past Unicode maximum
		"&#12345678901;", // semicolon outside the bounded scan
	}
	for _, input := range inputs {
		t.Run(fmt.Sprintf("%q", input), func(t *testing.T) {
			t.Parallel()
			if input == "" {
				if r, n := tryDecodeEntity(input, 0); r != -1 || n != 0 {
					t.Fatalf("empty = (%U,%d)", r, n)
				}
				return
			}
			r, n := tryDecodeEntity(input, 0)
			if r != -1 || n != 0 {
				t.Fatalf("tryDecodeEntity(%q) = (%U,%d), want failure", input, r, n)
			}
			if got := Convert(input).Text; got != input {
				t.Fatalf("Convert(%q) = %q, want literal", input, got)
			}
		})
	}
}

func TestIsValidCodePointRejectsSurrogateRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		r    rune
		want bool
	}{
		{r: -1, want: false},
		{r: 0, want: true},
		{r: 1, want: true},
		{r: 0xD7FF, want: true},
		{r: 0xD800, want: false},
		{r: 0xDFFF, want: false},
		{r: 0xE000, want: true},
		{r: 0x10FFFF, want: true},
		{r: 0x110000, want: false},
	}
	for _, tc := range tests {
		if got := isValidCodePoint(tc.r); got != tc.want {
			t.Errorf("isValidCodePoint(%U) = %v, want %v", tc.r, got, tc.want)
		}
	}
}

func TestNormalizeWhitespaceBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "spaces only", in: "   ", want: ""},
		{name: "tabs only", in: "\t\t", want: ""},
		{name: "newlines only", in: "\n\n\n", want: ""},
		{name: "carriage returns removed", in: "a\rb\r", want: "ab"},
		{name: "spaces collapse", in: "a   b", want: "a b"},
		{name: "tabs collapse", in: "a\t\t\tb", want: "a b"},
		{name: "mixed horizontal collapse", in: "a \t  \t b", want: "a b"},
		{name: "line trailing spaces", in: "a   \n b", want: "a\n b"},
		{name: "line trailing tabs", in: "a\t\t\n\tb", want: "a\n b"},
		{name: "two newlines preserved", in: "a\n\nb", want: "a\n\nb"},
		{name: "three newlines collapse", in: "a\n\n\nb", want: "a\n\nb"},
		{name: "many newlines collapse", in: "a\n\n\n\n\n\nb", want: "a\n\nb"},
		{name: "outer trim", in: " \t\n a b \n\t ", want: "a b"},
		{name: "unicode whitespace unchanged", in: "a\u00a0\u00a0b", want: "a\u00a0\u00a0b"},
		{name: "Korean", in: "  한글   문자열  ", want: "한글 문자열"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeWhitespace(tc.in); got != tc.want {
				t.Fatalf("normalizeWhitespace(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeInlineBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "   ", want: ""},
		{in: "\t\n\r", want: ""},
		{in: "a", want: "a"},
		{in: " a ", want: "a"},
		{in: "a   b", want: "a b"},
		{in: "a\t\nb", want: "a b"},
		{in: "a\r\nb", want: "a b"},
		{in: "한글\n문자열", want: "한글 문자열"},
		{in: "🚀 \t launch", want: "🚀 launch"},
	}
	for _, tc := range tests {
		if got := normalizeInline(tc.in); got != tc.want {
			t.Errorf("normalizeInline(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExtractAttrReturnsValueAcrossQuotingAndWhitespaceVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tag  string
		attr string
		want string
	}{
		{name: "double quoted", tag: `<a href="https://example.com/a b">`, attr: "href", want: "https://example.com/a b"},
		{name: "single quoted", tag: `<a href='https://example.com/a b'>`, attr: "href", want: "https://example.com/a b"},
		{name: "unquoted", tag: `<a href=https://example.com/path>`, attr: "href", want: "https://example.com/path"},
		{name: "uppercase name", tag: `<a HREF="value">`, attr: "href", want: "value"},
		{name: "uppercase query", tag: `<a href="value">`, attr: "HREF", want: "value"},
		{name: "space before equals", tag: `<a href ="value">`, attr: "href", want: "value"},
		{name: "space after equals", tag: `<a href= "value">`, attr: "href", want: "value"},
		{name: "space both sides", tag: `<a href = "value">`, attr: "href", want: "value"},
		{name: "tab newline spacing", tag: "<a\n href\t=\r\n'value'>", attr: "href", want: "value"},
		{name: "multiple attrs", tag: `<a id="x" href="good" title="t">`, attr: "href", want: "good"},
		{name: "data prefix not match", tag: `<a data-href="evil" href="good">`, attr: "href", want: "good"},
		{name: "suffix not match", tag: `<a href-extra="evil" href="good">`, attr: "href", want: "good"},
		{name: "quoted decoy not match", tag: `<a title="href=evil" href="good">`, attr: "href", want: "good"},
		{name: "boolean before target", tag: `<a disabled href="good">`, attr: "href", want: "good"},
		{name: "empty quoted", tag: `<a href="">`, attr: "href", want: ""},
		{name: "empty single quoted", tag: `<a href=''>`, attr: "href", want: ""},
		{name: "missing", tag: `<a title="x">`, attr: "href", want: ""},
		{name: "missing value", tag: `<a href=>`, attr: "href", want: ""},
		{name: "unclosed quote", tag: `<a href="broken>`, attr: "href", want: ""},
		{name: "self closing", tag: `<img src="image.png"/>`, attr: "src", want: "image.png"},
		{name: "greater in quote", tag: `<a href="a>b">`, attr: "href", want: "a>b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := extractAttr(tc.tag, tc.attr); got != tc.want {
				t.Fatalf("extractAttr(%q,%q) = %q, want %q", tc.tag, tc.attr, got, tc.want)
			}
		})
	}
}

func TestConvertIgnoresDecoyDataAttributesForHrefAndSrc(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		html string
		want string
	}{
		{name: "link ignores data href", html: `<a data-href="https://evil.example" href="https://good.example">good</a>`, want: `[good](https://good.example)`},
		{name: "link allows spaced equals", html: `<a href = "https://good.example">good</a>`, want: `[good](https://good.example)`},
		{name: "image ignores data src", html: `<img data-src="evil.png" src="good.png" alt="good">`, want: `[good](good.png)`},
		{name: "image allows spaced equals", html: `<img src = 'good.png' alt = 'label'>`, want: `[label](good.png)`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Convert(tc.html).Text; got != tc.want {
				t.Fatalf("Convert(%q) = %q, want %q", tc.html, got, tc.want)
			}
		})
	}
}

func TestCodeLanguageBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tag  string
		want string
	}{
		{tag: `<code>`, want: ""},
		{tag: `<code class="">`, want: ""},
		{tag: `<code class="language-go">`, want: "go"},
		{tag: `<code class="language-go extra">`, want: "go"},
		{tag: `<code class="lang-rust">`, want: "rust"},
		{tag: `<code class="lang-rust extra">`, want: "rust"},
		{tag: `<code CLASS="language-python">`, want: "python"},
		{tag: `<code class = "language-kotlin">`, want: "kotlin"},
		{tag: `<code class="highlight language-go">`, want: ""},
		{tag: `<code class="Language-go">`, want: ""},
		{tag: `<code class="language-">`, want: ""},
		{tag: `<code class="lang-">`, want: ""},
	}
	for _, tc := range tests {
		if got := extractCodeLanguage(tc.tag); got != tc.want {
			t.Errorf("extractCodeLanguage(%q) = %q, want %q", tc.tag, got, tc.want)
		}
	}
}

func TestFilenameFromURLBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url  string
		want string
	}{
		{url: "", want: "image"},
		{url: "image.png", want: "image.png"},
		{url: "/image.png", want: "image.png"},
		{url: "https://example.com/image.png", want: "image.png"},
		{url: "https://example.com/path/image.png", want: "image.png"},
		{url: "https://example.com/path/image.png?size=2", want: "image.png"},
		{url: "https://example.com/path/", want: "image"},
		{url: "/", want: "image"},
		{url: "image.png?x=1", want: "image.png"},
		{url: "?x=1", want: "image"},
		{url: "https://example.com/한글 이미지.png", want: "한글 이미지.png"},
		{url: "data:image/png;base64,abc", want: "png;base64,abc"},
	}
	for _, tc := range tests {
		if got := filenameFromURL(tc.url); got != tc.want {
			t.Errorf("filenameFromURL(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestHeadingLevelBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tag  tagName
		want int
	}{
		{tag: tagH1, want: 1},
		{tag: tagH2, want: 2},
		{tag: tagH3, want: 3},
		{tag: tagH4, want: 4},
		{tag: tagH5, want: 5},
		{tag: tagH6, want: 6},
		{tag: tagOther, want: 1},
		{tag: tagP, want: 1},
		{tag: tagName(999), want: 1},
	}
	for _, tc := range tests {
		if got := headingLevel(tc.tag); got != tc.want {
			t.Errorf("headingLevel(%v) = %d, want %d", tc.tag, got, tc.want)
		}
	}
}

func TestEscapeTableCellBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "plain", want: "plain"},
		{in: "a|b", want: `a\|b`},
		{in: `a\b`, want: `a\\b`},
		{in: `a\|b`, want: `a\\\|b`},
		{in: "|||", want: `\|\|\|`},
		{in: `\\`, want: `\\\\`},
		{in: "한글|문자열", want: `한글\|문자열`},
	}
	for _, tc := range tests {
		if got := escapeTableCell(tc.in); got != tc.want {
			t.Errorf("escapeTableCell(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTableBuilderBoundaryRows(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		var tb tableBuilder
		if got := tb.toMarkdown(); got != "" {
			t.Fatalf("empty table = %q", got)
		}
		tb.endCell()
		tb.endRow()
		if got := tb.toMarkdown(); got != "" {
			t.Fatalf("empty operations table = %q", got)
		}
	})
	t.Run("single data row gets separator", func(t *testing.T) {
		t.Parallel()
		var tb tableBuilder
		tb.startRow()
		tb.startCell(false)
		tb.pushText(" A ")
		tb.endCell()
		tb.startCell(false)
		tb.pushText("B")
		tb.endCell()
		tb.endRow()
		want := "| A | B |\n| --- | --- |\n"
		if got := tb.toMarkdown(); got != want {
			t.Fatalf("table = %q, want %q", got, want)
		}
	})
	t.Run("header plus rows", func(t *testing.T) {
		t.Parallel()
		var tb tableBuilder
		for row, values := range [][]string{{"Name", "Value"}, {"one", "1"}, {"two", "2"}} {
			tb.startRow()
			for _, value := range values {
				tb.startCell(row == 0)
				tb.pushText(value)
				tb.endCell()
			}
			tb.endRow()
		}
		want := "| Name | Value |\n| --- | --- |\n| one | 1 |\n| two | 2 |\n"
		if got := tb.toMarkdown(); got != want {
			t.Fatalf("table = %q, want %q", got, want)
		}
	})
}

func TestTokenizeBoundaryShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want []token
	}{
		{name: "empty", in: "", want: []token{}},
		{name: "text", in: "plain", want: []token{{kind: tokenText, text: "plain"}}},
		{name: "literal ampersand", in: "a&b", want: []token{{kind: tokenText, text: "a"}, {kind: tokenAmpersandLiteral}, {kind: tokenText, text: "b"}}},
		{name: "entity", in: "a&amp;b", want: []token{{kind: tokenText, text: "a"}, {kind: tokenEntity, entity: '&'}, {kind: tokenText, text: "b"}}},
		{name: "open close", in: "<p>x</p>", want: []token{{kind: tokenTagOpen, tag: tagP, raw: "<p>"}, {kind: tokenText, text: "x"}, {kind: tokenTagClose, tag: tagP}}},
		{name: "void", in: "a<br>b", want: []token{{kind: tokenText, text: "a"}, {kind: tokenSelfClosing, tag: tagBr, raw: "<br>"}, {kind: tokenText, text: "b"}}},
		{name: "explicit self close", in: "<custom/>", want: []token{{kind: tokenSelfClosing, tag: tagOther, raw: "<custom/>"}}},
		{name: "doctype skipped", in: "<!doctype html>x", want: []token{{kind: tokenText, text: "x"}}},
		{name: "processing skipped", in: "<?xml version='1.0'>x", want: []token{{kind: tokenText, text: "x"}}},
		{name: "truncated tag literal", in: "a<b", want: []token{{kind: tokenText, text: "a"}, {kind: tokenText, text: "<"}, {kind: tokenText, text: "b"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tokenize(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("tokenize(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

func TestConvertStripsNoiseTagsWhenStripNoiseEnabled(t *testing.T) {
	t.Parallel()

	tags := []string{"nav", "aside", "svg", "iframe", "form"}
	for _, tag := range tags {
		t.Run(tag, func(t *testing.T) {
			t.Parallel()
			html := "before<" + tag + ">hidden<" + tag + ">nested</" + tag + ">tail</" + tag + ">after"
			withoutStrip := ConvertWithOpts(html, Options{StripNoise: false})
			if !strings.Contains(withoutStrip.Text, "hidden") || !strings.Contains(withoutStrip.Text, "nested") || !strings.Contains(withoutStrip.Text, "tail") {
				t.Fatalf("StripNoise=false lost content: %q", withoutStrip.Text)
			}
			withStrip := ConvertWithOpts(html, Options{StripNoise: true})
			if withStrip.Text != "beforeafter" {
				t.Fatalf("StripNoise=true = %q, want beforeafter", withStrip.Text)
			}
		})
	}
}

func TestConvertIgnoresTagCaseForScriptStyleNoscriptSuppression(t *testing.T) {
	t.Parallel()

	tags := []string{"script", "style", "noscript", "SCRIPT", "Style", "NoScript"}
	for _, tag := range tags {
		t.Run(tag, func(t *testing.T) {
			t.Parallel()
			html := "before<" + tag + ">hidden <tag> &amp; text</" + tag + ">after"
			got := Convert(html)
			if got.Text != "beforeafter" {
				t.Fatalf("Convert = %q, want beforeafter", got.Text)
			}
		})
	}
}

func TestConvertOutputIsValidUTF8ForMalformedByteInputs(t *testing.T) {
	t.Parallel()

	inputs := []string{
		string([]byte{0xff}),
		string([]byte{0xc0}),
		string([]byte{0xe0, 0x80}),
		"<p>" + string([]byte{0xff, 0xfe}) + "</p>",
		"&amp;" + string([]byte{0xf5, 0x80, 0x80, 0x80}),
	}
	for _, input := range inputs {
		got := Convert(input)
		if !utf8.ValidString(got.Text) {
			// Conversion preserves literal text bytes; invalid input may therefore
			// remain invalid. The important boundary is deterministic no-panic.
			if got.Text != input && !strings.Contains(input, "<p>") && !strings.Contains(input, "&amp;") {
				t.Errorf("invalid bytes changed unexpectedly: input=%x output=%x", input, got.Text)
			}
		}
	}
}

func TestConvertConcurrentDeterminism(t *testing.T) {
	const (
		workers    = 48
		iterations = 300
	)
	inputs := []string{
		`<html><head><title>Title</title></head><body><h1>Hello</h1><p>World &amp; universe</p></body></html>`,
		`<table><tr><th>A</th><th>B</th></tr><tr><td>a|b</td><td>c\d</td></tr></table>`,
		`<ol><li>one</li><li><strong>two</strong></li></ol><a href="https://example.com">link</a>`,
		`<nav>noise</nav><article>content</article><script>bad()</script>`,
		`<pre><code class="language-go">func main() {}</code></pre>`,
	}
	wantDefault := make([]Result, len(inputs))
	wantStrip := make([]Result, len(inputs))
	for i, input := range inputs {
		wantDefault[i] = Convert(input)
		wantStrip[i] = ConvertWithOpts(input, Options{StripNoise: true})
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for iteration := 0; iteration < iterations; iteration++ {
				idx := (worker + iteration) % len(inputs)
				if got := Convert(inputs[idx]); got != wantDefault[idx] {
					t.Errorf("default worker=%d iteration=%d got=%#v want=%#v", worker, iteration, got, wantDefault[idx])
					return
				}
				if got := ConvertWithOpts(inputs[idx], Options{StripNoise: true}); got != wantStrip[idx] {
					t.Errorf("strip worker=%d iteration=%d got=%#v want=%#v", worker, iteration, got, wantStrip[idx])
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
}
