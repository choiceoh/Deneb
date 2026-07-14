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
	switch {
	case ctx.inTitle:
		ctx.titleBuf.WriteString(s)
	case ctx.inLink:
		ctx.linkBuf.WriteString(s)
	case ctx.inBlockquote:
		ctx.blockquoteBuf.WriteString(s)
	case ctx.inTable && ctx.tableBuilder.inCell:
		ctx.tableBuilder.cellBuf.WriteString(s)
	default:
		ctx.out.WriteString(s)
	}
}

// pushChar writes a single rune to the active buffer.
func (ctx *emitCtx) pushChar(ch rune) {
	switch {
	case ctx.inTitle:
		ctx.titleBuf.WriteRune(ch)
	case ctx.inLink:
		ctx.linkBuf.WriteRune(ch)
	case ctx.inBlockquote:
		ctx.blockquoteBuf.WriteRune(ch)
	case ctx.inTable:
		ctx.tableBuilder.pushChar(ch)
	default:
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
func emit(tokens []token, inputLen int, stripNoise bool) (string, *string) { //nolint:gocritic // unnamedResult — naming would shadow local 'title' var
	ctx := newEmitCtx(inputLen)

	for i := range tokens {
		tok := &tokens[i]

		// --- Suppressed content ---
		if ctx.suppressDepth > 0 {
			trackSuppressed(ctx, tok, stripNoise)
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

// trackSuppressed maintains the suppression nesting depth while content of a
// suppressed element (script/style/noscript, or noise tags) is being skipped.
func trackSuppressed(ctx *emitCtx, tok *token, stripNoise bool) {
	alwaysSuppressed := tok.tag == tagScript || tok.tag == tagStyle || tok.tag == tagNoscript
	noiseSuppressed := stripNoise && isNoiseTag(tok.tag)
	if !alwaysSuppressed && !noiseSuppressed {
		return
	}
	switch tok.kind {
	case tokenTagOpen:
		ctx.suppressDepth++
	case tokenTagClose:
		ctx.suppressDepth--
		if ctx.suppressDepth < 0 {
			ctx.suppressDepth = 0
		}
	}
}

// inlineMarks maps inline emphasis tags to their symmetric Markdown marker,
// pushed on both open and close.
var inlineMarks = map[tagName]string{
	tagStrong: "**", tagB: "**",
	tagEm: "*", tagI: "*",
	tagS: "~~", tagDel: "~~", tagStrike: "~~",
}

func emitTagOpen(ctx *emitCtx, tok *token, stripNoise bool) {
	if mark, ok := inlineMarks[tok.tag]; ok {
		ctx.push(mark)
		return
	}
	switch tok.tag {
	// --- Suppression start ---
	case tagScript, tagStyle, tagNoscript:
		ctx.suppressDepth++
	case tagNav, tagAside, tagSvg, tagIframe, tagForm:
		// When not stripping noise, content flows as text.
		if stripNoise {
			ctx.suppressDepth++
		}

	// --- Compound elements buffered until close ---
	case tagTitle:
		ctx.inTitle = true
		ctx.titleBuf.Reset()
	case tagA:
		ctx.inLink = true
		ctx.linkHref = extractAttr(tok.raw, "href")
		ctx.linkBuf.Reset()
	case tagBlockquote:
		ctx.inBlockquote = true
		ctx.blockquoteBuf.Reset()
	case tagTable, tagTr, tagTh, tagTd:
		openTableTag(ctx, tok.tag)

	// --- Code blocks ---
	case tagPre:
		ctx.inPre = true
	case tagCode:
		openCode(ctx, tok.raw)

	// --- Headings ---
	case tagH1, tagH2, tagH3, tagH4, tagH5, tagH6:
		openHeading(ctx, tok.tag)

	// --- Lists ---
	case tagOl, tagUl:
		ctx.listStack = append(ctx.listStack, listCtx{ordered: tok.tag == tagOl})
	case tagLi:
		openListItem(ctx)

	// --- Void elements (same handling as their self-closing form) ---
	case tagBr, tagHr, tagImg:
		emitSelfClosing(ctx, tok)

	default:
		// Block elements (p/div/section/...) emit nothing on open (close
		// emits a newline); tagOther has no special handling.
	}
}

func openCode(ctx *emitCtx, raw string) {
	if !ctx.inPre {
		ctx.push("`")
		return
	}
	ctx.inCodeInPre = true
	lang := extractCodeLanguage(raw)
	ctx.out.WriteString("\n```")
	ctx.out.WriteString(lang)
	ctx.out.WriteByte('\n')
}

func openHeading(ctx *emitCtx, tag tagName) {
	ctx.out.WriteByte('\n')
	for range headingLevel(tag) {
		ctx.out.WriteByte('#')
	}
	ctx.out.WriteByte(' ')
}

func openListItem(ctx *emitCtx) {
	n := len(ctx.listStack)
	if n == 0 || !ctx.listStack[n-1].ordered {
		ctx.out.WriteString("\n- ")
		return
	}
	lc := &ctx.listStack[n-1]
	lc.counter++
	fmt.Fprintf(&ctx.out, "\n%d. ", lc.counter)
}

func openTableTag(ctx *emitCtx, tag tagName) {
	if tag == tagTable {
		ctx.inTable = true
		ctx.tableBuilder = tableBuilder{}
		return
	}
	if !ctx.inTable {
		return
	}
	switch tag {
	case tagTr:
		ctx.tableBuilder.startRow()
	case tagTh:
		ctx.tableBuilder.endCell()
		ctx.tableBuilder.startCell(true)
	case tagTd:
		ctx.tableBuilder.endCell()
		ctx.tableBuilder.startCell(false)
	}
}

func emitTagClose(ctx *emitCtx, tok *token) {
	if mark, ok := inlineMarks[tok.tag]; ok {
		ctx.push(mark)
		return
	}
	switch tok.tag {
	case tagTitle:
		closeTitle(ctx)
	case tagA:
		closeLink(ctx)
	case tagPre:
		closePre(ctx)
	case tagCode:
		closeCode(ctx)
	case tagBlockquote:
		closeBlockquote(ctx)
	case tagTable, tagTr, tagTh, tagTd:
		closeTableTag(ctx, tok.tag)
	case tagOl, tagUl:
		if len(ctx.listStack) > 0 {
			ctx.listStack = ctx.listStack[:len(ctx.listStack)-1]
		}
	case tagH1, tagH2, tagH3, tagH4, tagH5, tagH6,
		tagP, tagDiv, tagSection, tagArticle, tagHeader, tagFooter:
		// Headings and block elements close with a newline.
		ctx.out.WriteByte('\n')
	default:
		// tagLi, tagOther, suppression tags, etc.: no close action.
	}
}

func closeTitle(ctx *emitCtx) {
	ctx.inTitle = false
	t := normalizeInline(ctx.titleBuf.String())
	if t != "" {
		ctx.title = &t
	}
}

// closeLink flushes the buffered link as [label](href), bare href, or bare label.
func closeLink(ctx *emitCtx) {
	if !ctx.inLink {
		return
	}
	label := normalizeInline(ctx.linkBuf.String())
	href := ctx.linkHref
	ctx.inLink = false
	ctx.linkHref = ""
	target := ctx.activeBuf()
	if href == "" {
		target.WriteString(label)
		return
	}
	if label == "" {
		target.WriteString(href)
		return
	}
	target.WriteByte('[')
	target.WriteString(label)
	target.WriteString("](")
	target.WriteString(href)
	target.WriteByte(')')
}

func closePre(ctx *emitCtx) {
	if ctx.inPre && !ctx.inCodeInPre {
		ctx.out.WriteString("\n```\n")
	}
	ctx.inPre = false
	ctx.inCodeInPre = false
}

func closeCode(ctx *emitCtx) {
	if !ctx.inCodeInPre {
		ctx.push("`")
		return
	}
	ctx.out.WriteString("\n```\n")
	ctx.inCodeInPre = false
}

func closeBlockquote(ctx *emitCtx) {
	if !ctx.inBlockquote {
		return
	}
	ctx.inBlockquote = false
	text := normalizeInline(ctx.blockquoteBuf.String())
	if text == "" {
		return
	}
	ctx.out.WriteByte('\n')
	for _, line := range strings.Split(text, "\n") {
		ctx.out.WriteString("> ")
		ctx.out.WriteString(line)
		ctx.out.WriteByte('\n')
	}
}

func closeTableTag(ctx *emitCtx, tag tagName) {
	if !ctx.inTable {
		return
	}
	switch tag {
	case tagTable:
		md := ctx.tableBuilder.toMarkdown()
		if md != "" {
			ctx.out.WriteByte('\n')
			ctx.out.WriteString(md)
		}
		ctx.inTable = false
	case tagTr:
		ctx.tableBuilder.endCell()
		ctx.tableBuilder.endRow()
	case tagTh, tagTd:
		ctx.tableBuilder.endCell()
	}
}

func emitSelfClosing(ctx *emitCtx, tok *token) {
	switch tok.tag {
	case tagBr, tagHr:
		ctx.out.WriteByte('\n')
	case tagImg:
		emitImage(ctx, tok.raw)
	default:
		// Other self-closing tags: no special handling.
	}
}

func emitText(ctx *emitCtx, s string) {
	switch {
	case ctx.inTitle:
		ctx.titleBuf.WriteString(s)
	case ctx.inLink:
		ctx.linkBuf.WriteString(s)
	case ctx.inBlockquote:
		ctx.blockquoteBuf.WriteString(s)
	case ctx.inTable:
		ctx.tableBuilder.pushText(s)
	default:
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
	// Start after the tag name. Attribute lookup must be token-aware: a raw
	// substring search would mistake data-href for href (or an href= fragment
	// inside another attribute's quoted value).
	i := 1
	for i < len(tag) && !isAttrSpace(tag[i]) && tag[i] != '>' {
		i++
	}
	for i < len(tag) {
		name, next, ok := scanAttrName(tag, i)
		if !ok {
			return ""
		}
		i = next
		for i < len(tag) && isAttrSpace(tag[i]) {
			i++
		}
		if i >= len(tag) || tag[i] != '=' {
			// Boolean attribute. Whitespace was already consumed, so i is at
			// either the next attribute name or the end of the tag.
			continue
		}
		valueStart, valueEnd, valueNext, valueOK := scanAttrValue(tag, i+1)
		if !valueOK {
			return ""
		}
		i = valueNext
		if strings.EqualFold(name, attr) {
			return tag[valueStart:valueEnd]
		}
	}
	return ""
}

// scanAttrName skips whitespace and '/' then scans one attribute name starting
// at i. Returns the name and the index just past it; ok=false when the tag
// ends (or '>' is reached) before another attribute starts.
func scanAttrName(tag string, i int) (name string, next int, ok bool) {
	for i < len(tag) && (isAttrSpace(tag[i]) || tag[i] == '/') {
		i++
	}
	if i >= len(tag) || tag[i] == '>' {
		return "", 0, false
	}
	start := i
	for i < len(tag) && !isAttrSpace(tag[i]) && tag[i] != '=' && tag[i] != '>' && tag[i] != '/' {
		i++
	}
	return tag[start:i], i, true
}

// scanAttrValue scans an attribute value starting just past '='. Returns the
// value bounds and the index past the value; ok=false for a truncated tag
// (missing value or unterminated quote).
func scanAttrValue(tag string, i int) (start, end, next int, ok bool) {
	for i < len(tag) && isAttrSpace(tag[i]) {
		i++
	}
	if i >= len(tag) {
		return 0, 0, 0, false
	}
	if tag[i] == '"' || tag[i] == '\'' {
		quote := tag[i]
		start = i + 1
		end = start
		for end < len(tag) && tag[end] != quote {
			end++
		}
		if end >= len(tag) {
			return 0, 0, 0, false
		}
		return start, end, end + 1, true
	}
	start = i
	end = i
	for end < len(tag) && !isAttrSpace(tag[end]) && tag[end] != '>' {
		end++
	}
	return start, end, end, true
}

func isAttrSpace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
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
