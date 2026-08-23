package document

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxDocumentOutputBytes   = 8 << 20
	maxSubprocessStderrBytes = 64 << 10

	maxOOXMLRelevantParts = 256
	maxOOXMLPartBytes     = 16 << 20
	maxOOXMLTotalBytes    = 64 << 20
	maxOOXMLOutputBytes   = maxDocumentOutputBytes
	maxOOXMLTableRows     = 500
	maxOOXMLTableColumns  = 1024

	maxPDFTextOutputBytes = maxDocumentOutputBytes
	maxPDFTextStderrBytes = maxSubprocessStderrBytes

	maxPDFRasterFiles      = ocrPageCap
	maxPDFRasterPageBytes  = maxDocumentOutputBytes
	maxPDFRasterTotalBytes = 64 << 20
	pdfRasterScalePixels   = 3000
)

var errDocumentExtractionLimit = errors.New("document extraction limit exceeded")

// limitedBuffer stores at most limit bytes. Strict buffers return a sentinel
// error when the limit is crossed; truncating buffers keep draining their input
// while retaining only the bounded prefix (used for subprocess stderr).
type limitedBuffer struct {
	buf        bytes.Buffer
	limit      int
	exceeded   bool
	truncating bool
}

func newLimitedBuffer(limit int, truncating bool) *limitedBuffer {
	return &limitedBuffer{limit: limit, truncating: truncating}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.buf.Len()
	if remaining < 0 {
		remaining = 0
	}
	keep := len(p)
	if keep > remaining {
		keep = remaining
		b.exceeded = true
	}
	if keep > 0 {
		_, _ = b.buf.Write(p[:keep])
	}
	if b.exceeded && !b.truncating {
		return keep, errDocumentExtractionLimit
	}
	return len(p), nil
}

func (b *limitedBuffer) WriteString(s string) (int, error) {
	remaining := b.limit - b.buf.Len()
	if remaining < 0 {
		remaining = 0
	}
	keep := len(s)
	if keep > remaining {
		keep = remaining
		b.exceeded = true
	}
	if keep > 0 {
		_, _ = b.buf.WriteString(s[:keep])
	}
	if b.exceeded && !b.truncating {
		return keep, errDocumentExtractionLimit
	}
	return len(s), nil
}

func (b *limitedBuffer) WriteByte(c byte) error {
	if b.buf.Len() >= b.limit {
		b.exceeded = true
		if !b.truncating {
			return errDocumentExtractionLimit
		}
		return nil
	}
	return b.buf.WriteByte(c)
}

func (b *limitedBuffer) Len() int       { return b.buf.Len() }
func (b *limitedBuffer) String() string { return b.buf.String() }
func (b *limitedBuffer) Remaining() int { return b.limit - b.buf.Len() }

func (b *limitedBuffer) Reset() {
	b.buf.Reset()
	b.exceeded = false
}

// docparse is the shared, local document-parser library: per-format extractors
// (PDF, Excel, Word, PowerPoint, images via OCR) plus the markdown-table and
// format-detection helpers they share. It has no mail/files/web coupling —
// those callers route raw bytes through the dispatchers in document_extract.go,
// which in turn call these parsers. Keeping the library here lets mail
// attachment, files, and fetch callers stay focused on their own orchestration.
//
// Everything here uses only the standard library (OOXML is a zip of XML parts)
// and the local `pdftotext`/`pdftoppm`/`tesseract` CLIs, so extraction is fully
// self-contained — no external service or `lit` CLI required.

// --- format detection ---

// isTextFile reports whether a filename has a plain-text extension.
func isTextFile(lowerName string) bool {
	for _, ext := range []string{".txt", ".csv", ".md", ".json", ".xml", ".log", ".yaml", ".yml"} {
		if strings.HasSuffix(lowerName, ext) {
			return true
		}
	}
	return false
}

// hasImageExt reports whether a filename has a known raster image extension.
func hasImageExt(lowerName string) bool {
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff"} {
		if strings.HasSuffix(lowerName, ext) {
			return true
		}
	}
	return false
}

// --- PDF text extraction ---

type boundedCommandRunner func(context.Context, io.Reader, io.Writer, io.Writer) error

// pdfToText extracts text from PDF bytes via the `pdftotext` CLI (poppler).
// The PDF is piped through stdin so no temp file is needed.
func pdfToText(ctx context.Context, pdf []byte) (string, error) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return "", fmt.Errorf("pdftotext 미설치 — DGX Spark에서 `apt install poppler-utils` 실행 필요")
	}
	return runPDFToText(ctx, pdf, func(runCtx context.Context, stdin io.Reader, stdout, stderr io.Writer) error {
		// `pdftotext -layout - -` reads the PDF from stdin, writes text to stdout.
		cmd := exec.CommandContext(runCtx, "pdftotext", "-layout", "-", "-")
		cmd.Stdin = stdin
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		return cmd.Run()
	})
}

func runPDFToText(ctx context.Context, pdf []byte, run boundedCommandRunner) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	out := newLimitedBuffer(maxPDFTextOutputBytes, false)
	errBuf := newLimitedBuffer(maxPDFTextStderrBytes, true)
	if err := run(runCtx, bytes.NewReader(pdf), out, errBuf); err != nil {
		if ctxErr := runCtx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		if out.exceeded {
			return "", fmt.Errorf("%w: pdftotext stdout exceeds %d bytes", errDocumentExtractionLimit, maxPDFTextOutputBytes)
		}
		if msg := strings.TrimSpace(errBuf.String()); msg != "" {
			return "", fmt.Errorf("%s", firstLine(msg))
		}
		return "", err
	}
	if ctxErr := runCtx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	if out.exceeded {
		return "", fmt.Errorf("%w: pdftotext stdout exceeds %d bytes", errDocumentExtractionLimit, maxPDFTextOutputBytes)
	}

	text := out.String()
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("추출된 텍스트가 없습니다 (스캔본 PDF일 수 있음)")
	}
	// Returned untrimmed: form feeds are whitespace, so trimming here would
	// swallow the separators of empty leading/trailing pages and shift every
	// page index in pdfToTextStructured off by one.
	return text, nil
}

type ooxmlPartSet struct {
	files     []*zip.File
	remaining int64
}

// collectOOXMLParts validates central-directory sizes before any relevant XML
// part is opened. The runtime reader below repeats the byte checks while
// decompressing so corrupt size metadata cannot bypass the preflight.
func collectOOXMLParts(zr *zip.Reader, relevant func(string) bool) (*ooxmlPartSet, error) {
	files := make([]*zip.File, 0)
	seen := make(map[string]struct{})
	var total uint64
	for _, f := range zr.File {
		if !relevant(f.Name) {
			continue
		}
		if len(files) >= maxOOXMLRelevantParts {
			return nil, fmt.Errorf("%w: OOXML has more than %d relevant parts", errDocumentExtractionLimit, maxOOXMLRelevantParts)
		}
		if _, duplicate := seen[f.Name]; duplicate {
			return nil, fmt.Errorf("malformed OOXML: duplicate part %q", f.Name)
		}
		seen[f.Name] = struct{}{}
		if f.UncompressedSize64 > maxOOXMLPartBytes {
			return nil, fmt.Errorf("%w: OOXML part %q exceeds %d uncompressed bytes", errDocumentExtractionLimit, f.Name, maxOOXMLPartBytes)
		}
		if f.UncompressedSize64 > maxOOXMLTotalBytes-total {
			return nil, fmt.Errorf("%w: OOXML relevant parts exceed %d uncompressed bytes", errDocumentExtractionLimit, maxOOXMLTotalBytes)
		}
		total += f.UncompressedSize64
		files = append(files, f)
	}
	return &ooxmlPartSet{files: files, remaining: maxOOXMLTotalBytes}, nil
}

func (parts *ooxmlPartSet) read(f *zip.File, consume func(io.Reader) error) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	limit := int64(maxOOXMLPartBytes)
	if parts.remaining < limit {
		limit = parts.remaining
	}
	lr := &io.LimitedReader{R: rc, N: limit + 1}
	if err := consume(lr); err != nil {
		return err
	}
	if _, err := io.Copy(io.Discard, lr); err != nil {
		return err
	}
	consumed := limit + 1 - lr.N
	if consumed > limit {
		return fmt.Errorf("%w: OOXML part %q exceeded its runtime byte budget", errDocumentExtractionLimit, f.Name)
	}
	parts.remaining -= consumed
	return nil
}

func (parts *ooxmlPartSet) unmarshalXML(f *zip.File, v any) error {
	return parts.read(f, func(r io.Reader) error {
		decoder := xml.NewDecoder(r)
		if err := decoder.Decode(v); err != nil {
			return err
		}
		for {
			if _, err := decoder.Token(); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
		}
	})
}

// --- Excel (.xlsx) extraction ---
//
// An .xlsx file is a zip of XML parts. The cell strings live in a shared
// table (xl/sharedStrings.xml) and each sheet (xl/worksheets/sheetN.xml)
// references them by index. This reader uses only the standard library.

type xlsxSST struct {
	Items []xlsxSI `xml:"si"`
}

type xlsxSI struct {
	T    string   `xml:"t"`   // plain string
	Runs []string `xml:"r>t"` // rich-text runs
}

func (si xlsxSI) text() string {
	if len(si.Runs) > 0 {
		return strings.Join(si.Runs, "")
	}
	return si.T
}

type xlsxSheet struct {
	Rows []xlsxRow `xml:"sheetData>row"`
}

type xlsxRow struct {
	Cells []xlsxCell `xml:"c"`
}

type xlsxCell struct {
	Ref      string `xml:"r,attr"`
	Type     string `xml:"t,attr"`
	V        string `xml:"v"`
	InlineST string `xml:"is>t"`
}

// xlsxToText extracts the cell contents of every sheet in an .xlsx workbook.
func xlsxToText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("xlsx 압축 해제 실패: %w", err)
	}

	parts, err := collectOOXMLParts(zr, func(name string) bool {
		return name == "xl/sharedStrings.xml" ||
			(strings.HasPrefix(name, "xl/worksheets/sheet") && strings.HasSuffix(name, ".xml"))
	})
	if err != nil {
		return "", fmt.Errorf("xlsx 읽기 실패: %w", err)
	}

	shared, err := readXLSXSharedStrings(parts)
	if err != nil {
		return "", fmt.Errorf("xlsx 공유 문자열 읽기 실패: %w", err)
	}

	var sheetFiles []*zip.File
	for _, f := range parts.files {
		if f.Name != "xl/sharedStrings.xml" {
			sheetFiles = append(sheetFiles, f)
		}
	}
	if len(sheetFiles) == 0 {
		return "", fmt.Errorf("워크시트를 찾을 수 없습니다")
	}
	sort.Slice(sheetFiles, func(i, j int) bool {
		return ooxmlPartLess(sheetFiles[i].Name, sheetFiles[j].Name, "xl/worksheets/sheet")
	})

	const (
		maxRowsPerSheet = maxOOXMLTableRows
		// maxExcelCols is Excel's hard column ceiling (XFD). A cell ref beyond it
		// is malformed or crafted; capping here stops a single bad ref from
		// driving the column-padding loop into an unbounded allocation — a DoS
		// vector, since .xlsx bytes are untrusted attachment input.
		maxExcelCols = 16384
	)
	sb := newLimitedBuffer(maxOOXMLOutputBytes, false)
	for idx, f := range sheetFiles {
		var sheet xlsxSheet
		if err := parts.unmarshalXML(f, &sheet); err != nil {
			return "", fmt.Errorf("빈 워크북: 워크시트 %q 읽기 실패: %w", f.Name, err)
		}
		rows := sheet.Rows
		truncated := false
		if len(rows) > maxRowsPerSheet {
			rows = rows[:maxRowsPerSheet]
			truncated = true
		}
		// Place each cell in its true column (parsed from the A1-style ref like
		// "B2") so sparse rows — where empty leading/middle cells are dropped
		// from the XML — stay aligned. Without this a markdown table shifts
		// columns row-to-row.
		var grid [][]string
		for _, row := range rows {
			var cells []string
			for _, c := range row.Cells {
				col := colIndexFromRef(c.Ref)
				if col < 0 {
					col = len(cells) // no usable ref → next slot
				}
				if col >= maxExcelCols {
					continue // reject malformed/oversized refs before padding
				}
				if col >= maxOOXMLTableColumns {
					return "", fmt.Errorf("%w: worksheet %q exceeds %d rendered columns", errDocumentExtractionLimit, f.Name, maxOOXMLTableColumns)
				}
				for len(cells) <= col {
					cells = append(cells, "")
				}
				cells[col] = xlsxCellValue(c, shared)
			}
			if strings.TrimSpace(strings.Join(cells, "")) == "" {
				continue // skip fully empty rows
			}
			grid = append(grid, cells)
		}
		table, err := mdTableWithLimit(grid, sb.Remaining())
		if err != nil {
			return "", fmt.Errorf("%w: worksheet %q output exceeds %d bytes", errDocumentExtractionLimit, f.Name, maxOOXMLOutputBytes)
		}
		if table == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(sb, "### Sheet %d\n\n", idx+1)
		sb.WriteString(table)
		sb.WriteString("\n")
		if truncated {
			fmt.Fprintf(sb, "... (%d행 이하 생략)\n", len(sheet.Rows)-maxRowsPerSheet)
		}
		if sb.exceeded {
			return "", fmt.Errorf("%w: xlsx output exceeds %d bytes", errDocumentExtractionLimit, maxOOXMLOutputBytes)
		}
	}

	out := strings.TrimSpace(sb.String())
	if out == "" {
		return "", fmt.Errorf("빈 워크북")
	}
	return out, nil
}

func readXLSXSharedStrings(parts *ooxmlPartSet) ([]string, error) {
	for _, f := range parts.files {
		if f.Name != "xl/sharedStrings.xml" {
			continue
		}
		var sst xlsxSST
		if err := parts.unmarshalXML(f, &sst); err != nil {
			return nil, err
		}
		out := make([]string, len(sst.Items))
		for i, si := range sst.Items {
			out[i] = si.text()
		}
		return out, nil
	}
	return nil, nil
}

func xlsxCellValue(c xlsxCell, shared []string) string {
	switch c.Type {
	case "s": // shared string: V is an index into the shared table
		if idx, err := strconv.Atoi(strings.TrimSpace(c.V)); err == nil && idx >= 0 && idx < len(shared) {
			return shared[idx]
		}
		return ""
	case "inlineStr":
		return c.InlineST
	default: // number, boolean, or formula result
		return c.V
	}
}

// --- Word (.docx) and PowerPoint (.pptx) extraction ---
//
// Both formats are Office Open XML — a zip of XML parts. Body text lives in
// `<w:t>` (docx) / `<a:t>` (pptx) elements grouped by `<w:p>` / `<a:p>`
// paragraphs. Go's xml decoder matches local names regardless of namespace
// prefix, so a single streaming extractor (`extractOOXMLText`) handles both.

// extractOOXMLText streams an Office Open XML part and returns its text. Plain
// paragraphs (<p>) are separated by newlines; tables (<tbl>/<tr>/<tc>) are
// rendered as markdown so column structure survives instead of collapsing into
// a vertical list of cell values. The same local names cover Word
// (w:tbl/w:tr/w:tc) and PowerPoint (a:tbl/a:tr/a:tc), so one extractor does both.
func extractOOXMLText(r io.Reader) string {
	text, _ := extractOOXMLTextWithLimit(r, maxOOXMLOutputBytes)
	return text
}

func extractOOXMLTextWithLimit(r io.Reader, maxOutput int) (string, error) {
	decoder := xml.NewDecoder(r)
	extractor := newOOXMLTextExtractor(maxOutput)
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return extractor.output.String(), nil
		}
		if err != nil {
			return extractor.output.String(), fmt.Errorf("malformed OOXML XML: %w", err)
		}
		extractor.consume(token)
		if extractor.err != nil {
			return "", extractor.err
		}
	}
}

// ooxmlTextExtractor owns the streaming OOXML state machine. Table depth and
// cell state stay together so nested tables inline into their parent cell while
// only the outer table emits markdown.
type ooxmlTextExtractor struct {
	output        *limitedBuffer
	inText        bool
	paragraphOpen bool
	tableDepth    int
	rows          [][]string
	currentRow    []string
	currentCell   *limitedBuffer
	cellOpen      bool
	err           error
}

func newOOXMLTextExtractor(maxOutput int) *ooxmlTextExtractor {
	return &ooxmlTextExtractor{
		output:      newLimitedBuffer(maxOutput, false),
		currentCell: newLimitedBuffer(maxOutput, false),
	}
}

func (e *ooxmlTextExtractor) consume(token xml.Token) {
	if e.err != nil {
		return
	}
	switch value := token.(type) {
	case xml.StartElement:
		e.startElement(value.Name.Local)
	case xml.EndElement:
		e.endElement(value.Name.Local)
	case xml.CharData:
		e.appendText(value)
	}
}

func (e *ooxmlTextExtractor) startElement(name string) {
	switch name {
	case "tbl":
		e.startTable()
	case "tr":
		if e.tableDepth == 1 {
			e.currentRow = nil
		}
	case "tc":
		e.startCell()
	case "p":
		e.startParagraph()
	case "t":
		e.inText = true
	}
}

func (e *ooxmlTextExtractor) endElement(name string) {
	switch name {
	case "tbl":
		e.endTable()
	case "tr":
		e.endRow()
	case "tc":
		e.endCell()
	case "p":
		e.endParagraph()
	case "t":
		e.inText = false
	}
}

func (e *ooxmlTextExtractor) startTable() {
	e.tableDepth++
	if e.tableDepth == 1 {
		e.rows = nil
	}
}

func (e *ooxmlTextExtractor) endTable() {
	if e.tableDepth == 1 {
		e.renderTable()
		e.rows = nil
	}
	if e.tableDepth > 0 {
		e.tableDepth--
	}
}

func (e *ooxmlTextExtractor) renderTable() {
	table, err := mdTableWithLimit(e.rows, e.output.Remaining())
	if err != nil {
		e.err = fmt.Errorf("%w: OOXML table output exceeds %d bytes", errDocumentExtractionLimit, e.output.limit)
		return
	}
	if table == "" {
		return
	}
	if e.output.Len() > 0 {
		if _, err := e.output.WriteString("\n"); err != nil {
			e.err = err
			return
		}
	}
	if _, err := e.output.WriteString(table); err != nil {
		e.err = err
		return
	}
	if _, err := e.output.WriteString("\n"); err != nil {
		e.err = err
	}
}

func (e *ooxmlTextExtractor) endRow() {
	if e.tableDepth != 1 {
		return
	}
	if len(e.rows) >= maxOOXMLTableRows {
		e.err = fmt.Errorf("%w: OOXML table exceeds %d rows", errDocumentExtractionLimit, maxOOXMLTableRows)
		return
	}
	e.rows = append(e.rows, e.currentRow)
	e.currentRow = nil
}

func (e *ooxmlTextExtractor) startCell() {
	if e.tableDepth != 1 {
		return
	}
	e.cellOpen = true
	e.currentCell.Reset()
}

func (e *ooxmlTextExtractor) endCell() {
	if e.tableDepth != 1 {
		return
	}
	if e.currentCell.exceeded {
		e.err = fmt.Errorf("%w: OOXML table cell exceeds %d bytes", errDocumentExtractionLimit, e.currentCell.limit)
		return
	}
	if len(e.currentRow) >= maxOOXMLTableColumns {
		e.err = fmt.Errorf("%w: OOXML table exceeds %d columns", errDocumentExtractionLimit, maxOOXMLTableColumns)
		return
	}
	e.currentRow = append(e.currentRow, strings.TrimSpace(e.currentCell.String()))
	e.currentCell.Reset()
	e.cellOpen = false
}

func (e *ooxmlTextExtractor) startParagraph() {
	if e.cellOpen {
		if e.currentCell.Len() > 0 {
			if err := e.currentCell.WriteByte(' '); err != nil {
				e.err = err
			}
		}
		return
	}
	e.paragraphOpen = true
}

func (e *ooxmlTextExtractor) endParagraph() {
	if e.cellOpen || !e.paragraphOpen {
		return
	}
	if _, err := e.output.WriteString("\n"); err != nil {
		e.err = err
		return
	}
	e.paragraphOpen = false
}

func (e *ooxmlTextExtractor) appendText(text xml.CharData) {
	if !e.inText {
		return
	}
	if e.cellOpen {
		if _, err := e.currentCell.Write(text); err != nil {
			e.err = err
		}
		return
	}
	if _, err := e.output.Write(text); err != nil {
		e.err = err
	}
}

// docxToText extracts body text from a .docx file (word/document.xml).
func docxToText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("docx 압축 해제 실패: %w", err)
	}
	parts, err := collectOOXMLParts(zr, func(name string) bool { return name == "word/document.xml" })
	if err != nil {
		return "", fmt.Errorf("docx 읽기 실패: %w", err)
	}
	if len(parts.files) == 0 {
		return "", fmt.Errorf("word/document.xml을 찾을 수 없습니다")
	}
	var text string
	if err := parts.read(parts.files[0], func(r io.Reader) error {
		var extractErr error
		text, extractErr = extractOOXMLTextWithLimit(r, maxOOXMLOutputBytes)
		return extractErr
	}); err != nil {
		return "", fmt.Errorf("docx 본문 읽기 실패: %w", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("빈 문서")
	}
	return text, nil
}

// pptxToText extracts text from every slide of a .pptx file (ppt/slides/slideN.xml).
func pptxToText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("pptx 압축 해제 실패: %w", err)
	}

	parts, err := collectOOXMLParts(zr, func(name string) bool {
		return strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml")
	})
	if err != nil {
		return "", fmt.Errorf("pptx 읽기 실패: %w", err)
	}
	slideFiles := parts.files
	if len(slideFiles) == 0 {
		return "", fmt.Errorf("슬라이드를 찾을 수 없습니다")
	}
	sort.Slice(slideFiles, func(i, j int) bool {
		return ooxmlPartLess(slideFiles[i].Name, slideFiles[j].Name, "ppt/slides/slide")
	})

	sb := newLimitedBuffer(maxOOXMLOutputBytes, false)
	for i, f := range slideFiles {
		var slideText string
		if err := parts.read(f, func(r io.Reader) error {
			var extractErr error
			slideText, extractErr = extractOOXMLTextWithLimit(r, sb.Remaining())
			return extractErr
		}); err != nil {
			return "", fmt.Errorf("pptx 슬라이드 %q 읽기 실패: %w", f.Name, err)
		}
		slideText = strings.TrimSpace(slideText)
		if slideText == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(sb, "### Slide %d\n\n%s\n", i+1, slideText)
		if sb.exceeded {
			return "", fmt.Errorf("%w: pptx output exceeds %d bytes", errDocumentExtractionLimit, maxOOXMLOutputBytes)
		}
	}

	out := strings.TrimSpace(sb.String())
	if out == "" {
		return "", fmt.Errorf("빈 프레젠테이션")
	}
	return out, nil
}

// ooxmlPartLess orders numbered OOXML parts naturally. Lexicographic order puts
// sheet10/slide10 before sheet2/slide2, silently scrambling a workbook or deck
// once it grows past nine parts.
func ooxmlPartLess(a, b, prefix string) bool {
	partNumber := func(name string) (int, bool) {
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".xml") {
			return 0, false
		}
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".xml"))
		return n, err == nil
	}
	an, aok := partNumber(a)
	bn, bok := partNumber(b)
	if aok && bok && an != bn {
		return an < bn
	}
	if aok != bok {
		return aok
	}
	return a < b
}

// --- OCR (scanned PDFs and image attachments) ---

// ocrLangs is the tesseract language set: Korean + English, matching the
// project's Korean-first business documents.
const ocrLangs = "kor+eng"

// imageOCR recognizes text in raw image bytes via PaddleOCR-VL, falling back
// to tesseract when the local model server is unavailable.
func imageOCR(ctx context.Context, img []byte) (string, error) {
	if err := validateOCRImage(img); err != nil {
		return "", err
	}
	return ocrImageBytes(ctx, img)
}

// ocrPageCap bounds how many pages of a PDF we rasterize+OCR — enough for
// long scanned contracts and image-heavy decks without letting a huge PDF
// monopolize the GPU. Raised from 10 once page OCR became batched
// (ocrPageConcurrency): the vLLM sidecar amortizes concurrent pages, so 30
// pages cost wall-clock comparable to what 10 sequential pages did.
const ocrPageCap = 30

// ocrPageConcurrency bounds how many pages OCR at once. PaddleOCR-VL's vLLM
// server batches concurrent requests (served with --max-num-seqs 8), and since
// decode on the GB10 is memory-bandwidth-bound, batching amortizes the per-token
// weight read across pages — an N-page scan collapses from N sequential decodes
// toward one batched decode. Bounded (< the server's seq limit) so one big PDF
// can't crowd out the OCR sidecar it shares with live chat/mail analysis.
const ocrPageConcurrency = 6

// rasterizePDF renders the first maxPages of a PDF to PNG (200 DPI) via
// pdftoppm, returned in page order (index 0 = page 1). Each page is rendered,
// bounded, read, and removed before the next page starts. Shared by the
// scanned-PDF OCR fallback and the table-page upgrade.
func rasterizePDF(ctx context.Context, pdf []byte, maxPages int) ([][]byte, error) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		return nil, fmt.Errorf("pdftoppm 미설치 (poppler-utils)")
	}

	dir, err := os.MkdirTemp("", "deneb-pdfraster-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "in.pdf"), pdf, 0o600); err != nil {
		return nil, err
	}

	runCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	if maxPages <= 0 {
		return nil, fmt.Errorf("PDF 래스터화 페이지 수가 올바르지 않습니다")
	}
	maxPages = min(maxPages, maxPDFRasterFiles)
	budget := &pdfRasterBudget{}
	out := make([][]byte, 0, maxPages)
	for page := 1; page <= maxPages; page++ {
		img, err := rasterizePDFPage(runCtx, dir, page, budget)
		if errors.Is(err, errRasterPageUnavailable) {
			break
		}
		if err != nil {
			return nil, err
		}
		out = append(out, img)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("래스터화된 페이지 없음")
	}
	return out, nil
}

var errRasterPageUnavailable = errors.New("raster page unavailable")

type pdfRasterBudget struct {
	files int
	bytes int64
}

func (budget *pdfRasterBudget) read(path string) ([]byte, error) {
	defer os.Remove(path)
	if budget.files >= maxPDFRasterFiles {
		return nil, fmt.Errorf("%w: PDF raster has more than %d files", errDocumentExtractionLimit, maxPDFRasterFiles)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxPDFRasterPageBytes {
		return nil, fmt.Errorf("%w: PDF raster page exceeds %d bytes", errDocumentExtractionLimit, maxPDFRasterPageBytes)
	}
	remaining := int64(maxPDFRasterTotalBytes) - budget.bytes
	if remaining <= 0 {
		return nil, fmt.Errorf("%w: PDF raster total exceeds %d bytes", errDocumentExtractionLimit, maxPDFRasterTotalBytes)
	}
	readLimit := min(int64(maxPDFRasterPageBytes), remaining)
	data, err := readBoundedRegularFile(path, readLimit)
	if err != nil {
		return nil, err
	}
	if err := validateOCRImage(data); err != nil {
		return nil, err
	}
	budget.files++
	budget.bytes += int64(len(data))
	return data, nil
}

func rasterizePDFPage(ctx context.Context, dir string, page int, budget *pdfRasterBudget) ([]byte, error) {
	pageArg := strconv.Itoa(page)
	prefix := "page-" + pageArg
	outPath := filepath.Join(dir, prefix+".png")
	// One process writes one page, which is read+deleted immediately. -scale-to
	// bounds the output before it reaches disk even for a hostile MediaBox.
	args := []string{
		"-png", "-r", "200", "-scale-to", strconv.Itoa(pdfRasterScalePixels),
		"-singlefile", "-f", pageArg, "-l", pageArg, "in.pdf", prefix,
	}
	cmd := exec.CommandContext(ctx, "pdftoppm", args...) //nolint:gosec // G204 — args are literals/integers, no shell, isolated temp dir
	cmd.Dir = dir
	runErr := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		_ = os.Remove(outPath)
		return nil, ctxErr
	}
	img, readErr := budget.read(outPath)
	if errors.Is(readErr, os.ErrNotExist) {
		return nil, errRasterPageUnavailable
	}
	if readErr != nil {
		return nil, readErr
	}
	if runErr != nil {
		return nil, fmt.Errorf("PDF 래스터화 실패: %w", runErr)
	}
	return img, nil
}

// contiguousRuns collapses sorted, deduplicated 0-based indices into
// inclusive [first, last] runs so adjacent candidate pages share one
// pdftoppm invocation.
func contiguousRuns(sorted []int) [][2]int {
	var runs [][2]int
	for _, idx := range sorted {
		if n := len(runs); n > 0 && runs[n-1][1] == idx-1 {
			runs[n-1][1] = idx
			continue
		}
		runs = append(runs, [2]int{idx, idx})
	}
	return runs
}

// rasterizePDFPages renders only the given sorted 0-based page indices to PNG
// (200 DPI) via pdftoppm — bounded range invocations, so a candidate deep in a
// long document (a chart on page 27) doesn't require rasterizing everything
// before it. Returns a map keyed by the same indices; a missing key means
// that page failed to render (out of range, corrupt).
func rasterizePDFPages(ctx context.Context, pdf []byte, pageIdx []int) (map[int][]byte, error) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		return nil, fmt.Errorf("pdftoppm 미설치 (poppler-utils)")
	}

	dir, err := os.MkdirTemp("", "deneb-pdfraster-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "in.pdf"), pdf, 0o600); err != nil {
		return nil, err
	}

	runCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	if len(pageIdx) > maxPDFRasterFiles {
		return nil, fmt.Errorf("%w: requested more than %d PDF raster pages", errDocumentExtractionLimit, maxPDFRasterFiles)
	}
	budget := &pdfRasterBudget{}
	out := make(map[int][]byte, len(pageIdx))
	for _, idx := range pageIdx {
		if idx < 0 {
			return nil, fmt.Errorf("PDF 페이지 인덱스가 올바르지 않습니다: %d", idx)
		}
		if _, duplicate := out[idx]; duplicate {
			continue
		}
		img, err := rasterizePDFPage(runCtx, dir, idx+1, budget)
		if errors.Is(err, errRasterPageUnavailable) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[idx] = img
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("래스터화된 페이지 없음")
	}
	return out, nil
}

// pdfOCR rasterizes a PDF and OCRs each page. It is the fallback path when
// pdftotext extracts nothing — i.e. a scanned (image-only) PDF.
func pdfOCR(ctx context.Context, pdf []byte) (string, error) {
	imgs, err := rasterizePDF(ctx, pdf, ocrPageCap)
	if err != nil {
		return "", err
	}

	// OCR pages concurrently (bounded) — the vLLM server batches the requests,
	// so an N-page scan finishes in roughly one batched decode instead of N
	// sequential ones. Each page writes its own slot so order is preserved, and a
	// per-page failure just leaves that slot empty (same skip-on-error as before).
	texts := make([]string, len(imgs))
	var (
		wg          sync.WaitGroup
		textMu      sync.Mutex
		textBytes   int
		textLimited bool
	)
	sem := make(chan struct{}, ocrPageConcurrency)
	for i, img := range imgs {
		if img == nil {
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return "", ctx.Err()
		}
		wg.Add(1)
		go func(i int, img []byte) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() { _ = recover() }() // one page's panic must not crash the gateway
			if text, err := ocrImageBytes(ctx, img); err == nil {
				text = strings.TrimSpace(text)
				textMu.Lock()
				defer textMu.Unlock()
				if len(text) > maxDocumentOutputBytes-textBytes {
					textLimited = true
					return
				}
				textBytes += len(text)
				texts[i] = text
			}
		}(i, img)
	}
	wg.Wait()
	if textLimited {
		return "", fmt.Errorf("%w: PDF OCR output exceeds %d bytes", errDocumentExtractionLimit, maxDocumentOutputBytes)
	}

	sb := newLimitedBuffer(maxDocumentOutputBytes, false)
	for i, text := range texts {
		if text == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		fmt.Fprintf(sb, "[페이지 %d]\n%s", i+1, text)
		if sb.exceeded {
			return "", fmt.Errorf("%w: PDF OCR output exceeds %d bytes", errDocumentExtractionLimit, maxDocumentOutputBytes)
		}
	}

	out := strings.TrimSpace(sb.String())
	if out == "" {
		return "", fmt.Errorf("OCR 결과 없음")
	}
	// Surface the page cap — a 30-page scanned contract must not silently
	// lose 20 pages (xlsx/csv extraction already reports truncation).
	if len(imgs) == ocrPageCap {
		if total := pdfPageCount(ctx, pdf); total > ocrPageCap {
			fmt.Fprintf(sb, "\n\n[페이지 %d–%d 생략: 총 %d페이지 중 처음 %d페이지만 OCR]",
				ocrPageCap+1, total, total, ocrPageCap)
		} else if total == 0 {
			fmt.Fprintf(sb, "\n\n[처음 %d페이지만 OCR — 문서가 더 길면 뒷부분은 생략됨]", ocrPageCap)
		}
		if sb.exceeded {
			return "", fmt.Errorf("%w: PDF OCR output exceeds %d bytes", errDocumentExtractionLimit, maxDocumentOutputBytes)
		}
		out = strings.TrimSpace(sb.String())
	}
	return out, nil
}

// pdfPageCount returns the PDF's total page count via pdfinfo, or 0 when
// unavailable (pdfinfo missing / parse failure). Best-effort — used only to
// make the OCR page-cap notice precise.
func pdfPageCount(ctx context.Context, pdf []byte) int {
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		return 0
	}
	dir, err := os.MkdirTemp("", "deneb-pdfinfo-")
	if err != nil {
		return 0
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "in.pdf"), pdf, 0o600); err != nil {
		return 0
	}
	out, err := runBoundedTextCommand(ctx, 10*time.Second, nil, func(runCtx context.Context, _ io.Reader, stdout, stderr io.Writer) error {
		cmd := exec.CommandContext(runCtx, "pdfinfo", "in.pdf")
		cmd.Dir = dir
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		return cmd.Run()
	})
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "Pages:"); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil {
				return n
			}
		}
	}
	return 0
}

// tesseract runs the tesseract CLI on image bytes piped through stdin.
func tesseract(ctx context.Context, img []byte) (string, error) {
	if _, err := exec.LookPath("tesseract"); err != nil {
		return "", fmt.Errorf("tesseract 미설치 — `apt install tesseract-ocr tesseract-ocr-kor` 필요")
	}

	text, err := runBoundedTextCommand(ctx, 60*time.Second, bytes.NewReader(img), func(runCtx context.Context, stdin io.Reader, stdout, stderr io.Writer) error {
		// `tesseract stdin stdout -l kor+eng` reads the image from stdin.
		cmd := exec.CommandContext(runCtx, "tesseract", "stdin", "stdout", "-l", ocrLangs)
		cmd.Stdin = stdin
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		return cmd.Run()
	})
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", fmt.Errorf("OCR로 추출된 텍스트 없음")
	}
	return text, nil
}

// firstLine returns the first line of s, for compact CLI error messages.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// --- markdown table helpers ---
//
// Office documents carry tabular data (Excel sheets, Word/PowerPoint tables)
// that the LLM reads far more reliably as a GitHub-flavored markdown table than
// as a flattened blob. These helpers render any [][]string grid as a well-formed
// table, padding ragged rows and escaping cell content.

// colIndexFromRef parses the 0-based column index from an A1-style cell
// reference (e.g. "A1" → 0, "B2" → 1, "AA10" → 26). Returns -1 when the ref has
// no leading letters, so the caller can fall back to positional placement.
func colIndexFromRef(ref string) int {
	n, letters := 0, 0
	for i := 0; i < len(ref); i++ {
		ch := ref[i]
		if ch >= 'a' && ch <= 'z' {
			ch -= 'a' - 'A'
		}
		if ch < 'A' || ch > 'Z' {
			break
		}
		n = n*26 + int(ch-'A') + 1
		letters++
	}
	if letters == 0 {
		return -1
	}
	return n - 1
}

// mdEscapeCell makes a string safe inside a markdown table cell: pipes are
// escaped and any newline/whitespace run collapses to a single space, so a cell
// can never break the table grid.
func mdEscapeCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.Join(strings.Fields(s), " ")
}

// mdTable renders rows as a GitHub-flavored markdown table, treating the first
// row as the header. Ragged rows are padded to the widest row so the grid stays
// valid. Returns "" when there are no cells.
func mdTable(rows [][]string) string {
	table, _ := mdTableWithLimit(rows, int(^uint(0)>>1))
	return table
}

func mdTableWithLimit(rows [][]string, maxBytes int) (string, error) {
	maxCols := 0
	for _, r := range rows {
		if len(r) > maxCols {
			maxCols = len(r)
		}
	}
	if maxCols == 0 {
		return "", nil
	}
	sb := newLimitedBuffer(maxBytes, false)
	writeRow := func(cells []string) error {
		if err := sb.WriteByte('|'); err != nil {
			return err
		}
		for i := 0; i < maxCols; i++ {
			v := ""
			if i < len(cells) {
				if len(cells[i]) > sb.Remaining() {
					return errDocumentExtractionLimit
				}
				v = mdEscapeCell(cells[i])
			}
			if err := sb.WriteByte(' '); err != nil {
				return err
			}
			if _, err := sb.WriteString(v); err != nil {
				return err
			}
			if _, err := sb.WriteString(" |"); err != nil {
				return err
			}
		}
		return sb.WriteByte('\n')
	}
	if err := writeRow(rows[0]); err != nil {
		return "", err
	}
	if err := sb.WriteByte('|'); err != nil {
		return "", err
	}
	for i := 0; i < maxCols; i++ {
		if _, err := sb.WriteString(" --- |"); err != nil {
			return "", err
		}
	}
	if err := sb.WriteByte('\n'); err != nil {
		return "", err
	}
	for _, r := range rows[1:] {
		if err := writeRow(r); err != nil {
			return "", err
		}
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}
