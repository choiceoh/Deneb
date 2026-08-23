package document

import (
	"context"
	"strings"
)

// ExtractText runs the canonical format dispatcher and returns its readable
// text, whether OCR produced that text, and any parser error. Unsupported
// formats return empty text and a nil error.
func ExtractText(ctx context.Context, data []byte, filename, mimeType string) (text string, usedOCR bool, err error) {
	r := extractDocument(ctx, data, filename, mimeType)
	return r.text, r.ocr, r.err
}

// ExtractFileText applies file-store semantics to the canonical dispatcher.
// CSV parser failures fall back to the raw payload; unsupported formats and
// other parser failures return no text.
func ExtractFileText(ctx context.Context, filename string, data []byte) string {
	return extractFileText(ctx, filename, data)
}

// ExtractFileTextWithoutOCR extracts exact-search text without invoking image
// or scanned-PDF OCR. It is intended for latency-bounded request paths; the
// background file index continues to use ExtractFileText and owns OCR work.
// complete is false when OCR would be required to cover the payload.
func ExtractFileTextWithoutOCR(ctx context.Context, filename string, data []byte) (text string, complete bool) {
	r := extractDocumentWithOCR(ctx, data, filename, "", false)
	if r.kind == docImage || r.kind == docUnsupported || (r.kind == docPDF && strings.TrimSpace(r.text) == "") {
		return "", false
	}
	if r.err != nil && r.kind != docCSV {
		return "", false
	}
	return fileTextFromResult(r, data), true
}

func extractFileText(ctx context.Context, filename string, data []byte) string {
	return fileTextFromResult(extractDocument(ctx, data, filename, ""), data)
}

func fileTextFromResult(r docResult, data []byte) string {
	if r.kind == docCSV && r.err != nil {
		return string(data)
	}
	return r.text
}

// ExtractAttachmentText returns any readable attachment content, including
// images and plain text. It is the raw-text API used by mail analysis and
// differs from ExtractDocumentText, which intentionally declines those kinds.
func ExtractAttachmentText(ctx context.Context, data []byte, filename, mimeType string) string {
	r := extractDocument(ctx, data, filename, mimeType)
	return strings.TrimSpace(r.text)
}

// OCRImage recognizes text in an image using the local PaddleOCR-VL service
// with the command-line tesseract engine as a fallback.
func OCRImage(ctx context.Context, image []byte) (string, error) {
	return imageOCR(ctx, image)
}

// OCRPDF rasterizes and recognizes scanned PDF pages directly. Callers that
// accept ordinary documents should prefer ExtractText, which first preserves
// lossless text from born-digital PDFs.
func OCRPDF(ctx context.Context, pdf []byte) (string, error) {
	return pdfOCR(ctx, pdf)
}
