package coremarkdown

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// ---------------------------------------------------------------------------
// AST walker
// ---------------------------------------------------------------------------

// MarkdownToIRWithMeta parses markdown into IR plus a has_tables flag.
func MarkdownToIRWithMeta(markdown string, opts *ParseOptions) (MarkdownIR, bool) {
	if opts == nil {
		d := DefaultParseOptions()
		opts = &d
	}

	input := markdown
	if opts.EnableSpoilers {
		input = preprocessSpoilers(markdown)
	}

	// Build goldmark parser with extensions matching Rust pulldown-cmark config.
	gmOpts := []goldmark.Option{
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
	}
	// Strikethrough is always enabled (matches Rust).
	// Table extension enabled when tableMode != "off".
	exts := []goldmark.Extender{extension.Strikethrough}
	if opts.TableMode != "off" {
		exts = append(exts, extension.Table)
	}
	gmOpts = append(gmOpts, goldmark.WithExtensions(exts...))

	md := goldmark.New(gmOpts...)
	source := []byte(input)
	doc := md.Parser().Parse(text.NewReader(source))

	rs := newRenderState(opts)
	walkNode(doc, source, rs, opts)
	rs.closeRemainingStyles()

	// Post-process spoiler sentinels that survived AST walking.
	// Goldmark may split text nodes at zero-width characters, preventing
	// inline detection. This pass strips sentinels and adds Spoiler spans.
	if opts.EnableSpoilers {
		postProcessSpoilers(rs)
	}

	// Final trimming (matching Rust behavior).
	fullText := rs.text.String()
	trimmedLen := len(strings.TrimRight(fullText, " \t\n\r"))
	codeBlockEnd := 0
	for _, sp := range rs.styles {
		if sp.Style == StyleCodeBlock && sp.End > codeBlockEnd {
			codeBlockEnd = sp.End
		}
	}
	finalLen := trimmedLen
	if codeBlockEnd > finalLen {
		finalLen = codeBlockEnd
	}
	if finalLen < len(fullText) {
		fullText = fullText[:finalLen]
	}

	styles := mergeStyleSpans(clampStyleSpans(rs.styles, finalLen))
	links := clampLinkSpans(rs.links, finalLen)

	return MarkdownIR{Text: fullText, Styles: styles, Links: links}, rs.hasTables
}

// MarkdownToIR parses markdown into IR (without the has_tables flag).
func MarkdownToIR(markdown string, opts *ParseOptions) MarkdownIR {
	ir, _ := MarkdownToIRWithMeta(markdown, opts)
	return ir
}

func walkNode(n ast.Node, source []byte, rs *renderState, opts *ParseOptions) {
	switch n.Kind() {
	case ast.KindDocument:
		walkChildren(n, source, rs, opts)

	case ast.KindParagraph:
		walkChildren(n, source, rs, opts)
		rs.appendParagraphSep()

	case ast.KindHeading:
		if rs.headingBold {
			rs.openStyle(StyleBold)
		}
		walkChildren(n, source, rs, opts)
		if rs.headingBold {
			rs.closeStyle(StyleBold)
		}
		rs.appendParagraphSep()

	case ast.KindBlockquote:
		if rs.blockquotePrefix != "" {
			rs.appendText(rs.blockquotePrefix)
		}
		rs.openStyle(StyleBlockquote)
		walkChildren(n, source, rs, opts)
		rs.closeStyle(StyleBlockquote)

	case ast.KindFencedCodeBlock, ast.KindCodeBlock:
		var buf strings.Builder
		for i := 0; i < n.Lines().Len(); i++ {
			seg := n.Lines().At(i)
			buf.Write(seg.Value(source))
		}
		rs.renderCodeBlock(buf.String())

	case ast.KindList:
		list := n.(*ast.List)
		if len(rs.listStack) > 0 {
			rs.text.WriteByte('\n')
		}
		if list.IsOrdered() {
			rs.listStack = append(rs.listStack, listEntry{ordered: true, index: int(list.Start) - 1})
		} else {
			rs.listStack = append(rs.listStack, listEntry{ordered: false})
		}
		walkChildren(n, source, rs, opts)
		rs.listStack = rs.listStack[:len(rs.listStack)-1]
		if len(rs.listStack) == 0 {
			rs.text.WriteByte('\n')
		}

	case ast.KindListItem:
		rs.appendListPrefix()
		walkChildren(n, source, rs, opts)
		if t := rs.text.String(); !strings.HasSuffix(t, "\n") {
			rs.text.WriteByte('\n')
		}

	case ast.KindThematicBreak:
		rs.text.WriteString("───\n\n")

	case ast.KindEmphasis:
		em := n.(*ast.Emphasis)
		if em.Level == 2 {
			rs.openStyle(StyleBold)
			walkChildren(n, source, rs, opts)
			rs.closeStyle(StyleBold)
		} else {
			rs.openStyle(StyleItalic)
			walkChildren(n, source, rs, opts)
			rs.closeStyle(StyleItalic)
		}

	case ast.KindCodeSpan:
		var buf strings.Builder
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			if c.Kind() == ast.KindText {
				buf.Write(c.Text(source))
			}
		}
		rs.renderInlineCode(buf.String())

	case ast.KindLink:
		link := n.(*ast.Link)
		rs.handleLinkOpen(string(link.Destination))
		walkChildren(n, source, rs, opts)
		rs.handleLinkClose()

	case ast.KindAutoLink:
		al := n.(*ast.AutoLink)
		url := string(al.URL(source))
		rs.handleLinkOpen(url)
		rs.appendText(url)
		rs.handleLinkClose()

	case ast.KindImage:
		// Emit alt text (matching pulldown-cmark behavior).
		walkChildren(n, source, rs, opts)

	case ast.KindText:
		t := n.(*ast.Text)
		segment := t.Segment
		content := string(segment.Value(source))
		if opts.EnableSpoilers && (strings.Contains(content, sentinelOpen) || strings.Contains(content, sentinelClose)) {
			handleSpoilerText(rs, content)
		} else {
			rs.appendText(content)
		}
		if t.SoftLineBreak() || t.HardLineBreak() {
			rs.appendText("\n")
		}

	case ast.KindRawHTML:
		for i := 0; i < n.Lines().Len(); i++ {
			seg := n.Lines().At(i)
			rs.appendText(string(seg.Value(source)))
		}

	case ast.KindHTMLBlock:
		for i := 0; i < n.Lines().Len(); i++ {
			seg := n.Lines().At(i)
			rs.appendText(string(seg.Value(source)))
		}

	default:
		// Handle goldmark extension nodes.
		if handleExtensionNode(n, source, rs, opts) {
			return
		}
		walkChildren(n, source, rs, opts)
	}
}

func handleExtensionNode(n ast.Node, source []byte, rs *renderState, opts *ParseOptions) bool {
	switch n.Kind() {
	case east.KindStrikethrough:
		rs.openStyle(StyleStrikethrough)
		walkChildren(n, source, rs, opts)
		rs.closeStyle(StyleStrikethrough)
		return true

	case east.KindTable:
		if rs.tableMode == "off" {
			walkChildren(n, source, rs, opts)
			return true
		}
		rs.table = &tableState{}
		rs.hasTables = true
		walkChildren(n, source, rs, opts)
		switch rs.tableMode {
		case "bullets":
			rs.renderTableAsBullets()
		case "code":
			rs.renderTableAsCode()
		}
		rs.table = nil
		return true

	case east.KindTableHeader:
		if rs.table != nil {
			rs.table.inHeader = true
		}
		walkChildren(n, source, rs, opts)
		if rs.table != nil {
			if rs.table.inHeader && len(rs.table.currentRow) > 0 {
				rs.table.headers = rs.table.currentRow
				rs.table.currentRow = nil
			}
			rs.table.inHeader = false
		}
		return true

	case east.KindTableRow:
		if rs.table != nil {
			rs.table.currentRow = nil
		}
		walkChildren(n, source, rs, opts)
		if rs.table != nil {
			row := rs.table.currentRow
			rs.table.currentRow = nil
			if rs.table.inHeader {
				rs.table.headers = row
			} else {
				rs.table.rows = append(rs.table.rows, row)
			}
		}
		return true

	case east.KindTableCell:
		if rs.table != nil {
			rs.table.inCell = true
			rs.table.cellText.Reset()
			rs.table.cellStyles = nil
			rs.table.cellLinks = nil
			rs.table.cellOpen = nil
			rs.table.cellLink = nil
		}
		walkChildren(n, source, rs, opts)
		if rs.table != nil {
			rs.finishCell()
		}
		return true
	}
	return false
}

func walkChildren(n ast.Node, source []byte, rs *renderState, opts *ParseOptions) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		walkNode(c, source, rs, opts)
	}
}
