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

func tagNameFromLower(s string) tagName {
	switch s {
	case "script":
		return tagScript
	case "style":
		return tagStyle
	case "noscript":
		return tagNoscript
	case "a":
		return tagA
	case "b":
		return tagB
	case "strong":
		return tagStrong
	case "em":
		return tagEm
	case "i":
		return tagI
	case "s":
		return tagS
	case "del":
		return tagDel
	case "strike":
		return tagStrike
	case "h1":
		return tagH1
	case "h2":
		return tagH2
	case "h3":
		return tagH3
	case "h4":
		return tagH4
	case "h5":
		return tagH5
	case "h6":
		return tagH6
	case "pre":
		return tagPre
	case "code":
		return tagCode
	case "img":
		return tagImg
	case "blockquote":
		return tagBlockquote
	case "table":
		return tagTable
	case "tr":
		return tagTr
	case "th":
		return tagTh
	case "td":
		return tagTd
	case "ol":
		return tagOl
	case "ul":
		return tagUl
	case "li":
		return tagLi
	case "br":
		return tagBr
	case "hr":
		return tagHr
	case "p":
		return tagP
	case "div":
		return tagDiv
	case "section":
		return tagSection
	case "article":
		return tagArticle
	case "header":
		return tagHeader
	case "footer":
		return tagFooter
	case "title":
		return tagTitle
	case "nav":
		return tagNav
	case "aside":
		return tagAside
	case "svg":
		return tagSvg
	case "iframe":
		return tagIframe
	case "form":
		return tagForm
	default:
		return tagOther
	}
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
		nameStr := inner[1:]
		nameEnd := strings.IndexFunc(nameStr, isWhitespaceOrGT)
		if nameEnd < 0 {
			nameEnd = len(nameStr)
		}
		nameLower := strings.ToLower(nameStr[:nameEnd])
		tag := tagNameFromLower(nameLower)
		*tokens = append(*tokens, token{kind: tokenTagClose, tag: tag})
		return gt + 1
	}

	// Opening or self-closing tag. Extract tag name.
	nameEnd := strings.IndexFunc(inner, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '/' || r == '>'
	})
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
	isSelfClosing := strings.HasSuffix(inner, "/") || isVoidTag(tag)

	if isSelfClosing {
		*tokens = append(*tokens, token{kind: tokenSelfClosing, tag: tag, raw: tagStr})
	} else {
		*tokens = append(*tokens, token{kind: tokenTagOpen, tag: tag, raw: tagStr})
	}

	// For script/style/noscript: find matching close tag, emit raw content.
	if tag == tagScript || tag == tagStyle || tag == tagNoscript {
		closeTag := "</" + nameLower + ">"
		searchFrom := gt + 1
		lowerRest := strings.ToLower(input[searchFrom:])
		closeRel := strings.Index(lowerRest, closeTag)
		if closeRel >= 0 {
			contentEnd := searchFrom + closeRel
			if contentEnd > searchFrom {
				*tokens = append(*tokens, token{kind: tokenText, text: input[searchFrom:contentEnd]})
			}
			*tokens = append(*tokens, token{kind: tokenTagClose, tag: tag})
			return contentEnd + len(closeTag)
		}
		// No closing tag found — rest is all suppressed content.
		if searchFrom < len(input) {
			*tokens = append(*tokens, token{kind: tokenText, text: input[searchFrom:]})
		}
		return len(input)
	}

	return gt + 1
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
