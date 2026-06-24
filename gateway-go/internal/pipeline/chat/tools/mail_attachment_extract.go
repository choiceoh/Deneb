package tools

import (
	"context"
	"fmt"
)

// attachmentTextLimit caps extracted attachment text (runes) so a large
// document never blows the model's context budget.
const attachmentTextLimit = 50000

// extractMailAttachmentText turns raw mail attachment bytes into text the model
// can read, dispatching through the shared extractor (document_extract.go) and
// dressing the result with mail-facing Korean header/error strings. The format
// coverage — PDFs (with a scanned-PDF OCR fallback), Excel/Word/PowerPoint via
// OOXML readers, images via OCR, CSV/text inline, everything else metadata only
// — lives in the shared dispatcher, so mail, files, and web-fetch paths stay in
// lockstep.
func extractMailAttachmentText(ctx context.Context, filename, mimeType string, size int, data []byte) string {
	r := extractDocument(ctx, data, filename, mimeType)
	switch r.kind {
	case docPDF:
		if r.err != nil {
			return fmt.Sprintf("📎 %s (PDF, %s)\n\n⚠️ PDF 텍스트 추출 실패: %s", filename, formatBytes(int64(size)), r.err)
		}
		label := "PDF"
		if r.ocr {
			label = "PDF, OCR"
		}
		return fmt.Sprintf("## 📎 %s (%s)\n\n%s", filename, label, truncate(r.text, attachmentTextLimit))
	case docXLSX:
		if r.err != nil {
			return fmt.Sprintf("📎 %s (Excel, %s)\n\n⚠️ 엑셀 읽기 실패: %s", filename, formatBytes(int64(size)), r.err)
		}
		return fmt.Sprintf("## 📎 %s (Excel)\n\n%s", filename, truncate(r.text, attachmentTextLimit))
	case docDOCX:
		if r.err != nil {
			return fmt.Sprintf("📎 %s (Word, %s)\n\n⚠️ Word 읽기 실패: %s", filename, formatBytes(int64(size)), r.err)
		}
		return fmt.Sprintf("## 📎 %s (Word)\n\n%s", filename, truncate(r.text, attachmentTextLimit))
	case docPPTX:
		if r.err != nil {
			return fmt.Sprintf("📎 %s (PowerPoint, %s)\n\n⚠️ PowerPoint 읽기 실패: %s", filename, formatBytes(int64(size)), r.err)
		}
		return fmt.Sprintf("## 📎 %s (PowerPoint)\n\n%s", filename, truncate(r.text, attachmentTextLimit))
	case docImage:
		if r.err != nil {
			return fmt.Sprintf("📎 %s (이미지, %s)\n\n⚠️ 이미지 OCR 실패: %s", filename, formatBytes(int64(size)), r.err)
		}
		return fmt.Sprintf("## 📎 %s (이미지 OCR)\n\n%s", filename, truncate(r.text, attachmentTextLimit))
	case docCSV:
		// csvToMarkdown only fails on an empty CSV; fall back to the raw bytes so
		// the model still sees the content, matching the prior behavior.
		body := r.text
		if r.err != nil {
			body = string(data)
		}
		return fmt.Sprintf("## 📎 %s (CSV)\n\n%s", filename, truncate(body, attachmentTextLimit))
	case docText:
		return fmt.Sprintf("## 📎 %s\n\n%s", filename, truncate(r.text, attachmentTextLimit))
	default:
		return fmt.Sprintf("📎 %s (%s, %s) — 텍스트로 추출할 수 없는 형식입니다.", filename, mimeType, formatBytes(int64(size)))
	}
}
