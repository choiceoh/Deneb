package htmlmd

import (
	"bytes"
	"strings"
)

// Result holds the output of HTML → Markdown conversion.
type Result struct {
	Text  string
	Title string
}

// Options controls HTML → Markdown conversion behavior.
type Options struct {
	// StripNoise suppresses nav, aside, svg, iframe, form elements
	// in addition to the always-suppressed script/style/noscript.
	StripNoise bool
}

// Convert converts HTML to a Markdown-like plain text representation.
func Convert(html string) Result {
	return ConvertWithOpts(html, Options{})
}

// ConvertWithOpts converts HTML to Markdown with configurable options.
func ConvertWithOpts(html string, opts Options) (result Result) {
	// Panic safety: return empty result on any panic.
	defer func() {
		if r := recover(); r != nil {
			result = Result{}
		}
	}()

	tokens := tokenize(html)
	rawText, title := emit(tokens, len(html), opts.StripNoise)
	text := normalizeWhitespace(rawText)

	result.Text = text
	if title != nil {
		result.Title = *title
	}
	return result
}

// --- Tokenizer ---

// tagName identifies known HTML tags for O(1) dispatch in the emitter.
type tagName int

const (
	tagOther tagName = iota
	tagScript
	tagStyle
	tagNoscript
	tagA
	tagB
	tagStrong
	tagEm
	tagI
	tagS
	tagDel
	tagStrike
	tagH1
	tagH2
	tagH3
	tagH4
	tagH5
	tagH6
	tagPre
	tagCode
	tagImg
	tagBlockquote
	tagTable
	tagTr
	tagTh
	tagTd
	tagOl
	tagUl
	tagLi
	tagBr
	tagHr
	tagP
	tagDiv
	tagSection
	tagArticle
	tagHeader
	tagFooter
	tagTitle
	tagNav
	tagAside
	tagSvg
	tagIframe
	tagForm
)

// tagNames maps lowercase tag names to their tagName constants.
var tagNames = map[string]tagName{
	"script": tagScript, "style": tagStyle, "noscript": tagNoscript,
	"a": tagA, "b": tagB, "strong": tagStrong, "em": tagEm, "i": tagI,
	"s": tagS, "del": tagDel, "strike": tagStrike,
	"h1": tagH1, "h2": tagH2, "h3": tagH3, "h4": tagH4, "h5": tagH5, "h6": tagH6,
	"pre": tagPre, "code": tagCode, "img": tagImg, "blockquote": tagBlockquote,
	"table": tagTable, "tr": tagTr, "th": tagTh, "td": tagTd,
	"ol": tagOl, "ul": tagUl, "li": tagLi, "br": tagBr, "hr": tagHr,
	"p": tagP, "div": tagDiv, "section": tagSection, "article": tagArticle,
	"header": tagHeader, "footer": tagFooter, "title": tagTitle,
	"nav": tagNav, "aside": tagAside, "svg": tagSvg, "iframe": tagIframe,
	"form": tagForm,
}

func tagNameFromLower(s string) tagName {
	if tag, ok := tagNames[s]; ok {
		return tag
	}
	return tagOther
}

func isVoidTag(tag tagName) bool {
	return tag == tagBr || tag == tagHr || tag == tagImg
}

// tokenKind distinguishes different token types.
type tokenKind int

const (
	tokenText tokenKind = iota
	tokenTagOpen
	tokenTagClose
	tokenSelfClosing
	tokenEntity
	tokenAmpersandLiteral
)

// token is a single HTML token. text fields reference the original input
// via string slicing (zero-copy where possible).
type token struct {
	kind   tokenKind
	tag    tagName
	raw    string // full tag string for TagOpen/SelfClosing (includes < and >)
	text   string // for Text tokens
	entity rune   // for Entity tokens
}

// tokenize scans HTML input into a token stream in a single pass.
func tokenize(input string) []token {
	b := []byte(input)
	n := len(b)
	tokens := make([]token, 0, n/8)
	cursor := 0
	textStart := 0

	for cursor < n {
		// Fast scan for next '<' or '&'.
		pos := bytes.IndexAny(b[cursor:], "<&")
		if pos < 0 {
			break
		}
		pos += cursor

		// Flush accumulated text.
		if pos > textStart {
			tokens = append(tokens, token{kind: tokenText, text: input[textStart:pos]})
		}

		if b[pos] == '<' {
			cursor = scanTag(input, pos, &tokens)
		} else {
			cursor = scanEntity(input, pos, &tokens)
		}
		textStart = cursor
	}

	// Flush trailing text.
	if textStart < n {
		tokens = append(tokens, token{kind: tokenText, text: input[textStart:]})
	}

	return tokens
}

// scanTag processes a tag starting at pos ('<'). Returns new cursor position.
func scanTag(input string, pos int, tokens *[]token) int {
	// Find closing '>'.
	gt := strings.IndexByte(input[pos:], '>')
	if gt < 0 {
		// Malformed: no closing '>'. Emit '<' as text.
		*tokens = append(*tokens, token{kind: tokenText, text: "<"})
		return pos + 1
	}
	gt += pos

	tagStr := input[pos : gt+1]
	inner := input[pos+1 : gt]

	// Closing tag?
	if strings.HasPrefix(inner, "/") {
		return scanCloseTag(inner, gt, tokens)
	}

	// Opening or self-closing tag. Extract tag name.
	nameEnd := strings.IndexFunc(inner, isTagNameEnd)
	if nameEnd < 0 {
		nameEnd = len(inner)
	}
	nameLower := strings.ToLower(inner[:nameEnd])

	// Skip <!doctype, <!--, <!, <? etc.
	if strings.HasPrefix(nameLower, "!") || strings.HasPrefix(nameLower, "?") {
		return gt + 1
	}

	tag := tagNameFromLower(nameLower)

	// Self-closing: ends with '/' before '>', or is a void element.
	if strings.HasSuffix(inner, "/") || isVoidTag(tag) {
		*tokens = append(*tokens, token{kind: tokenSelfClosing, tag: tag, raw: tagStr})
	} else {
		*tokens = append(*tokens, token{kind: tokenTagOpen, tag: tag, raw: tagStr})
	}

	// For script/style/noscript: find matching close tag, emit raw content.
	if tag == tagScript || tag == tagStyle || tag == tagNoscript {
		return scanRawContent(input, gt+1, nameLower, tag, tokens)
	}

	return gt + 1
}

// scanCloseTag processes a closing tag whose inner text (after '<') starts
// with '/'. Returns the cursor position past the tag's '>'.
func scanCloseTag(inner string, gt int, tokens *[]token) int {
	nameStr := inner[1:]
	nameEnd := strings.IndexFunc(nameStr, isWhitespaceOrGT)
	if nameEnd < 0 {
		nameEnd = len(nameStr)
	}
	tag := tagNameFromLower(strings.ToLower(nameStr[:nameEnd]))
	*tokens = append(*tokens, token{kind: tokenTagClose, tag: tag})
	return gt + 1
}

// scanRawContent consumes raw element content (script/style/noscript) up to
// the matching close tag, emitting it as a single text token. Returns the
// cursor position past the close tag, or end of input if none is found.
func scanRawContent(input string, searchFrom int, nameLower string, tag tagName, tokens *[]token) int {
	closeTag := "</" + nameLower + ">"
	lowerRest := strings.ToLower(input[searchFrom:])
	closeRel := strings.Index(lowerRest, closeTag)
	if closeRel < 0 {
		// No closing tag found — rest is all suppressed content.
		if searchFrom < len(input) {
			*tokens = append(*tokens, token{kind: tokenText, text: input[searchFrom:]})
		}
		return len(input)
	}
	contentEnd := searchFrom + closeRel
	if contentEnd > searchFrom {
		*tokens = append(*tokens, token{kind: tokenText, text: input[searchFrom:contentEnd]})
	}
	*tokens = append(*tokens, token{kind: tokenTagClose, tag: tag})
	return contentEnd + len(closeTag)
}

// scanEntity processes an entity starting at pos ('&'). Returns new cursor.
func scanEntity(input string, pos int, tokens *[]token) int {
	ch, advance := tryDecodeEntity(input, pos)
	if advance > 0 && ch >= 0 {
		*tokens = append(*tokens, token{kind: tokenEntity, entity: ch})
		return pos + advance
	}
	*tokens = append(*tokens, token{kind: tokenAmpersandLiteral})
	return pos + 1
}

func isWhitespaceOrGT(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '>'
}

// isTagNameEnd reports whether r terminates a tag name in an opening tag.
func isTagNameEnd(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '/' || r == '>'
}
