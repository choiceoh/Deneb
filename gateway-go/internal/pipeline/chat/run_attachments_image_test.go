package chat

import (
	"context"
	"fmt"
	"testing"
)

func TestDescribeCapturedImageUsesVisionWhenAvailable(t *testing.T) {
	ocrCalled := false
	ocr := func(context.Context, []byte) (string, error) { ocrCalled = true; return "ocr text", nil }
	vis := func(_ context.Context, _, _, mime, b64 string, _ int) string {
		if mime != "image/png" {
			t.Errorf("mime = %q, want image/png", mime)
		}
		if b64 == "" {
			t.Error("image should be base64-encoded for the vision call")
		}
		return "  차트 설명  "
	}
	if got := describeCapturedImage(context.Background(), []byte("imgbytes"), "image/png", ocr, vis); got != "차트 설명" {
		t.Errorf("vision path = %q, want trimmed description", got)
	}
	if ocrCalled {
		t.Error("OCR must not run when vision describe succeeds")
	}
}

func TestDescribeCapturedImageFallsBackToOCR(t *testing.T) {
	img := []byte("imgbytes")
	visEmpty := func(context.Context, string, string, string, string, int) string { return "" }

	// Vision unavailable/empty → OCR text (trimmed).
	ocr := func(context.Context, []byte) (string, error) { return "  글자 추출  ", nil }
	if got := describeCapturedImage(context.Background(), img, "image/jpeg", ocr, visEmpty); got != "글자 추출" {
		t.Errorf("OCR fallback = %q", got)
	}
	// Vision empty + OCR error → empty (caller skips the image).
	ocrErr := func(context.Context, []byte) (string, error) { return "", fmt.Errorf("ocr down") }
	if got := describeCapturedImage(context.Background(), img, "image/jpeg", ocrErr, visEmpty); got != "" {
		t.Errorf("OCR error should yield empty, got %q", got)
	}
	// Vision empty + no OCR wired → empty.
	if got := describeCapturedImage(context.Background(), img, "image/jpeg", nil, visEmpty); got != "" {
		t.Errorf("no OCR should yield empty, got %q", got)
	}
	// Empty image → empty, no calls.
	if got := describeCapturedImage(context.Background(), nil, "image/jpeg", ocr, visEmpty); got != "" {
		t.Errorf("empty image should yield empty, got %q", got)
	}
}
