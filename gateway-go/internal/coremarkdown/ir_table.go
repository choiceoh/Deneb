package coremarkdown

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Table rendering (mirrors core-rs/core/src/markdown/tables.rs)
// ---------------------------------------------------------------------------

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
