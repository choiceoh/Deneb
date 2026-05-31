package tools

import "context"

// OcrImageBytes exposes the package-private OCR entry point so other packages
// (the Mini App / native-client image-capture RPC) can OCR a directly-shared
// image the same way Gmail attachments are — PaddleOCR-VL first, tesseract
// fallback. Keeping the implementation private and exposing one thin wrapper
// avoids widening the tools surface.
func OcrImageBytes(ctx context.Context, img []byte) (string, error) {
	return ocrImageBytes(ctx, img)
}
