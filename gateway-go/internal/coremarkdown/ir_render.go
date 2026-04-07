package coremarkdown

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Render state (mirrors Rust RenderState)
// ---------------------------------------------------------------------------

type openStyle struct {
	style MarkdownStyle
	start int
}

type linkState struct {
	href       string
	labelStart int
}

type listEntry struct {
	ordered bool
	index   int
}

type renderState struct {
	text       strings.Builder
	styles     []StyleSpan
	links      []LinkSpan
	openStyles []openStyle
	linkStack  []linkState
	listStack  []listEntry
	// Config
	headingBold      bool
	blockquotePrefix string
	tableMode        string // "off", "bullets", "code"
	// Table state
	table     *tableState
	hasTables bool
}

func newRenderState(opts *ParseOptions) *renderState {
	return &renderState{
		headingBold:      opts.HeadingStyle == "bold",
		blockquotePrefix: opts.BlockquotePrefix,
		tableMode:        opts.TableMode,
	}
}

func (rs *renderState) textLen() int {
	if rs.table != nil && rs.table.inCell {
		return rs.table.cellText.Len()
	}
	return rs.text.Len()
}

func (rs *renderState) appendText(s string) {
	if len(s) == 0 {
		return
	}
	if rs.table != nil && rs.table.inCell {
		rs.table.cellText.WriteString(s)
	} else {
		rs.text.WriteString(s)
	}
}

func (rs *renderState) stylesSlice() *[]StyleSpan {
	if rs.table != nil && rs.table.inCell {
		return &rs.table.cellStyles
	}
	return &rs.styles
}

func (rs *renderState) openStylesSlice() *[]openStyle {
	if rs.table != nil && rs.table.inCell {
		return &rs.table.cellOpen
	}
	return &rs.openStyles
}

func (rs *renderState) linksSlice() *[]LinkSpan {
	if rs.table != nil && rs.table.inCell {
		return &rs.table.cellLinks
	}
	return &rs.links
}

func (rs *renderState) linkStackSlice() *[]linkState {
	if rs.table != nil && rs.table.inCell {
		return &rs.table.cellLink
	}
	return &rs.linkStack
}

func (rs *renderState) openStyle(style MarkdownStyle) {
	start := rs.textLen()
	s := rs.openStylesSlice()
	*s = append(*s, openStyle{style: style, start: start})
}

func (rs *renderState) closeStyle(style MarkdownStyle) {
	s := rs.openStylesSlice()
	for i := len(*s) - 1; i >= 0; i-- {
		if (*s)[i].style == style {
			start := (*s)[i].start
			*s = append((*s)[:i], (*s)[i+1:]...)
			end := rs.textLen()
			if end > start {
				sl := rs.stylesSlice()
				*sl = append(*sl, StyleSpan{Start: start, End: end, Style: style})
			}
			return
		}
	}
}

func (rs *renderState) closeRemainingStyles() {
	end := rs.text.Len()
	for i := len(rs.openStyles) - 1; i >= 0; i-- {
		o := rs.openStyles[i]
		if end > o.start {
			rs.styles = append(rs.styles, StyleSpan{Start: o.start, End: end, Style: o.style})
		}
	}
	rs.openStyles = rs.openStyles[:0]
}

func (rs *renderState) handleLinkOpen(href string) {
	start := rs.textLen()
	s := rs.linkStackSlice()
	*s = append(*s, linkState{href: href, labelStart: start})
}

func (rs *renderState) handleLinkClose() {
	s := rs.linkStackSlice()
	if len(*s) == 0 {
		return
	}
	link := (*s)[len(*s)-1]
	*s = (*s)[:len(*s)-1]
	href := strings.TrimSpace(link.href)
	if href == "" {
		return
	}
	end := rs.textLen()
	ls := rs.linksSlice()
	*ls = append(*ls, LinkSpan{Start: link.labelStart, End: end, Href: href})
}

func (rs *renderState) appendParagraphSep() {
	if len(rs.listStack) > 0 || rs.table != nil {
		return
	}
	rs.text.WriteString("\n\n")
}

func (rs *renderState) appendListPrefix() {
	depth := len(rs.listStack)
	top := &rs.listStack[depth-1]
	top.index++
	for i := 0; i < depth-1; i++ {
		rs.text.WriteString("  ")
	}
	if top.ordered {
		rs.text.WriteString(fmt.Sprintf("%d. ", top.index))
	} else {
		rs.text.WriteString("• ")
	}
}

func (rs *renderState) renderInlineCode(content string) {
	if content == "" {
		return
	}
	start := rs.textLen()
	rs.appendText(content)
	end := rs.textLen()
	sl := rs.stylesSlice()
	*sl = append(*sl, StyleSpan{Start: start, End: end, Style: StyleCode})
}

func (rs *renderState) renderCodeBlock(content string) {
	code := content
	if !strings.HasSuffix(code, "\n") {
		code += "\n"
	}
	start := rs.textLen()
	rs.appendText(code)
	end := rs.textLen()
	sl := rs.stylesSlice()
	*sl = append(*sl, StyleSpan{Start: start, End: end, Style: StyleCodeBlock})
	if len(rs.listStack) == 0 {
		rs.appendText("\n")
	}
}
