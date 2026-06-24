package tools

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

	text := strings.TrimSpace(out.String())
	if text == "" {
		return "", fmt.Errorf("추출된 텍스트가 없습니다 (스캔본 PDF일 수 있음)")
	}
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
	sort.Slice(sheetFiles, func(i, j int) bool { return sheetFiles[i].Name < sheetFiles[j].Name })

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
		if idx > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "### Sheet %d\n\n", idx+1)

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
		if table := mdTable(grid); table != "" {
			sb.WriteString(table)
			sb.WriteString("\n")
		}
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
	var sb strings.Builder
	var inT, paragraphOpen bool

	// Table state. tableDepth>0 means we're inside a <tbl>; rows/cells are only
	// tracked at the outermost level (depth 1) — a nested table's text inlines
	// into its parent cell rather than corrupting the grid.
	tableDepth := 0
	var rows [][]string
	var curRow []string
	var curCell strings.Builder
	cellOpen := false

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "tbl":
				tableDepth++
				if tableDepth == 1 {
					rows = nil
				}
			case "tr":
				if tableDepth == 1 {
					curRow = nil
				}
			case "tc":
				if tableDepth == 1 {
					cellOpen = true
					curCell.Reset()
				}
			case "p":
				if cellOpen {
					// New paragraph inside a cell → keep words apart.
					if curCell.Len() > 0 {
						curCell.WriteByte(' ')
					}
				} else {
					paragraphOpen = true
				}
			case "t":
				inT = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "tbl":
				if tableDepth == 1 {
					if table := mdTable(rows); table != "" {
						if sb.Len() > 0 {
							sb.WriteString("\n")
						}
						sb.WriteString(table)
						sb.WriteString("\n")
					}
					rows = nil
				}
				if tableDepth > 0 {
					tableDepth--
				}
			case "tr":
				if tableDepth == 1 {
					rows = append(rows, curRow)
					curRow = nil
				}
			case "tc":
				if tableDepth == 1 {
					curRow = append(curRow, strings.TrimSpace(curCell.String()))
					curCell.Reset()
					cellOpen = false
				}
			case "p":
				if !cellOpen && paragraphOpen {
					sb.WriteString("\n")
					paragraphOpen = false
				}
			case "t":
				inT = false
			}
		case xml.CharData:
			if inT {
				if cellOpen {
					curCell.Write(t)
				} else {
					sb.Write(t)
				}
			}
		}
	}
	return sb.String()
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
	sort.Slice(slideFiles, func(i, j int) bool { return slideFiles[i].Name < slideFiles[j].Name })

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

// pdfOCR rasterizes a PDF and OCRs each page. It is the fallback path when
// pdftotext extracts nothing — i.e. a scanned (image-only) PDF.
func pdfOCR(ctx context.Context, pdf []byte) (string, error) {
	imgs, err := rasterizePDF(ctx, pdf, ocrPageCap)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for i, img := range imgs {
		if img == nil {
			continue
		}
		text, err := ocrImageBytes(ctx, img)
		if err != nil || strings.TrimSpace(text) == "" {
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
	return out, nil
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
