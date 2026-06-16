package tools

import (
	"context"
	"strings"
)

// OcrImageBytes exposes the package-private OCR entry point so other packages
// (the Mini App / native-client image-capture RPC) can OCR a directly-shared
// image the same way Gmail attachments are — PaddleOCR-VL first, tesseract
// fallback. Keeping the implementation private and exposing one thin wrapper
// avoids widening the tools surface.
func OcrImageBytes(ctx context.Context, img []byte) (string, error) {
	return ocrImageBytes(ctx, img)
}

// ExtractAttachmentTextBytes extracts readable text from an email attachment's
// raw bytes — documents (PDF/Excel/Word/PowerPoint/CSV) AND images (OCR) — for
// the autonomous mail-analysis attachment gate (internal/platform/gmailpoll).
// Broader than ExtractDocumentText, which excludes images: a scanned 견적서 photo
// must come back as text here. Returns "" when nothing readable is produced.
// Injected via DI so the platform layer never imports this pipeline package.
func ExtractAttachmentTextBytes(ctx context.Context, data []byte, filename, mimeType string) string {
	r := extractDocument(ctx, data, filename, mimeType)
	return strings.TrimSpace(r.text)
}
