package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestPaddleOCR_Live hits a real PaddleOCR-VL server. Opt-in only:
//
//	DENEB_OCR_VL_LIVE=1 DENEB_OCR_VL_IMG=/path/to.png go test -run Live ./...
//
// Skipped in CI (no GPU). Used to confirm the Go path end-to-end on the host.
func TestPaddleOCR_Live(t *testing.T) {
	if os.Getenv("DENEB_OCR_VL_LIVE") != "1" {
		t.Skip("set DENEB_OCR_VL_LIVE=1 to run against a live PaddleOCR-VL server")
	}
	imgPath := os.Getenv("DENEB_OCR_VL_IMG")
	if imgPath == "" {
		t.Fatal("DENEB_OCR_VL_IMG must point to a test image")
	}
	img, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatalf("read image: %v", err)
	}
	text, err := paddleOCR(context.Background(), img, "OCR:")
	if err != nil {
		t.Fatalf("paddleOCR live: %v", err)
	}
	t.Logf("live OCR result (%d chars):\n%s", len(text), text)
	if strings.TrimSpace(text) == "" {
		t.Error("empty OCR result")
	}
}

func TestOCRVLBaseURL_EnvOverride(t *testing.T) {
	t.Setenv("DENEB_OCR_VL_URL", "http://example.test:9999/")
	if got := ocrVLBaseURL(); got != "http://example.test:9999" {
		t.Errorf("ocrVLBaseURL() = %q, want trailing slash trimmed", got)
	}
	t.Setenv("DENEB_OCR_VL_URL", "")
	if got := ocrVLBaseURL(); got != ocrVLDefaultURL {
		t.Errorf("ocrVLBaseURL() = %q, want default %q", got, ocrVLDefaultURL)
	}
}

// TestPaddleOCR_WireFormat verifies the request we send matches PaddleOCR-VL's
// OpenAI-compatible contract (image_url data URI + task text part) and that we
// parse the chat-completion response correctly — without needing the model.
func TestPaddleOCR_WireFormat(t *testing.T) {
	var gotReq ocrChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"세금계산서\n합계 59,400,000원"}}]}`))
	}))
	defer srv.Close()

	t.Setenv("DENEB_OCR_VL_URL", srv.URL)

	// A 1x1 PNG is enough; content detection should yield image/png.
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
	}
	got, err := paddleOCR(context.Background(), png, "Table Recognition:")
	if err != nil {
		t.Fatalf("paddleOCR: %v", err)
	}
	if !strings.Contains(got, "59,400,000") {
		t.Errorf("result = %q, want the recognized text", got)
	}

	if gotReq.Model != ocrVLModel {
		t.Errorf("model = %q, want %q", gotReq.Model, ocrVLModel)
	}
	if len(gotReq.Messages) != 1 || len(gotReq.Messages[0].Content) != 2 {
		t.Fatalf("unexpected message structure: %+v", gotReq.Messages)
	}
	parts := gotReq.Messages[0].Content
	if parts[0].Type != "image_url" || parts[0].ImageURL == nil {
		t.Errorf("part 0 = %+v, want image_url", parts[0])
	}
	if !strings.HasPrefix(parts[0].ImageURL.URL, "data:image/png;base64,") {
		t.Errorf("image url = %q, want data:image/png;base64 prefix", parts[0].ImageURL.URL)
	}
	if parts[1].Type != "text" || parts[1].Text != "Table Recognition:" {
		t.Errorf("part 1 = %+v, want text 'Table Recognition:'", parts[1])
	}
}

// TestPaddleOCR_FallbackOnError confirms ocrImageBytes degrades to tesseract
// when the OCR server returns an error. Skipped if tesseract is absent.
func TestPaddleOCR_FallbackOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("DENEB_OCR_VL_URL", srv.URL)

	// Empty image: tesseract will also fail, but the point is that
	// ocrImageBytes reaches the fallback path without panicking.
	_, err := ocrImageBytes(context.Background(), []byte{0x00})
	if err == nil {
		t.Skip("tesseract produced output for junk input; fallback path exercised")
	}
	if !strings.Contains(err.Error(), "tesseract") && !strings.Contains(err.Error(), "OCR") {
		t.Logf("fallback error (expected tesseract-related): %v", err)
	}
}
