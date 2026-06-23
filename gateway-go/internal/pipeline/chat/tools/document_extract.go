package tools

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

// --- shared document extraction (Gmail, Dropbox, and web fetch) ---
//
// extractDocument below is the single canonical MIME/extension dispatcher: it
// maps a document's type to the right per-format parser in docparse.go (PDF with
// a scanned-PDF OCR fallback, Excel/Word/PowerPoint, CSV, images, plain text).
// Every caller funnels through it instead of carrying its own copy of the switch:
//   - ExtractDocumentText — the exported (text, ok) facade used by web fetch and
//     the attachment-classifier; declines images/plain-text (not "documents").
//   - extractAttachmentText (gmail_attachment.go) — adds Gmail Korean headers/errors.
// One switch, two thin formatters — so the paths can never drift apart again.

// csvToMarkdown parses CSV bytes and renders them as a markdown table so the
// model reads columns as a grid instead of comma soup. Ragged rows are
// tolerated; output is capped to keep large exports out of the context budget.
func csvToMarkdown(data []byte) (string, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1 // tolerate ragged rows
	r.LazyQuotes = true

	const maxRows = 500
	var grid [][]string
	total := 0
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			break // stop at the first malformed line, keep what parsed
		}
		total++
		if len(grid) < maxRows {
			grid = append(grid, rec)
		}
	}

	table := mdTable(grid)
	if table == "" {
		return "", fmt.Errorf("빈 CSV")
	}
	if total > maxRows {
		table += fmt.Sprintf("\n... (%d행 이하 생략)", total-maxRows)
	}
	return table, nil
}

// docKind identifies which format family the dispatcher recognized for a payload.
type docKind int

const (
	docUnsupported docKind = iota
	docPDF
	docXLSX
	docDOCX
	docPPTX
	docImage
	docCSV
	docText
)

// docResult is the canonical extraction outcome. text holds the extracted text
// (empty on failure); err is the parser's error when extraction failed (nil on
// success, and always nil for docText/docUnsupported); ocr reports that a PDF's
// text came from the scanned-page OCR fallback rather than pdftotext. Callers
// layer their own headers and error strings over these fields.
type docResult struct {
	kind docKind
	text string
	ocr  bool
	err  error
}

// extractDocument is the single canonical dispatcher: it classifies a payload by
// MIME type / filename extension and runs the matching parser from docparse.go.
// The classification predicates and their order are the one shared definition —
// every caller (ExtractDocumentText, the Gmail attachment path, the Dropbox path)
// funnels through here, so the supported-format set can never drift between them.
// Dropbox passes an empty MIME type, which naturally degrades to filename-only
// classification.
func extractDocument(ctx context.Context, data []byte, filename, mimeType string) docResult {
	lower := strings.ToLower(filename)
	mime := strings.ToLower(mimeType)

	switch {
	case strings.Contains(mime, "pdf") || strings.HasSuffix(lower, ".pdf"):
		text, err := pdfToTextStructured(ctx, data)
		if err == nil && strings.TrimSpace(text) != "" {
			return docResult{kind: docPDF, text: text}
		}
		// pdftotext found nothing — likely a scanned PDF. Try per-page OCR.
		if ocrText, ocrErr := pdfOCR(ctx, data); ocrErr == nil && strings.TrimSpace(ocrText) != "" {
			return docResult{kind: docPDF, text: ocrText, ocr: true}
		}
		return docResult{kind: docPDF, err: err}
	case strings.Contains(mime, "spreadsheetml") || strings.HasSuffix(lower, ".xlsx") || strings.HasSuffix(lower, ".xlsm"):
		text, err := xlsxToText(data)
		return docResult{kind: docXLSX, text: text, err: err}
	case strings.Contains(mime, "wordprocessingml") || strings.HasSuffix(lower, ".docx"):
		text, err := docxToText(data)
		return docResult{kind: docDOCX, text: text, err: err}
	case strings.Contains(mime, "presentationml") || strings.HasSuffix(lower, ".pptx"):
		text, err := pptxToText(data)
		return docResult{kind: docPPTX, text: text, err: err}
	case strings.HasPrefix(mime, "image/") || hasImageExt(lower):
		text, err := imageOCR(ctx, data)
		return docResult{kind: docImage, text: text, err: err}
	case strings.Contains(mime, "csv") || strings.HasSuffix(lower, ".csv"):
		text, err := csvToMarkdown(data)
		return docResult{kind: docCSV, text: text, err: err}
	case strings.HasPrefix(mime, "text/") || isTextFile(lower):
		return docResult{kind: docText, text: string(data)}
	default:
		return docResult{kind: docUnsupported}
	}
}

// ExtractDocumentText extracts text from a document's raw bytes for callers
// outside the attachment flow (web fetch, the attachment classifier). It runs the
// canonical extractDocument dispatcher and promotes only true document formats —
// PDF (with a scanned-PDF OCR fallback), Excel, Word, PowerPoint, CSV, Markdown —
// to a successful (text, true). Images and raw plain text (.txt/.json/…) are not
// "documents" here, so they yield ("", false), as does any unsupported type.
func ExtractDocumentText(ctx context.Context, data []byte, filename, mimeType string) (string, bool) {
	r := extractDocument(ctx, data, filename, mimeType)
	switch r.kind {
	case docPDF, docXLSX, docDOCX, docPPTX, docCSV:
		if r.err == nil && strings.TrimSpace(r.text) != "" {
			return r.text, true
		}
	case docText:
		// Markdown is a real document worth extracting (like CSV, already promoted);
		// raw .txt/.json/.log/etc. stay declined as non-documents.
		if isMarkdownExt(strings.ToLower(filename)) && strings.TrimSpace(r.text) != "" {
			return r.text, true
		}
	default:
		// docImage, docUnsupported are not "documents" for this facade.
	}
	return "", false
}

// IsExtractableDocument reports whether ExtractDocumentText can handle the given
// MIME type or filename — used by the web fetch pipeline to classify a payload
// as a document worth extracting.
func IsExtractableDocument(mimeType, filename string) bool {
	lower := strings.ToLower(filename)
	mime := strings.ToLower(mimeType)
	switch {
	case strings.Contains(mime, "pdf") || strings.HasSuffix(lower, ".pdf"):
		return true
	case strings.Contains(mime, "officedocument") || strings.Contains(mime, "opendocument"):
		return true
	case strings.Contains(mime, "msword") ||
		strings.Contains(mime, "ms-excel") || strings.Contains(mime, "ms-powerpoint"):
		return true
	case strings.HasSuffix(lower, ".xlsx"), strings.HasSuffix(lower, ".xlsm"),
		strings.HasSuffix(lower, ".docx"), strings.HasSuffix(lower, ".pptx"):
		return true
	case strings.Contains(mime, "csv") || strings.HasSuffix(lower, ".csv"):
		return true
	case strings.Contains(mime, "markdown") || isMarkdownExt(lower):
		return true
	}
	return false
}

// isMarkdownExt reports whether a (lowercased) filename is a Markdown document.
func isMarkdownExt(lowerName string) bool {
	return strings.HasSuffix(lowerName, ".md") || strings.HasSuffix(lowerName, ".markdown")
}

// pdfToTextStructured extracts a digital PDF's text with pdftotext, then upgrades
// pages that look like they contain a table: those pages are rasterized and
// re-read with PaddleOCR-VL so the table comes back as markdown instead of
// whitespace-aligned columns. Pages without a confirmed table keep the faster,
// lossless pdftotext output. Degrades to plain pdftotext whenever rasterization
// or OCR is unavailable, so it is a safe drop-in for pdfToText.
func pdfToTextStructured(ctx context.Context, pdf []byte) (string, error) {
	raw, err := pdfToText(ctx, pdf)
	if err != nil {
		return "", err
	}

	pages := strings.Split(raw, "\f") // pdftotext separates pages with form feeds
	var tableIdx []int
	for i, p := range pages {
		if pageHasTable(p) {
			tableIdx = append(tableIdx, i)
		}
	}
	if len(tableIdx) == 0 {
		return strings.TrimSpace(raw), nil // no tables → nothing to upgrade
	}

	// Rasterize the table pages and re-read them with OCR. Any failure
	// (rasterizer or OCR unavailable) leaves the pdftotext text in place.
	if imgs, rerr := rasterizePDF(ctx, pdf, ocrPageCap); rerr == nil {
		for _, i := range tableIdx {
			if i >= len(imgs) || imgs[i] == nil {
				continue // page beyond the raster cap — keep pdftotext
			}
			text, oerr := ocrImageBytes(ctx, imgs[i])
			// Swap in the OCR page only when it actually produced a markdown
			// table; otherwise keep pdftotext so a false positive can't lose
			// prose to OCR truncation.
			if oerr == nil && strings.Contains(text, "| ---") {
				pages[i] = text
			}
		}
	}
	return strings.TrimSpace(strings.Join(pages, "\n\n")), nil
}

// pageHasTable reports whether a pdftotext -layout page likely contains a table:
// at least 3 consecutive lines that each carry 2+ multi-space column gaps.
func pageHasTable(page string) bool {
	consec := 0
	for _, ln := range strings.Split(page, "\n") {
		if columnGaps(ln) >= 2 {
			consec++
			if consec >= 3 {
				return true
			}
		} else {
			consec = 0
		}
	}
	return false
}

// columnGaps counts interior runs of 2+ spaces in a line — the column
// separators pdftotext -layout inserts between table columns.
func columnGaps(line string) int {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return 0
	}
	gaps, spaces := 0, 0
	for _, r := range trimmed {
		if r == ' ' {
			spaces++
			continue
		}
		if spaces >= 2 {
			gaps++
		}
		spaces = 0
	}
	return gaps
}
