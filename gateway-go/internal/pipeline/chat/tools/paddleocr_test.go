package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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

// TestPaddleOCR_FallbackOnError confirms ocrImageBytes actually reaches the
// tesseract fallback when PaddleOCR-VL errors — not that it merely avoids a
// panic. ocrImageBytes returns tesseract's (text, err) verbatim on PaddleOCR-VL
// failure (it discards the HTTP error), so the fallback being reached is
// provable from the returned error alone.
func TestPaddleOCR_FallbackOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("DENEB_OCR_VL_URL", srv.URL)

	// Server is up but 500s, so paddleOCR fails and ocrImageBytes must fall
	// through to tesseract on this 1-byte junk image.
	text, err := ocrImageBytes(context.Background(), []byte{0x00})

	// The fallback must surface an error: with the OCR server down, the only
	// path left is tesseract, which rejects the junk image (or, if not
	// installed, returns its install-hint error). A nil error would mean the
	// 500 was swallowed and the fallback bypassed — the regression to catch.
	if err == nil {
		t.Fatalf("OCR server down + junk image: want a fallback error, got success text=%q", text)
	}
	// The PaddleOCR-VL error must NOT leak through: ocrImageBytes drops it in
	// favor of tesseract's result, so its presence proves the fallback was
	// skipped instead of reached.
	if strings.Contains(strings.ToLower(err.Error()), "paddleocr") {
		t.Fatalf("fallback not reached — PaddleOCR-VL error leaked instead of tesseract: %v", err)
	}
	// When tesseract is installed it must have actually run (and rejected the
	// junk), so the error is the CLI's — never the "not installed" hint.
	if _, look := exec.LookPath("tesseract"); look == nil && strings.Contains(err.Error(), "미설치") {
		t.Fatalf("tesseract present but fallback returned the not-installed hint: %v", err)
	}
}

// TestHTMLTablesToMarkdown verifies that HTML tables embedded in OCR output are
// normalized to markdown while the surrounding text is preserved. No GPU needed.
func TestHTMLTablesToMarkdown(t *testing.T) {
	in := "보고서 요약\n" +
		"<table><tr><td>품목</td><td>수량</td></tr><tr><td>모듈</td><td>100</td></tr></table>\n" +
		"이상."
	got := htmlTablesToMarkdown(in)
	for _, want := range []string{"보고서 요약", "이상.", "품목", "수량", "모듈", "100", "---"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "|") {
		t.Errorf("no markdown table pipes in:\n%s", got)
	}

	// No table → returned unchanged.
	const plain = "plain text, no tables here"
	if htmlTablesToMarkdown(plain) != plain {
		t.Errorf("plain text was mutated")
	}
}
