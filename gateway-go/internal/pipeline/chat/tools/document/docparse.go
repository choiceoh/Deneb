package document

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
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

// pdfToText extracts text from PDF bytes via the `pdftotext` CLI (poppler).
// The PDF is piped through stdin so no temp file is needed.
func pdfToText(ctx context.Context, pdf []byte) (string, error) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return "", fmt.Errorf("pdftotext 미설치 — DGX Spark에서 `apt install poppler-utils` 실행 필요")
	}

	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// `pdftotext -layout - -` reads the PDF from stdin, writes text to stdout.
	cmd := exec.CommandContext(runCtx, "pdftotext", "-layout", "-", "-")
	cmd.Stdin = bytes.NewReader(pdf)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errBuf.String()); msg != "" {
			return "", fmt.Errorf("%s", firstLine(msg))
		}
		return "", err
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

	shared := readXLSXSharedStrings(zr)

	var sheetFiles []*zip.File
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
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
		maxRowsPerSheet = 500
		// maxExcelCols is Excel's hard column ceiling (XFD). A cell ref beyond it
		// is malformed or crafted; capping here stops a single bad ref from
		// driving the column-padding loop into an unbounded allocation — a DoS
		// vector, since .xlsx bytes are untrusted attachment input.
		maxExcelCols = 16384
	)
	var sb strings.Builder
	for idx, f := range sheetFiles {
		var sheet xlsxSheet
		if err := unmarshalZipXML(f, &sheet); err != nil {
			continue
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
		table := mdTable(grid)
		if table == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "### Sheet %d\n\n", idx+1)
		sb.WriteString(table)
		sb.WriteString("\n")
		if truncated {
			fmt.Fprintf(&sb, "... (%d행 이하 생략)\n", len(sheet.Rows)-maxRowsPerSheet)
		}
	}

	out := strings.TrimSpace(sb.String())
	if out == "" {
		return "", fmt.Errorf("빈 워크북")
	}
	return out, nil
}

func readXLSXSharedStrings(zr *zip.Reader) []string {
	for _, f := range zr.File {
		if f.Name != "xl/sharedStrings.xml" {
			continue
		}
		var sst xlsxSST
		if err := unmarshalZipXML(f, &sst); err != nil {
			return nil
		}
		out := make([]string, len(sst.Items))
		for i, si := range sst.Items {
			out[i] = si.text()
		}
		return out
	}
	return nil
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

func unmarshalZipXML(f *zip.File, v any) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return xml.NewDecoder(rc).Decode(v)
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
	decoder := xml.NewDecoder(r)
	extractor := &ooxmlTextExtractor{}
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		extractor.consume(token)
	}
	return extractor.output.String()
}

// ooxmlTextExtractor owns the streaming OOXML state machine. Table depth and
// cell state stay together so nested tables inline into their parent cell while
// only the outer table emits markdown.
type ooxmlTextExtractor struct {
	output        strings.Builder
	inText        bool
	paragraphOpen bool
	tableDepth    int
	rows          [][]string
	currentRow    []string
	currentCell   strings.Builder
	cellOpen      bool
}

func (e *ooxmlTextExtractor) consume(token xml.Token) {
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
	table := mdTable(e.rows)
	if table == "" {
		return
	}
	if e.output.Len() > 0 {
		e.output.WriteString("\n")
	}
	e.output.WriteString(table)
	e.output.WriteString("\n")
}

func (e *ooxmlTextExtractor) endRow() {
	if e.tableDepth != 1 {
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
	e.currentRow = append(e.currentRow, strings.TrimSpace(e.currentCell.String()))
	e.currentCell.Reset()
	e.cellOpen = false
}

func (e *ooxmlTextExtractor) startParagraph() {
	if e.cellOpen {
		if e.currentCell.Len() > 0 {
			e.currentCell.WriteByte(' ')
		}
		return
	}
	e.paragraphOpen = true
}

func (e *ooxmlTextExtractor) endParagraph() {
	if e.cellOpen || !e.paragraphOpen {
		return
	}
	e.output.WriteString("\n")
	e.paragraphOpen = false
}

func (e *ooxmlTextExtractor) appendText(text xml.CharData) {
	if !e.inText {
		return
	}
	if e.cellOpen {
		e.currentCell.Write(text)
		return
	}
	e.output.Write(text)
}

// docxToText extracts body text from a .docx file (word/document.xml).
func docxToText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("docx 압축 해제 실패: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		text := strings.TrimSpace(extractOOXMLText(rc))
		rc.Close()
		if text == "" {
			return "", fmt.Errorf("빈 문서")
		}
		return text, nil
	}
	return "", fmt.Errorf("word/document.xml을 찾을 수 없습니다")
}

// pptxToText extracts text from every slide of a .pptx file (ppt/slides/slideN.xml).
func pptxToText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("pptx 압축 해제 실패: %w", err)
	}

	var slideFiles []*zip.File
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			slideFiles = append(slideFiles, f)
		}
	}
	if len(slideFiles) == 0 {
		return "", fmt.Errorf("슬라이드를 찾을 수 없습니다")
	}
	sort.Slice(slideFiles, func(i, j int) bool {
		return ooxmlPartLess(slideFiles[i].Name, slideFiles[j].Name, "ppt/slides/slide")
	})

	var sb strings.Builder
	for i, f := range slideFiles {
		rc, err := f.Open()
		if err != nil {
			continue
		}
		slideText := strings.TrimSpace(extractOOXMLText(rc))
		rc.Close()
		if slideText == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "### Slide %d\n\n%s\n", i+1, slideText)
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
	return ocrImageBytes(ctx, img)
}

// ocrPageCap bounds how many pages of a PDF we rasterize — enough for typical
// business documents without letting a huge PDF monopolize the GPU.
const ocrPageCap = 10

// ocrPageConcurrency bounds how many pages OCR at once. PaddleOCR-VL's vLLM
// server batches concurrent requests (served with --max-num-seqs 8), and since
// decode on the GB10 is memory-bandwidth-bound, batching amortizes the per-token
// weight read across pages — an N-page scan collapses from N sequential decodes
// toward one batched decode. Bounded (< the server's seq limit) so one big PDF
// can't crowd out the OCR sidecar it shares with live chat/mail analysis.
const ocrPageConcurrency = 6

// rasterizePDF renders the first maxPages of a PDF to PNG (200 DPI) via
// pdftoppm, returned in page order (index 0 = page 1; a nil entry means that
// page failed to read). Shared by the scanned-PDF OCR fallback and the
// table-page upgrade.
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

	// The command runs inside the temp dir so every argument is a literal — no
	// variable (and therefore no tainted) input reaches the subprocess.
	rastArgs := []string{"-png", "-r", "200", "-f", "1", "-l", strconv.Itoa(maxPages), "in.pdf", "page"}
	rast := exec.CommandContext(runCtx, "pdftoppm", rastArgs...) //nolint:gosec // G204 — all args are literals (maxPages is an int), no shell, runs in temp dir
	rast.Dir = dir
	if err := rast.Run(); err != nil {
		return nil, fmt.Errorf("PDF 래스터화 실패: %w", err)
	}

	// pdftoppm names files page-N.png without zero-padding, so order by the
	// parsed page number — a lexicographic sort would put page-10 before page-2.
	files, _ := filepath.Glob(filepath.Join(dir, "page") + "-*.png")
	byNum := make(map[int][]byte, len(files))
	maxN := 0
	for _, f := range files {
		numStr := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "page-"), ".png")
		n, err := strconv.Atoi(numStr)
		if err != nil {
			continue
		}
		b, rerr := os.ReadFile(f)
		if rerr != nil {
			continue
		}
		byNum[n] = b
		if n > maxN {
			maxN = n
		}
	}
	if maxN == 0 {
		return nil, fmt.Errorf("래스터화된 페이지 없음")
	}
	out := make([][]byte, maxN)
	for n, b := range byNum {
		out[n-1] = b
	}
	return out, nil
}

// rasterizePDFPages renders only the given 0-based page indices to PNG
// (200 DPI) via pdftoppm — one bounded invocation per page, so a candidate
// deep in a long document (a chart on page 27) doesn't require rasterizing
// everything before it. Returns a map keyed by the same indices; a missing
// key means that page failed to render (out of range, corrupt).
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

	for _, idx := range pageIdx {
		n := strconv.Itoa(idx + 1)
		// Every argument is a literal (page numbers are ints), no shell, and the
		// command runs inside the temp dir — same containment as rasterizePDF.
		rast := exec.CommandContext(runCtx, "pdftoppm", "-png", "-r", "200", "-f", n, "-l", n, "in.pdf", "page") //nolint:gosec // G204 — literal args, no shell, temp dir
		rast.Dir = dir
		_ = rast.Run() // a page out of range just produces no file
	}

	// pdftoppm may or may not zero-pad the page number depending on version —
	// parse it back from the filename like rasterizePDF does.
	files, _ := filepath.Glob(filepath.Join(dir, "page") + "-*.png")
	out := make(map[int][]byte, len(pageIdx))
	for _, f := range files {
		numStr := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "page-"), ".png")
		n, err := strconv.Atoi(numStr)
		if err != nil {
			continue
		}
		b, rerr := os.ReadFile(f)
		if rerr != nil {
			continue
		}
		out[n-1] = b
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
	var wg sync.WaitGroup
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
				texts[i] = strings.TrimSpace(text)
			}
		}(i, img)
	}
	wg.Wait()

	var sb strings.Builder
	for i, text := range texts {
		if text == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		fmt.Fprintf(&sb, "[페이지 %d]\n%s", i+1, text)
	}

	out := strings.TrimSpace(sb.String())
	if out == "" {
		return "", fmt.Errorf("OCR 결과 없음")
	}
	// Surface the page cap — a 30-page scanned contract must not silently
	// lose 20 pages (xlsx/csv extraction already reports truncation).
	if len(imgs) == ocrPageCap {
		if total := pdfPageCount(ctx, pdf); total > ocrPageCap {
			fmt.Fprintf(&sb, "\n\n[페이지 %d–%d 생략: 총 %d페이지 중 처음 %d페이지만 OCR]",
				ocrPageCap+1, total, total, ocrPageCap)
		} else if total == 0 {
			fmt.Fprintf(&sb, "\n\n[처음 %d페이지만 OCR — 문서가 더 길면 뒷부분은 생략됨]", ocrPageCap)
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
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "pdfinfo", "in.pdf")
	cmd.Dir = dir
	outBytes, err := cmd.Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(outBytes), "\n") {
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

	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// `tesseract stdin stdout -l kor+eng` reads the image from stdin.
	cmd := exec.CommandContext(runCtx, "tesseract", "stdin", "stdout", "-l", ocrLangs)
	cmd.Stdin = bytes.NewReader(img)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errBuf.String()); msg != "" {
			return "", fmt.Errorf("%s", firstLine(msg))
		}
		return "", err
	}

	text := strings.TrimSpace(out.String())
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
	maxCols := 0
	for _, r := range rows {
		if len(r) > maxCols {
			maxCols = len(r)
		}
	}
	if maxCols == 0 {
		return ""
	}
	var sb strings.Builder
	writeRow := func(cells []string) {
		sb.WriteByte('|')
		for i := 0; i < maxCols; i++ {
			v := ""
			if i < len(cells) {
				v = mdEscapeCell(cells[i])
			}
			sb.WriteByte(' ')
			sb.WriteString(v)
			sb.WriteString(" |")
		}
		sb.WriteByte('\n')
	}
	writeRow(rows[0])
	sb.WriteByte('|')
	for i := 0; i < maxCols; i++ {
		sb.WriteString(" --- |")
	}
	sb.WriteByte('\n')
	for _, r := range rows[1:] {
		writeRow(r)
	}
	return strings.TrimRight(sb.String(), "\n")
}
