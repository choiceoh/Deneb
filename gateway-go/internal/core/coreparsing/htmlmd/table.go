package htmlmd

import "strings"

// tableBuilder accumulates table rows and renders them as a Markdown table.
type tableBuilder struct {
	rows         []tableRow // completed rows
	currentCells []string   // cells for the row being built
	currentHasTH bool       // whether current row has any <th>
	inCell       bool
	cellBuf      strings.Builder
	cellIsTH     bool
}

type tableRow struct {
	cells    []string
	isHeader bool
}

func (tb *tableBuilder) startRow() {
	tb.currentCells = tb.currentCells[:0]
	tb.currentHasTH = false
}

func (tb *tableBuilder) endRow() {
	if len(tb.currentCells) > 0 {
		cells := make([]string, len(tb.currentCells))
		copy(cells, tb.currentCells)
		tb.rows = append(tb.rows, tableRow{cells: cells, isHeader: tb.currentHasTH})
	}
	tb.currentCells = tb.currentCells[:0]
}

func (tb *tableBuilder) startCell(isTH bool) {
	tb.inCell = true
	tb.cellBuf.Reset()
	tb.cellIsTH = isTH
	if isTH {
		tb.currentHasTH = true
	}
}

func (tb *tableBuilder) endCell() {
	if !tb.inCell {
		return
	}
	text := escapeTableCell(strings.TrimSpace(tb.cellBuf.String()))
	tb.currentCells = append(tb.currentCells, text)
	tb.cellBuf.Reset()
	tb.inCell = false
}

func (tb *tableBuilder) pushText(s string) {
	if tb.inCell {
		tb.cellBuf.WriteString(s)
	}
}

func (tb *tableBuilder) pushChar(ch rune) {
	if tb.inCell {
		tb.cellBuf.WriteRune(ch)
	}
}

// toMarkdown renders the accumulated rows as a Markdown table.
func (tb *tableBuilder) toMarkdown() string {
	if len(tb.rows) == 0 {
		return ""
	}

	var b strings.Builder
	sepAdded := false

	for i, row := range tb.rows {
		b.WriteString("| ")
		b.WriteString(strings.Join(row.cells, " | "))
		b.WriteString(" |\n")

		if !sepAdded && (row.isHeader || i == 0) {
			b.WriteByte('|')
			for range row.cells {
				b.WriteString(" --- |")
			}
			b.WriteByte('\n')
			sepAdded = true
		}
	}

	return b.String()
}

// escapeTableCell escapes pipes and backslashes inside a table cell
// so the Markdown table structure is preserved.
func escapeTableCell(text string) string {
	var b strings.Builder
	b.Grow(len(text) + 4)
	for _, ch := range text {
		if ch == '|' || ch == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(ch)
	}
	return b.String()
}
