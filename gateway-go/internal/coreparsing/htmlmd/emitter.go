package htmlmd

import (
	"fmt"
	"strings"
)

// emitCtx tracks nested HTML context and routes text output to the correct
// buffer (main output, link, blockquote, table cell, or title).
type emitCtx struct {
	out   strings.Builder
	title *string

	// Suppression depth for script/style/noscript (+ noise tags).
	suppressDepth int

	// Block state.
	listStack   []listCtx
	inPre       bool
	inCodeInPre bool

	// Compound element buffers.
	inTitle  bool
	titleBuf strings.Builder

	inLink   bool
	linkHref string
	linkBuf  strings.Builder

	inBlockquote  bool
	blockquoteBuf strings.Builder

	inTable      bool
	tableBuilder tableBuilder
}

type listCtx struct {
	ordered bool
	counter int
}

func newEmitCtx(capacity int) *emitCtx {
	ctx := &emitCtx{}
	ctx.out.Grow(capacity)
	return ctx
}

// push writes a string to whichever buffer is currently active.
func (ctx *emitCtx) push(s string) {
	if ctx.inTitle {
		ctx.titleBuf.WriteString(s)
	} else if ctx.inLink {
		ctx.linkBuf.WriteString(s)
	} else if ctx.inBlockquote {
		ctx.blockquoteBuf.WriteString(s)
	} else if ctx.inTable && ctx.tableBuilder.inCell {
		ctx.tableBuilder.cellBuf.WriteString(s)
	} else {
		ctx.out.WriteString(s)
	}
}

// pushChar writes a single rune to the active buffer.
func (ctx *emitCtx) pushChar(ch rune) {
	if ctx.inTitle {
		ctx.titleBuf.WriteRune(ch)
	} else if ctx.inLink {
		ctx.linkBuf.WriteRune(ch)
	} else if ctx.inBlockquote {
		ctx.blockquoteBuf.WriteRune(ch)
	} else if ctx.inTable {
		ctx.tableBuilder.pushChar(ch)
	} else {
		ctx.out.WriteRune(ch)
	}
}

// activeBuf returns the active output builder for compound element emission.
func (ctx *emitCtx) activeBuf() *strings.Builder {
	if ctx.inBlockquote {
		return &ctx.blockquoteBuf
	}
	if ctx.inTable && ctx.tableBuilder.inCell {
		return &ctx.tableBuilder.cellBuf
	}
	return &ctx.out
}

// emit walks the token stream and produces Markdown. Returns (text, title).
func emit(tokens []token, inputLen int, stripNoise bool) (string, *string) {
	ctx := newEmitCtx(inputLen)

	for i := range tokens {
		tok := &tokens[i]

		// --- Suppressed content ---
		if ctx.suppressDepth > 0 {
			if tok.kind == tokenTagClose {
				alwaysSuppressed := tok.tag == tagScript || tok.tag == tagStyle || tok.tag == tagNoscript
				noiseSuppressed := stripNoise && isNoiseTag(tok.tag)
				if alwaysSuppressed || noiseSuppressed {
					ctx.suppressDepth--
					if ctx.suppressDepth < 0 {
						ctx.suppressDepth = 0
					}
				}
			}
			continue
		}

		switch tok.kind {
		case tokenTagOpen:
			emitTagOpen(ctx, tok, stripNoise)
		case tokenTagClose:
			emitTagClose(ctx, tok)
		case tokenSelfClosing:
			emitSelfClosing(ctx, tok)
		case tokenText:
			emitText(ctx, tok.text)
		case tokenEntity:
			ctx.pushChar(tok.entity)
		case tokenAmpersandLiteral:
			ctx.pushChar('&')
		}
	}

	var title *string
	if ctx.title != nil {
		title = ctx.title
	}
	return ctx.out.String(), title
}

func emitTagOpen(ctx *emitCtx, tok *token, stripNoise bool) {
	switch tok.tag {
	// --- Suppression start ---
	case tagScript, tagStyle, tagNoscript:
		ctx.suppressDepth++
		return
	case tagNav, tagAside, tagSvg, tagIframe, tagForm:
		if stripNoise {
			ctx.suppressDepth++
			return
		}
		// When not stripping noise, fall through — content flows as text.
		return

	// --- Title ---
	case tagTitle:
		ctx.inTitle = true
		ctx.titleBuf.Reset()

	// --- Links ---
	case tagA:
		ctx.inLink = true
		ctx.linkHref = extractAttr(tok.raw, "href")
		ctx.linkBuf.Reset()

	// --- Emphasis (bold) ---
	case tagStrong, tagB:
		ctx.push("**")

	// --- Emphasis (italic) ---
	case tagEm, tagI:
		ctx.push("*")

	// --- Strikethrough ---
	case tagS, tagDel, tagStrike:
		ctx.push("~~")

	// --- Pre blocks ---
	case tagPre:
		ctx.inPre = true

	// --- Code ---
	case tagCode:
		if ctx.inPre {
			ctx.inCodeInPre = true
			lang := extractCodeLanguage(tok.raw)
			ctx.out.WriteString("\n```")
			ctx.out.WriteString(lang)
			ctx.out.WriteByte('\n')
		} else {
			ctx.push("`")
		}

	// --- Headings ---
	case tagH1, tagH2, tagH3, tagH4, tagH5, tagH6:
		level := headingLevel(tok.tag)
		ctx.out.WriteByte('\n')
		for range level {
			ctx.out.WriteByte('#')
		}
		ctx.out.WriteByte(' ')

	// --- Images ---
	case tagImg:
		emitImage(ctx, tok.raw)

	// --- Blockquotes ---
	case tagBlockquote:
		ctx.inBlockquote = true
		ctx.blockquoteBuf.Reset()

	// --- Tables ---
	case tagTable:
		ctx.inTable = true
		ctx.tableBuilder = tableBuilder{}
	case tagTr:
		if ctx.inTable {
			ctx.tableBuilder.startRow()
		}
	case tagTh:
		if ctx.inTable {
			ctx.tableBuilder.endCell()
			ctx.tableBuilder.startCell(true)
		}
	case tagTd:
		if ctx.inTable {
			ctx.tableBuilder.endCell()
			ctx.tableBuilder.startCell(false)
		}

	// --- Lists ---
	case tagOl:
		ctx.listStack = append(ctx.listStack, listCtx{ordered: true})
	case tagUl:
		ctx.listStack = append(ctx.listStack, listCtx{ordered: false})
	case tagLi:
		if n := len(ctx.listStack); n > 0 {
			lc := &ctx.listStack[n-1]
			if lc.ordered {
				lc.counter++
				ctx.out.WriteString(fmt.Sprintf("\n%d. ", lc.counter))
			} else {
				ctx.out.WriteString("\n- ")
			}
		} else {
			ctx.out.WriteString("\n- ")
		}

	// --- Line breaks ---
	case tagBr, tagHr:
		ctx.out.WriteByte('\n')

	// --- Block elements: opening does nothing (close emits newline) ---
	case tagP, tagDiv, tagSection, tagArticle, tagHeader, tagFooter:
		// No output on open.
	}
}

func emitTagClose(ctx *emitCtx, tok *token) {
	switch tok.tag {
	case tagTitle:
		ctx.inTitle = false
		t := normalizeInline(ctx.titleBuf.String())
		if t != "" {
			ctx.title = &t
		}

	case tagA:
		if ctx.inLink {
			label := normalizeInline(ctx.linkBuf.String())
			href := ctx.linkHref
			target := ctx.activeBuf()
			if href != "" {
				if label == "" {
					target.WriteString(href)
				} else {
					target.WriteByte('[')
					target.WriteString(label)
					target.WriteString("](")
					target.WriteString(href)
					target.WriteByte(')')
				}
			} else {
				target.WriteString(label)
			}
			ctx.inLink = false
			ctx.linkHref = ""
		}

	case tagStrong, tagB:
		ctx.push("**")
	case tagEm, tagI:
		ctx.push("*")
	case tagS, tagDel, tagStrike:
		ctx.push("~~")

	case tagPre:
		if ctx.inPre && !ctx.inCodeInPre {
			ctx.out.WriteString("\n```\n")
		}
		ctx.inPre = false
		ctx.inCodeInPre = false

	case tagCode:
		if ctx.inCodeInPre {
			ctx.out.WriteString("\n```\n")
			ctx.inCodeInPre = false
		} else {
			ctx.push("`")
		}

	case tagH1, tagH2, tagH3, tagH4, tagH5, tagH6:
		ctx.out.WriteByte('\n')

	case tagBlockquote:
		if ctx.inBlockquote {
			text := normalizeInline(ctx.blockquoteBuf.String())
			if text != "" {
				ctx.out.WriteByte('\n')
				for _, line := range strings.Split(text, "\n") {
					ctx.out.WriteString("> ")
					ctx.out.WriteString(line)
					ctx.out.WriteByte('\n')
				}
			}
			ctx.inBlockquote = false
		}

	case tagTable:
		if ctx.inTable {
			md := ctx.tableBuilder.toMarkdown()
			if md != "" {
				ctx.out.WriteByte('\n')
				ctx.out.WriteString(md)
			}
			ctx.inTable = false
		}
	case tagTr:
		if ctx.inTable {
			ctx.tableBuilder.endCell()
			ctx.tableBuilder.endRow()
		}
	case tagTh, tagTd:
		if ctx.inTable {
			ctx.tableBuilder.endCell()
		}

	case tagOl, tagUl:
		if len(ctx.listStack) > 0 {
			ctx.listStack = ctx.listStack[:len(ctx.listStack)-1]
		}

	case tagLi:
		// No action needed.

	case tagP, tagDiv, tagSection, tagArticle, tagHeader, tagFooter:
		ctx.out.WriteByte('\n')
	}
}

func emitSelfClosing(ctx *emitCtx, tok *token) {
	switch tok.tag {
	case tagBr, tagHr:
		ctx.out.WriteByte('\n')
	case tagImg:
		emitImage(ctx, tok.raw)
	}
}

func emitText(ctx *emitCtx, s string) {
	if ctx.inTitle {
		ctx.titleBuf.WriteString(s)
	} else if ctx.inLink {
		ctx.linkBuf.WriteString(s)
	} else if ctx.inBlockquote {
		ctx.blockquoteBuf.WriteString(s)
	} else if ctx.inTable {
		ctx.tableBuilder.pushText(s)
	} else {
		ctx.out.WriteString(s)
	}
}

func emitImage(ctx *emitCtx, raw string) {
	src := extractAttr(raw, "src")
	if src == "" {
		return
	}
	alt := extractAttr(raw, "alt")
	label := alt
	if label == "" {
		label = filenameFromURL(src)
	}
	target := ctx.activeBuf()
	target.WriteByte('[')
	target.WriteString(label)
	target.WriteString("](")
	target.WriteString(src)
	target.WriteByte(')')
}

// --- Attribute extraction helpers ---

// extractAttr extracts an attribute value from a raw tag string.
// Handles quoted ("value", 'value') and unquoted attribute values.
func extractAttr(tag, attr string) string {
	lower := strings.ToLower(tag)
	pattern := attr + "="
	idx := strings.Index(lower, pattern)
	if idx < 0 {
		return ""
	}
	afterEq := idx + len(pattern)
	if afterEq >= len(tag) {
		return ""
	}
	quote := tag[afterEq]
	if quote == '"' || quote == '\'' {
		start := afterEq + 1
		if start >= len(tag) {
			return ""
		}
		end := strings.IndexByte(tag[start:], quote)
		if end < 0 {
			return ""
		}
		return tag[start : start+end]
	}
	// Unquoted: read until whitespace or >.
	rest := tag[afterEq:]
	end := strings.IndexFunc(rest, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '>'
	})
	if end < 0 {
		return rest
	}
	return rest[:end]
}

// extractCodeLanguage extracts language from <code class="language-X"> or "lang-X".
func extractCodeLanguage(tag string) string {
	class := extractAttr(tag, "class")
	if class == "" {
		return ""
	}
	for _, prefix := range []string{"language-", "lang-"} {
		if after, ok := strings.CutPrefix(class, prefix); ok {
			lang, _, _ := strings.Cut(after, " ")
			if lang != "" {
				return lang
			}
		}
	}
	return ""
}

// filenameFromURL extracts a filename from a URL for use as an image label.
func filenameFromURL(url string) string {
	lastSlash := strings.LastIndexByte(url, '/')
	name := url
	if lastSlash >= 0 {
		name = url[lastSlash+1:]
	}
	if qmark := strings.IndexByte(name, '?'); qmark >= 0 {
		name = name[:qmark]
	}
	if name == "" {
		return "image"
	}
	return name
}

func headingLevel(tag tagName) int {
	switch tag {
	case tagH1:
		return 1
	case tagH2:
		return 2
	case tagH3:
		return 3
	case tagH4:
		return 4
	case tagH5:
		return 5
	case tagH6:
		return 6
	default:
		return 1
	}
}

func isNoiseTag(tag tagName) bool {
	return tag == tagNav || tag == tagAside || tag == tagSvg || tag == tagIframe || tag == tagForm
}
