package coremarkdown

import "strings"

// parseMarkdown parses markdown into renderState without an external library.
// Optimized for well-formed LLM-generated markdown (not full CommonMark).
func parseMarkdown(input string, rs *renderState, opts *ParseOptions) {
	p := &mdParser{rs: rs, opts: opts}
	p.lines = strings.Split(input, "\n")
	p.parse()
}

type mdParser struct {
	rs    *renderState
	opts  *ParseOptions
	lines []string
	pos   int
}

// ---------------------------------------------------------------------------
// Block scanner
// ---------------------------------------------------------------------------

func (p *mdParser) parse() {
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]

		// Fenced code block
		if _, marker, _ := matchFenceLine(line); marker != "" {
			p.parseFencedCodeBlock()
			continue
		}

		// Thematic break (before list check: --- vs - item)
		if isThematicBreak(line) {
			p.rs.text.WriteString("───\n\n")
			p.pos++
			continue
		}

		// ATX heading
		if level, content := matchATXHeading(line); level > 0 {
			if p.rs.headingBold {
				p.rs.openStyle(StyleBold)
			}
			p.parseInline(content)
			if p.rs.headingBold {
				p.rs.closeStyle(StyleBold)
			}
			p.rs.appendParagraphSep()
			p.pos++
			continue
		}

		// Table (only when enabled, with separator look-ahead)
		if p.opts.TableMode != "off" && isTableRow(line) &&
			p.pos+1 < len(p.lines) && isTableSeparator(p.lines[p.pos+1]) {
			p.parseTable()
			continue
		}

		// Blockquote
		if content, ok := stripBlockquotePrefix(line); ok {
			p.parseBlockquote(content)
			continue
		}

		// List item
		if _, _, _, ok := classifyListItem(line); ok {
			p.parseList(countLeadingSpaces(line))
			continue
		}

		// Blank line
		if strings.TrimSpace(line) == "" {
			p.pos++
			continue
		}

		// Paragraph (default)
		p.parseParagraph()
	}
}

// ---------------------------------------------------------------------------
// Fenced code block
// ---------------------------------------------------------------------------

func (p *mdParser) parseFencedCodeBlock() {
	_, marker, _ := matchFenceLine(p.lines[p.pos])
	fenceChar := marker[0]
	fenceLen := len(marker)
	p.pos++

	var code strings.Builder
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		_, cm, rest := matchFenceLine(line)
		if cm != "" && cm[0] == fenceChar && len(cm) >= fenceLen && strings.TrimSpace(rest) == "" {
			p.pos++ // consume closing fence
			break
		}
		code.WriteString(line)
		code.WriteByte('\n')
		p.pos++
	}
	p.rs.renderCodeBlock(code.String())
}

// ---------------------------------------------------------------------------
// Paragraph
// ---------------------------------------------------------------------------

func (p *mdParser) parseParagraph() {
	first := true
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		if strings.TrimSpace(line) == "" {
			break
		}
		if p.isBlockStart(line) {
			break
		}
		if !first {
			p.rs.appendText("\n")
		}
		p.parseInline(line)
		first = false
		p.pos++
	}
	p.rs.appendParagraphSep()
}

// ---------------------------------------------------------------------------
// Blockquote
// ---------------------------------------------------------------------------

func (p *mdParser) parseBlockquote(firstContent string) {
	// Collect all continuation lines with > prefix stripped.
	var contentLines []string
	contentLines = append(contentLines, firstContent)
	p.pos++
	for p.pos < len(p.lines) {
		if c, ok := stripBlockquotePrefix(p.lines[p.pos]); ok {
			contentLines = append(contentLines, c)
			p.pos++
		} else {
			break
		}
	}

	if p.rs.blockquotePrefix != "" {
		p.rs.appendText(p.rs.blockquotePrefix)
	}
	p.rs.openStyle(StyleBlockquote)

	// Parse blockquote content as blocks (supports headings, code, lists inside quotes).
	inner := &mdParser{rs: p.rs, opts: p.opts, lines: contentLines}
	inner.parse()

	p.rs.closeStyle(StyleBlockquote)
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func (p *mdParser) parseList(baseIndent int) {
	_, ordered, _, _ := classifyListItem(p.lines[p.pos])

	if len(p.rs.listStack) > 0 {
		p.rs.text.WriteByte('\n')
	}

	entry := listEntry{ordered: ordered}
	if ordered {
		entry.index = extractOrderedStart(p.lines[p.pos]) - 1
	}
	p.rs.listStack = append(p.rs.listStack, entry)

	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		if strings.TrimSpace(line) == "" {
			break
		}

		indent, itemOrdered, content, ok := classifyListItem(line)
		if !ok {
			break
		}
		if indent < baseIndent {
			break
		}
		// Deeper indent → nested list
		if indent > baseIndent {
			p.parseList(indent)
			continue
		}
		// Different list type at same level → end this list
		if itemOrdered != ordered {
			break
		}

		// Same-level item
		p.rs.appendListPrefix()
		p.parseInline(content)
		p.pos++

		// Check for nested list on next line
		if p.pos < len(p.lines) {
			if ni, _, _, nok := classifyListItem(p.lines[p.pos]); nok && ni > baseIndent {
				p.parseList(ni)
			}
		}

		if t := p.rs.text.String(); !strings.HasSuffix(t, "\n") {
			p.rs.text.WriteByte('\n')
		}
	}

	p.rs.listStack = p.rs.listStack[:len(p.rs.listStack)-1]
	if len(p.rs.listStack) == 0 {
		p.rs.text.WriteByte('\n')
	}
}

// ---------------------------------------------------------------------------
// Table
// ---------------------------------------------------------------------------

func (p *mdParser) parseTable() {
	p.rs.table = &tableState{}
	p.rs.hasTables = true

	// Header row
	p.rs.table.inHeader = true
	p.parseTableRow(p.lines[p.pos])
	p.rs.table.headers = p.rs.table.currentRow
	p.rs.table.currentRow = nil
	p.rs.table.inHeader = false
	p.pos++

	// Separator row (skip)
	p.pos++

	// Body rows
	for p.pos < len(p.lines) {
		if !isTableRow(p.lines[p.pos]) {
			break
		}
		p.rs.table.currentRow = nil
		p.parseTableRow(p.lines[p.pos])
		p.rs.table.rows = append(p.rs.table.rows, p.rs.table.currentRow)
		p.rs.table.currentRow = nil
		p.pos++
	}

	switch p.rs.tableMode {
	case "bullets":
		p.rs.renderTableAsBullets()
	case "code":
		p.rs.renderTableAsCode()
	}
	p.rs.table = nil
}

func (p *mdParser) parseTableRow(line string) {
	cells := splitTableCells(line)
	for _, cell := range cells {
		p.rs.table.inCell = true
		p.rs.table.cellText.Reset()
		p.rs.table.cellStyles = nil
		p.rs.table.cellLinks = nil
		p.rs.table.cellOpen = nil
		p.rs.table.cellLink = nil
		p.parseInline(cell)
		p.rs.finishCell()
	}
}

// ---------------------------------------------------------------------------
// Inline parser
// ---------------------------------------------------------------------------

func (p *mdParser) parseInline(text string) {
	i := 0
	for i < len(text) {
		// Spoiler sentinels (from preprocessSpoilers)
		if p.opts.EnableSpoilers {
			if strings.HasPrefix(text[i:], sentinelOpen) {
				p.rs.openStyle(StyleSpoiler)
				i += len(sentinelOpen)
				continue
			}
			if strings.HasPrefix(text[i:], sentinelClose) {
				p.rs.closeStyle(StyleSpoiler)
				i += len(sentinelClose)
				continue
			}
		}

		ch := text[i]

		// Backtick: code span (highest priority — escapes everything inside)
		if ch == '`' {
			if content, end := p.scanCodeSpan(text, i); end > 0 {
				p.rs.renderInlineCode(content)
				i = end
				continue
			}
		}

		// Stars: bold / italic
		if ch == '*' {
			n := countRun(text, i, '*')
			if isEmphasisDelimiter(text, i, n) {
				p.handleEmphasis(n)
				i += n
				continue
			}
			// Literal asterisks (e.g. "3 * 4")
			p.rs.appendText(text[i : i+n])
			i += n
			continue
		}

		// Tilde: strikethrough
		if ch == '~' && i+1 < len(text) && text[i+1] == '~' {
			if p.hasOpenStyle(StyleStrikethrough) {
				p.rs.closeStyle(StyleStrikethrough)
			} else {
				p.rs.openStyle(StyleStrikethrough)
			}
			i += 2
			continue
		}

		// Image: ![alt](url) — emit alt text only
		if ch == '!' && i+1 < len(text) && text[i+1] == '[' {
			if alt, _, end := parseLinkBrackets(text, i+1); end > 0 {
				p.parseInline(alt)
				i = end
				continue
			}
		}

		// Link: [text](url)
		if ch == '[' {
			if label, href, end := parseLinkBrackets(text, i); end > 0 {
				p.rs.handleLinkOpen(href)
				p.parseInline(label)
				p.rs.handleLinkClose()
				i = end
				continue
			}
		}

		// AutoLink: <url>
		if ch == '<' {
			if url, end := scanAutoLink(text, i); end > 0 {
				p.rs.handleLinkOpen(url)
				p.rs.appendText(url)
				p.rs.handleLinkClose()
				i = end
				continue
			}
		}

		// Regular text: batch consecutive non-special characters.
		start := i
		for i < len(text) {
			c := text[i]
			if c == '`' || c == '*' || c == '~' || c == '!' || c == '[' || c == '<' {
				break
			}
			// Sentinel sequences start with 0xE2 (first byte of U+200B).
			if p.opts.EnableSpoilers && c == sentinelOpen[0] {
				break
			}
			i++
		}
		if i > start {
			p.rs.appendText(text[start:i])
		} else {
			// Fallback: single character that didn't match any rule above.
			p.rs.appendText(text[i : i+1])
			i++
		}
	}
}

// handleEmphasis processes a run of n asterisks using close-first strategy.
func (p *mdParser) handleEmphasis(n int) {
	remaining := n

	// *** with both open → close both
	if remaining >= 3 && p.hasOpenStyle(StyleBold) && p.hasOpenStyle(StyleItalic) {
		p.rs.closeStyle(StyleItalic)
		p.rs.closeStyle(StyleBold)
		remaining -= 3
	}

	// ** → toggle bold
	if remaining >= 2 {
		if p.hasOpenStyle(StyleBold) {
			p.rs.closeStyle(StyleBold)
		} else {
			p.rs.openStyle(StyleBold)
		}
		remaining -= 2
	}

	// * → toggle italic
	if remaining >= 1 {
		if p.hasOpenStyle(StyleItalic) {
			p.rs.closeStyle(StyleItalic)
		} else {
			p.rs.openStyle(StyleItalic)
		}
		remaining--
	}

	// 4+ stars: emit remaining as literal text
	for remaining > 0 {
		p.rs.appendText("*")
		remaining--
	}
}

func (p *mdParser) hasOpenStyle(style MarkdownStyle) bool {
	s := p.rs.openStylesSlice()
	for _, o := range *s {
		if o.style == style {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Inline helpers
// ---------------------------------------------------------------------------

// scanCodeSpan finds a matching closing backtick sequence starting at pos.
// Returns (content, endPos) or ("", -1).
func (p *mdParser) scanCodeSpan(text string, pos int) (string, int) {
	ticks := countRun(text, pos, '`')
	start := pos + ticks
	for j := start; j <= len(text)-ticks; j++ {
		if text[j] != '`' {
			continue
		}
		run := countRun(text, j, '`')
		if run == ticks {
			content := text[start:j]
			// Strip one leading+trailing space when both present (CommonMark rule).
			if len(content) >= 2 && content[0] == ' ' && content[len(content)-1] == ' ' {
				inner := content[1 : len(content)-1]
				if len(inner) > 0 {
					content = inner
				}
			}
			return content, j + ticks
		}
		j += run - 1 // skip past non-matching backtick run
	}
	return "", -1
}

// parseLinkBrackets parses [label](href) starting at pos (which should be '[').
// Returns (label, href, endPos) or ("","", -1).
func parseLinkBrackets(text string, pos int) (string, string, int) {
	if pos >= len(text) || text[pos] != '[' {
		return "", "", -1
	}
	// Find matching ]
	depth := 0
	labelEnd := -1
	for i := pos; i < len(text); i++ {
		switch text[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				labelEnd = i
			}
		}
		if labelEnd >= 0 {
			break
		}
	}
	if labelEnd < 0 || labelEnd+1 >= len(text) || text[labelEnd+1] != '(' {
		return "", "", -1
	}
	// Find matching )
	parenStart := labelEnd + 2
	parenEnd := -1
	parenDepth := 1
	for i := parenStart; i < len(text); i++ {
		switch text[i] {
		case '(':
			parenDepth++
		case ')':
			parenDepth--
			if parenDepth == 0 {
				parenEnd = i
			}
		}
		if parenEnd >= 0 {
			break
		}
	}
	if parenEnd < 0 {
		return "", "", -1
	}
	return text[pos+1 : labelEnd], text[parenStart:parenEnd], parenEnd + 1
}

// scanAutoLink parses <url> where content looks like a URL or email.
func scanAutoLink(text string, pos int) (string, int) {
	if pos >= len(text) || text[pos] != '<' {
		return "", -1
	}
	end := strings.IndexByte(text[pos+1:], '>')
	if end < 0 {
		return "", -1
	}
	content := text[pos+1 : pos+1+end]
	if strings.Contains(content, "://") || strings.Contains(content, "@") {
		return content, pos + 1 + end + 1
	}
	return "", -1
}

// ---------------------------------------------------------------------------
// Block-level matchers
// ---------------------------------------------------------------------------

func matchATXHeading(line string) (level int, content string) {
	if len(line) == 0 || line[0] != '#' {
		return 0, ""
	}
	i := 0
	for i < len(line) && i < 6 && line[i] == '#' {
		i++
	}
	if i >= len(line) {
		return i, "" // bare "###"
	}
	if line[i] != ' ' {
		return 0, "" // "#text" without space
	}
	return i, line[i+1:]
}

func isThematicBreak(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 {
		return false
	}
	ch := trimmed[0]
	if ch != '-' && ch != '*' && ch != '_' {
		return false
	}
	count := 0
	for _, r := range trimmed {
		if r == rune(ch) {
			count++
		} else if r != ' ' {
			return false
		}
	}
	return count >= 3
}

func stripBlockquotePrefix(line string) (string, bool) {
	if len(line) >= 2 && line[0] == '>' && line[1] == ' ' {
		return line[2:], true
	}
	if len(line) >= 1 && line[0] == '>' {
		return line[1:], true
	}
	return "", false
}

// classifyListItem returns (indent, ordered, content, ok).
func classifyListItem(line string) (int, bool, string, bool) {
	indent := countLeadingSpaces(line)
	if indent >= len(line) {
		return 0, false, "", false
	}
	rest := line[indent:]

	// Unordered: -, *, +  followed by space
	if len(rest) >= 2 && (rest[0] == '-' || rest[0] == '*' || rest[0] == '+') && rest[1] == ' ' {
		return indent, false, rest[2:], true
	}

	// Ordered: digits followed by ". "
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(rest) && rest[i] == '.' && rest[i+1] == ' ' {
		return indent, true, rest[i+2:], true
	}
	return 0, false, "", false
}

func extractOrderedStart(line string) int {
	indent := countLeadingSpaces(line)
	rest := line[indent:]
	num := 0
	for _, ch := range rest {
		if ch >= '0' && ch <= '9' {
			num = num*10 + int(ch-'0')
		} else {
			break
		}
	}
	if num == 0 {
		return 1
	}
	return num
}

func isTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	return len(trimmed) > 0 && trimmed[0] == '|'
}

func isTableSeparator(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") {
		return false
	}
	// Must contain at least one "---" between pipes.
	for _, ch := range trimmed {
		if ch != '|' && ch != '-' && ch != ':' && ch != ' ' {
			return false
		}
	}
	return strings.Contains(trimmed, "---")
}

func splitTableCells(line string) []string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "|") {
		trimmed = trimmed[1:]
	}
	if strings.HasSuffix(trimmed, "|") {
		trimmed = trimmed[:len(trimmed)-1]
	}

	// Split by | but respect backtick code spans (pipes inside `` are literal).
	var cells []string
	start := 0
	inCode := false
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] == '`' {
			inCode = !inCode
		} else if trimmed[i] == '|' && !inCode {
			cells = append(cells, trimmed[start:i])
			start = i + 1
		}
	}
	cells = append(cells, trimmed[start:])
	return cells
}

// ---------------------------------------------------------------------------
// isBlockStart — determines whether a line would start a new block.
// Used by parseParagraph to stop collecting continuation lines.
// ---------------------------------------------------------------------------

func (p *mdParser) isBlockStart(line string) bool {
	if _, marker, _ := matchFenceLine(line); marker != "" {
		return true
	}
	if isThematicBreak(line) {
		return true
	}
	if level, _ := matchATXHeading(line); level > 0 {
		return true
	}
	if _, ok := stripBlockquotePrefix(line); ok {
		return true
	}
	if _, _, _, ok := classifyListItem(line); ok {
		return true
	}
	if p.opts.TableMode != "off" && isTableRow(line) {
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

func countLeadingSpaces(s string) int {
	i := 0
	for i < len(s) && s[i] == ' ' {
		i++
	}
	return i
}

func countRun(s string, pos int, ch byte) int {
	n := 0
	for pos+n < len(s) && s[pos+n] == ch {
		n++
	}
	return n
}

// isEmphasisDelimiter returns true when a run of * is adjacent to non-space
// on at least one side (i.e., not a standalone " * " arithmetic operator).
func isEmphasisDelimiter(text string, pos, count int) bool {
	end := pos + count
	leftSpace := pos == 0 || text[pos-1] == ' ' || text[pos-1] == '\t' || text[pos-1] == '\n'
	rightSpace := end >= len(text) || text[end] == ' ' || text[end] == '\t' || text[end] == '\n'
	return !(leftSpace && rightSpace)
}
