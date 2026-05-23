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

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// --- attachment: fetch + extract email attachments (PDF/Excel/Word/PowerPoint/image/text) ---

// attachmentTextLimit caps extracted attachment text (runes) so a large
// document never blows the model's context budget.
const attachmentTextLimit = 50000

func gmailAttachment(ctx context.Context, client *gmail.Client, p GmailParams) (string, error) {
	if p.MessageID == "" {
		return "", fmt.Errorf("message_id는 attachment 액션에 필수입니다")
	}

	msg, err := client.GetMessage(ctx, p.MessageID)
	if err != nil {
		return "", err
	}
	if len(msg.Attachments) == 0 {
		return "이 메일에는 첨부파일이 없습니다.", nil
	}

	// No selector → list the attachments.
	if strings.TrimSpace(p.Attachment) == "" {
		var sb strings.Builder
		fmt.Fprintf(&sb, "## 📎 첨부파일 (%d개)\n\n", len(msg.Attachments))
		for i, a := range msg.Attachments {
			fmt.Fprintf(&sb, "%d. %s — %s, %s\n", i+1, a.Filename, a.MimeType, formatBytes(int64(a.Size)))
		}
		sb.WriteString("\n내용을 보려면 attachment에 파일명 또는 번호를 지정하세요.")
		return sb.String(), nil
	}

	att := resolveAttachment(msg.Attachments, p.Attachment)
	if att == nil {
		return fmt.Sprintf("첨부파일 %q를 찾을 수 없습니다. attachment 인자 없이 호출하면 목록을 봅니다.", p.Attachment), nil
	}
	if att.AttachmentID == "" {
		return fmt.Sprintf("'%s'는 인라인 첨부라 별도 추출이 지원되지 않습니다.", att.Filename), nil
	}

	data, err := client.GetAttachment(ctx, p.MessageID, att.AttachmentID)
	if err != nil {
		return "", fmt.Errorf("첨부파일 다운로드 실패: %w", err)
	}

	// download mode → save to disk and hand the path back for send_file.
	if p.Download {
		path, err := saveAttachmentToDisk(att.Filename, data)
		if err != nil {
			return "", fmt.Errorf("첨부파일 저장 실패: %w", err)
		}
		return fmt.Sprintf("📎 첨부파일을 저장했습니다: `%s` (%s)\nsend_file 도구의 file_path 인자에 이 경로를 넘기면 사용자에게 전달됩니다.",
			path, formatBytes(int64(len(data)))), nil
	}

	return extractAttachmentText(ctx, att, data), nil
}

// resolveAttachment picks an attachment by 1-based index or by filename
// (exact match first, then case-insensitive substring).
func resolveAttachment(atts []gmail.AttachmentInfo, sel string) *gmail.AttachmentInfo {
	sel = strings.TrimSpace(sel)
	if idx, err := strconv.Atoi(sel); err == nil && idx >= 1 && idx <= len(atts) {
		return &atts[idx-1]
	}
	for i := range atts {
		if atts[i].Filename == sel {
			return &atts[i]
		}
	}
	lower := strings.ToLower(sel)
	for i := range atts {
		if strings.Contains(strings.ToLower(atts[i].Filename), lower) {
			return &atts[i]
		}
	}
	return nil
}

// extractAttachmentText turns raw attachment bytes into text the model can
// read: PDFs via pdftotext (OCR fallback for scans), Excel/Word/PowerPoint
// via stdlib OOXML readers, images via OCR, text files directly, everything
// else metadata only.
func extractAttachmentText(ctx context.Context, att *gmail.AttachmentInfo, data []byte) string {
	lower := strings.ToLower(att.Filename)
	mime := strings.ToLower(att.MimeType)
	isPDF := strings.Contains(mime, "pdf") || strings.HasSuffix(lower, ".pdf")
	isXLSX := strings.Contains(mime, "spreadsheetml") ||
		strings.HasSuffix(lower, ".xlsx") || strings.HasSuffix(lower, ".xlsm")
	isDOCX := strings.Contains(mime, "wordprocessingml") || strings.HasSuffix(lower, ".docx")
	isPPTX := strings.Contains(mime, "presentationml") || strings.HasSuffix(lower, ".pptx")
	isImage := strings.HasPrefix(mime, "image/") || hasImageExt(lower)

	switch {
	case isPDF:
		text, err := pdfToText(ctx, data)
		if err == nil {
			return fmt.Sprintf("## 📎 %s (PDF)\n\n%s", att.Filename, truncate(text, attachmentTextLimit))
		}
		// pdftotext yielded nothing — likely a scanned PDF. Try OCR.
		if ocrText, ocrErr := pdfOCR(ctx, data); ocrErr == nil {
			return fmt.Sprintf("## 📎 %s (PDF, OCR)\n\n%s", att.Filename, truncate(ocrText, attachmentTextLimit))
		}
		return fmt.Sprintf("📎 %s (PDF, %s)\n\n⚠️ PDF 텍스트 추출 실패: %s", att.Filename, formatBytes(int64(att.Size)), err)
	case isXLSX:
		text, err := xlsxToText(data)
		if err != nil {
			return fmt.Sprintf("📎 %s (Excel, %s)\n\n⚠️ 엑셀 읽기 실패: %s", att.Filename, formatBytes(int64(att.Size)), err)
		}
		return fmt.Sprintf("## 📎 %s (Excel)\n\n%s", att.Filename, truncate(text, attachmentTextLimit))
	case isDOCX:
		text, err := docxToText(data)
		if err != nil {
			return fmt.Sprintf("📎 %s (Word, %s)\n\n⚠️ Word 읽기 실패: %s", att.Filename, formatBytes(int64(att.Size)), err)
		}
		return fmt.Sprintf("## 📎 %s (Word)\n\n%s", att.Filename, truncate(text, attachmentTextLimit))
	case isPPTX:
		text, err := pptxToText(data)
		if err != nil {
			return fmt.Sprintf("📎 %s (PowerPoint, %s)\n\n⚠️ PowerPoint 읽기 실패: %s", att.Filename, formatBytes(int64(att.Size)), err)
		}
		return fmt.Sprintf("## 📎 %s (PowerPoint)\n\n%s", att.Filename, truncate(text, attachmentTextLimit))
	case isImage:
		text, err := imageOCR(ctx, data)
		if err != nil {
			return fmt.Sprintf("📎 %s (이미지, %s)\n\n⚠️ 이미지 OCR 실패: %s", att.Filename, formatBytes(int64(att.Size)), err)
		}
		return fmt.Sprintf("## 📎 %s (이미지 OCR)\n\n%s", att.Filename, truncate(text, attachmentTextLimit))
	case strings.HasPrefix(mime, "text/") || isTextFile(lower):
		return fmt.Sprintf("## 📎 %s\n\n%s", att.Filename, truncate(string(data), attachmentTextLimit))
	default:
		return fmt.Sprintf("📎 %s (%s, %s) — 텍스트로 추출할 수 없는 형식입니다.", att.Filename, att.MimeType, formatBytes(int64(att.Size)))
	}
}

// appendAttachmentText fetches and extracts every attachment of a message and
// appends the text to detail.Body, so the analyze pipeline — which reads
// detail.Body — sees contract/invoice content, not just the cover note.
func appendAttachmentText(ctx context.Context, client *gmail.Client, detail *gmail.MessageDetail) {
	if detail == nil || len(detail.Attachments) == 0 {
		return
	}

	const maxAttachments = 10
	var sb strings.Builder
	for i := range detail.Attachments {
		if i >= maxAttachments {
			break
		}
		att := detail.Attachments[i]
		if att.AttachmentID == "" {
			continue
		}
		data, err := client.GetAttachment(ctx, detail.ID, att.AttachmentID)
		if err != nil {
			continue
		}
		sb.WriteString("\n\n")
		sb.WriteString(extractAttachmentText(ctx, &att, data))
	}
	if sb.Len() == 0 {
		return
	}
	detail.Body += "\n\n--- 첨부파일 내용 ---" + truncate(sb.String(), 80000)
}

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

// saveAttachmentToDisk writes attachment bytes to a temp file so the agent can
// hand the path to the send_file tool. The filename is sanitized to its base
// component to prevent path traversal.
func saveAttachmentToDisk(filename string, data []byte) (string, error) {
	dir := filepath.Join(os.TempDir(), "deneb-gmail-attachments")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := filepath.Base(strings.TrimSpace(filename))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "attachment"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
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

	const maxRowsPerSheet = 500
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
		for _, row := range rows {
			cells := make([]string, 0, len(row.Cells))
			for _, c := range row.Cells {
				cells = append(cells, xlsxCellValue(c, shared))
			}
			if strings.TrimSpace(strings.Join(cells, "")) == "" {
				continue // skip fully empty rows
			}
			sb.WriteString(strings.Join(cells, " | "))
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

// extractOOXMLText streams an Office Open XML part and concatenates the text
// inside every <t> element, separating paragraphs (<p>) with newlines.
func extractOOXMLText(r io.Reader) string {
	decoder := xml.NewDecoder(r)
	var sb strings.Builder
	var inT, paragraphOpen bool
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				paragraphOpen = true
			case "t":
				inT = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p":
				if paragraphOpen {
					sb.WriteString("\n")
					paragraphOpen = false
				}
			case "t":
				inT = false
			}
		case xml.CharData:
			if inT {
				sb.Write(t)
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

// imageOCR runs tesseract on raw image bytes.
func imageOCR(ctx context.Context, img []byte) (string, error) {
	return tesseract(ctx, img)
}

// pdfOCR rasterizes a PDF with pdftoppm and OCRs each page. It is the fallback
// path when pdftotext extracts nothing — i.e. a scanned (image-only) PDF.
func pdfOCR(ctx context.Context, pdf []byte) (string, error) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		return "", fmt.Errorf("pdftoppm 미설치 (poppler-utils)")
	}
	if _, err := exec.LookPath("tesseract"); err != nil {
		return "", fmt.Errorf("tesseract 미설치 — `apt install tesseract-ocr tesseract-ocr-kor` 필요")
	}

	dir, err := os.MkdirTemp("", "deneb-pdfocr-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "in.pdf"), pdf, 0o600); err != nil {
		return "", err
	}

	runCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	// Rasterize the first ocrPageLimit pages to PNG at 200 DPI. The command
	// runs inside the temp dir so every argument is a literal — no variable
	// (and therefore no tainted) input reaches the subprocess.
	const ocrPageLimit = "10"
	rast := exec.CommandContext(runCtx, "pdftoppm", "-png", "-r", "200",
		"-f", "1", "-l", ocrPageLimit, "in.pdf", "page")
	rast.Dir = dir
	if err := rast.Run(); err != nil {
		return "", fmt.Errorf("PDF 래스터화 실패: %w", err)
	}

	pages, _ := filepath.Glob(filepath.Join(dir, "page") + "-*.png")
	sort.Strings(pages)
	if len(pages) == 0 {
		return "", fmt.Errorf("래스터화된 페이지 없음")
	}

	var sb strings.Builder
	for i, page := range pages {
		img, err := os.ReadFile(page)
		if err != nil {
			continue
		}
		text, err := tesseract(runCtx, img)
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
