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

// The mail path reads attachments through an extractor, which OCRs images. These
// cover the upgrade that puts vision in front of it — and, just as important, the
// cases that must NOT spend a vision call (mail arrives unattended and in bulk).
func TestWithImageVisionRoutesAttachments(t *testing.T) {
	big := make([]byte, visionAttachmentMinBytes)
	small := make([]byte, visionAttachmentMinBytes-1)

	cases := []struct {
		name       string
		data       []byte
		filename   string
		mime       string
		visionText string
		want       string
		wantVision bool
	}{
		{"big image goes to vision", big, "site.jpg", "image/jpeg", "현장 사진 설명", "현장 사진 설명", true},
		{"vision empty falls through to the extractor", big, "site.jpg", "image/jpeg", "", "extracted", true},
		{"small image is not worth a vision call", small, "logo.png", "image/png", "should not run", "extracted", false},
		{"non-image never goes to vision", big, "quote.pdf", "application/pdf", "should not run", "extracted", false},
		{"octet-stream is judged by filename", big, "scan.JPEG", "application/octet-stream", "스캔 견적서", "스캔 견적서", true},
		{"extensionless octet-stream stays on the extractor", big, "attachment", "application/octet-stream", "should not run", "extracted", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			visionCalled := false
			vis := func(context.Context, string, string, string, string, int) string {
				visionCalled = true
				return tc.visionText
			}
			extract := func(context.Context, []byte, string, string) string { return "extracted" }
			got := withImageVision(extract, vis)(context.Background(), tc.data, tc.filename, tc.mime)
			if got != tc.want {
				t.Errorf("text = %q, want %q", got, tc.want)
			}
			if visionCalled != tc.wantVision {
				t.Errorf("vision called = %v, want %v", visionCalled, tc.wantVision)
			}
		})
	}
}

func TestWithImageVisionKeepsNilExtractorNil(t *testing.T) {
	// mailanalysis treats a nil AttachmentExtractFn as "attachments disabled";
	// wrapping must not turn that off by handing back a non-nil func.
	if WithImageVision(nil) != nil {
		t.Error("wrapping a nil extractor must stay nil")
	}
}
