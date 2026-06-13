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
// These build on the per-format extractors in gmail_attachment.go (pdfToText,
// xlsxToText, docxToText, pptxToText) plus the OCR fallback, exposing one entry
// point so callers outside the attachment flow — notably web fetch — get the
// same local extraction with no external `lit` CLI required.

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

// ExtractDocumentText extracts text from a document's raw bytes for callers
// outside the attachment flow (e.g. web fetch). It dispatches by MIME type /
// filename extension to the same local extractors the Gmail and Dropbox paths
// use — PDF (with a scanned-PDF OCR fallback), Excel, Word, PowerPoint, CSV.
// Returns (text, true) on success; ("", false) when the format is unsupported
// or extraction yields nothing.
func ExtractDocumentText(ctx context.Context, data []byte, filename, mimeType string) (string, bool) {
	lower := strings.ToLower(filename)
	mime := strings.ToLower(mimeType)

	switch {
	case strings.Contains(mime, "pdf") || strings.HasSuffix(lower, ".pdf"):
		if t, err := pdfToTextStructured(ctx, data); err == nil && strings.TrimSpace(t) != "" {
			return t, true
		}
		// pdftotext found nothing — likely a scanned PDF. Try OCR.
		if t, err := pdfOCR(ctx, data); err == nil && strings.TrimSpace(t) != "" {
			return t, true
		}
	case strings.Contains(mime, "spreadsheetml") || strings.HasSuffix(lower, ".xlsx") || strings.HasSuffix(lower, ".xlsm"):
		if t, err := xlsxToText(data); err == nil {
			return t, true
		}
	case strings.Contains(mime, "wordprocessingml") || strings.HasSuffix(lower, ".docx"):
		if t, err := docxToText(data); err == nil {
			return t, true
		}
	case strings.Contains(mime, "presentationml") || strings.HasSuffix(lower, ".pptx"):
		if t, err := pptxToText(data); err == nil {
			return t, true
		}
	case strings.Contains(mime, "csv") || strings.HasSuffix(lower, ".csv"):
		if t, err := csvToMarkdown(data); err == nil {
			return t, true
		}
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
	}
	return false
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
