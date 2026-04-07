package coremarkdown

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// ---------------------------------------------------------------------------
// LRU cache (matches markdown_cgo.go pattern)
// ---------------------------------------------------------------------------

const cacheMaxEntries = 128

type cacheEntry struct {
	value      json.RawMessage
	lastAccess int64
}

type irCache struct {
	mu        sync.Mutex
	entries   map[uint64]*cacheEntry
	accessCtr int64
}

var cache = &irCache{entries: make(map[uint64]*cacheEntry)}

func fnv1a64(s string) uint64 {
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

func (c *irCache) get(key uint64) (json.RawMessage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.accessCtr++
	e.lastAccess = c.accessCtr
	return e.value, true
}

func (c *irCache) put(key uint64, val json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accessCtr++
	if len(c.entries) >= cacheMaxEntries {
		var lruKey uint64
		lruAccess := c.accessCtr + 1
		for k, e := range c.entries {
			if e.lastAccess < lruAccess {
				lruAccess = e.lastAccess
				lruKey = k
			}
		}
		delete(c.entries, lruKey)
	}
	c.entries[key] = &cacheEntry{value: val, lastAccess: c.accessCtr}
}

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

// tableCell holds accumulated text + spans for a single table cell.
type tableCell struct {
	text   string
	styles []StyleSpan
	links  []LinkSpan
}

type tableState struct {
	headers    []tableCell
	rows       [][]tableCell
	currentRow []tableCell
	// During cell accumulation we capture into a temporary renderState-like target.
	cellText   strings.Builder
	cellStyles []StyleSpan
	cellLinks  []LinkSpan
	cellOpen   []openStyle
	cellLink   []linkState
	inHeader   bool
	inCell     bool
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

// ---------------------------------------------------------------------------
// Table rendering (mirrors core-rs/core/src/markdown/tables.rs)
// ---------------------------------------------------------------------------

func (rs *renderState) finishCell() {
	if rs.table == nil {
		return
	}
	t := rs.table
	// Close remaining open styles in cell.
	end := t.cellText.Len()
	for i := len(t.cellOpen) - 1; i >= 0; i-- {
		o := t.cellOpen[i]
		if end > o.start {
			t.cellStyles = append(t.cellStyles, StyleSpan{Start: o.start, End: end, Style: o.style})
		}
	}
	cell := tableCell{
		text:   t.cellText.String(),
		styles: t.cellStyles,
		links:  t.cellLinks,
	}
	t.currentRow = append(t.currentRow, cell)
	t.cellText.Reset()
	t.cellStyles = nil
	t.cellLinks = nil
	t.cellOpen = nil
	t.cellLink = nil
	t.inCell = false
}

func trimCell(c tableCell) tableCell {
	trimmed := strings.TrimSpace(c.text)
	if trimmed == c.text {
		return c
	}
	if trimmed == "" {
		return tableCell{}
	}
	start := strings.Index(c.text, trimmed)
	tLen := len(trimmed)
	styles := make([]StyleSpan, 0, len(c.styles))
	for _, sp := range c.styles {
		s := clampInt(sp.Start-start, 0, tLen)
		e := clampInt(sp.End-start, 0, tLen)
		if e > s {
			styles = append(styles, StyleSpan{Start: s, End: e, Style: sp.Style})
		}
	}
	links := make([]LinkSpan, 0, len(c.links))
	for _, lk := range c.links {
		s := clampInt(lk.Start-start, 0, tLen)
		e := clampInt(lk.End-start, 0, tLen)
		if e > s {
			links = append(links, LinkSpan{Start: s, End: e, Href: lk.Href})
		}
	}
	return tableCell{text: trimmed, styles: styles, links: links}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (rs *renderState) appendCellWithStyles(c tableCell) {
	if c.text == "" {
		return
	}
	base := rs.text.Len()
	rs.text.WriteString(c.text)
	for _, sp := range c.styles {
		rs.styles = append(rs.styles, StyleSpan{Start: base + sp.Start, End: base + sp.End, Style: sp.Style})
	}
	for _, lk := range c.links {
		rs.links = append(rs.links, LinkSpan{Start: base + lk.Start, End: base + lk.End, Href: lk.Href})
	}
}

func (rs *renderState) renderTableAsBullets() {
	if rs.table == nil {
		return
	}
	t := rs.table
	rs.table = nil
	headers := make([]tableCell, len(t.headers))
	for i, h := range t.headers {
		headers[i] = trimCell(h)
	}
	rows := make([][]tableCell, len(t.rows))
	for i, row := range t.rows {
		rows[i] = make([]tableCell, len(row))
		for j, c := range row {
			rows[i][j] = trimCell(c)
		}
	}
	if len(headers) == 0 && len(rows) == 0 {
		return
	}
	useFirstColAsLabel := len(headers) > 1 && len(rows) > 0
	if useFirstColAsLabel {
		for _, row := range rows {
			if len(row) == 0 {
				continue
			}
			label := row[0]
			if label.text != "" {
				labelStart := rs.text.Len()
				rs.appendCellWithStyles(label)
				labelEnd := rs.text.Len()
				if labelEnd > labelStart {
					rs.styles = append(rs.styles, StyleSpan{Start: labelStart, End: labelEnd, Style: StyleBold})
				}
				rs.text.WriteByte('\n')
			}
			for i := 1; i < len(row); i++ {
				val := row[i]
				if val.text == "" {
					continue
				}
				rs.text.WriteString("• ")
				if i < len(headers) && headers[i].text != "" {
					rs.appendCellWithStyles(headers[i])
					rs.text.WriteString(": ")
				} else {
					rs.text.WriteString(fmt.Sprintf("Column %d: ", i))
				}
				rs.appendCellWithStyles(val)
				rs.text.WriteByte('\n')
			}
			rs.text.WriteByte('\n')
		}
	} else {
		for _, row := range rows {
			for i, cell := range row {
				if cell.text == "" {
					continue
				}
				rs.text.WriteString("• ")
				if i < len(headers) && headers[i].text != "" {
					rs.appendCellWithStyles(headers[i])
					rs.text.WriteString(": ")
				}
				rs.appendCellWithStyles(cell)
				rs.text.WriteByte('\n')
			}
			rs.text.WriteByte('\n')
		}
	}
}

func (rs *renderState) renderTableAsCode() {
	if rs.table == nil {
		return
	}
	t := rs.table
	rs.table = nil
	headers := make([]tableCell, len(t.headers))
	for i, h := range t.headers {
		headers[i] = trimCell(h)
	}
	rows := make([][]tableCell, len(t.rows))
	for i, row := range t.rows {
		rows[i] = make([]tableCell, len(row))
		for j, c := range row {
			rows[i][j] = trimCell(c)
		}
	}
	colCount := len(headers)
	for _, row := range rows {
		if len(row) > colCount {
			colCount = len(row)
		}
	}
	if colCount == 0 {
		return
	}
	widths := make([]int, colCount)
	for i := 0; i < colCount; i++ {
		if i < len(headers) {
			widths[i] = len(headers[i].text)
		}
	}
	for _, row := range rows {
		for i := 0; i < colCount && i < len(row); i++ {
			if len(row[i].text) > widths[i] {
				widths[i] = len(row[i].text)
			}
		}
	}
	codeStart := rs.text.Len()
	appendRow := func(cells []tableCell) {
		rs.text.WriteByte('|')
		for i := 0; i < colCount; i++ {
			rs.text.WriteByte(' ')
			cellText := ""
			if i < len(cells) {
				cellText = cells[i].text
			}
			rs.text.WriteString(cellText)
			pad := widths[i] - len(cellText)
			for p := 0; p < pad; p++ {
				rs.text.WriteByte(' ')
			}
			rs.text.WriteString(" |")
		}
		rs.text.WriteByte('\n')
	}
	appendRow(headers)
	// Divider
	rs.text.WriteByte('|')
	for i := 0; i < colCount; i++ {
		w := widths[i]
		if w < 3 {
			w = 3
		}
		rs.text.WriteByte(' ')
		for d := 0; d < w; d++ {
			rs.text.WriteByte('-')
		}
		rs.text.WriteString(" |")
	}
	rs.text.WriteByte('\n')
	for _, row := range rows {
		appendRow(row)
	}
	codeEnd := rs.text.Len()
	if codeEnd > codeStart {
		rs.styles = append(rs.styles, StyleSpan{Start: codeStart, End: codeEnd, Style: StyleCodeBlock})
	}
	if len(rs.listStack) == 0 {
		rs.text.WriteByte('\n')
	}
}

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

// ---------------------------------------------------------------------------
// Span utilities (mirrors core-rs/core/src/markdown/spans.rs)
// ---------------------------------------------------------------------------

func styleSortKey(s MarkdownStyle) int {
	switch s {
	case StyleBlockquote:
		return 0
	case StyleBold:
		return 1
	case StyleCode:
		return 2
	case StyleCodeBlock:
		return 3
	case StyleItalic:
		return 4
	case StyleSpoiler:
		return 5
	case StyleStrikethrough:
		return 6
	}
	return 7
}

func mergeStyleSpans(spans []StyleSpan) []StyleSpan {
	if len(spans) == 0 {
		return spans
	}
	sorted := make([]StyleSpan, len(spans))
	copy(sorted, spans)
	// Sort by (start, end, style sort key).
	for i := 1; i < len(sorted); i++ {
		key := sorted[i]
		j := i - 1
		for j >= 0 && spanLess(key, sorted[j]) {
			sorted[j+1] = sorted[j]
			j--
		}
		sorted[j+1] = key
	}
	merged := make([]StyleSpan, 0, len(sorted))
	for _, sp := range sorted {
		if len(merged) > 0 {
			prev := &merged[len(merged)-1]
			if prev.Style == sp.Style &&
				(sp.Start < prev.End || (sp.Start == prev.End && sp.Style != StyleBlockquote)) {
				if sp.End > prev.End {
					prev.End = sp.End
				}
				continue
			}
		}
		merged = append(merged, sp)
	}
	return merged
}

func spanLess(a, b StyleSpan) bool {
	if a.Start != b.Start {
		return a.Start < b.Start
	}
	if a.End != b.End {
		return a.End < b.End
	}
	return styleSortKey(a.Style) < styleSortKey(b.Style)
}

func clampStyleSpans(spans []StyleSpan, maxLen int) []StyleSpan {
	out := make([]StyleSpan, 0, len(spans))
	for _, sp := range spans {
		s := sp.Start
		if s > maxLen {
			s = maxLen
		}
		e := sp.End
		if e < s {
			e = s
		}
		if e > maxLen {
			e = maxLen
		}
		if e > s {
			out = append(out, StyleSpan{Start: s, End: e, Style: sp.Style})
		}
	}
	return out
}

func clampLinkSpans(spans []LinkSpan, maxLen int) []LinkSpan {
	out := make([]LinkSpan, 0, len(spans))
	for _, sp := range spans {
		s := sp.Start
		if s > maxLen {
			s = maxLen
		}
		e := sp.End
		if e < s {
			e = s
		}
		if e > maxLen {
			e = maxLen
		}
		if e > s {
			out = append(out, LinkSpan{Start: s, End: e, Href: sp.Href})
		}
	}
	return out
}
