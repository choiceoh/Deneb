package document

import (
	"context"
	"errors"
	"testing"
)

func TestExtractFileTextReturnsRawOrEmpty(t *testing.T) {
	ctx := context.Background()
	if got := ExtractFileText(ctx, "note.txt", []byte("hello world")); got != "hello world" {
		t.Errorf("txt = %q", got)
	}
	if got := ExtractFileText(ctx, "readme.md", []byte("# title")); got != "# title" {
		t.Errorf("md = %q", got)
	}
	if got := ExtractFileText(ctx, "data.bin", []byte{0x00, 0x01}); got != "" {
		t.Errorf("unsupported format should be empty, got %q", got)
	}
}

func TestPDFPrimaryTextSkipsStructuredOCRWhenDisabled(t *testing.T) {
	want := errors.New("text-only sentinel")
	textCalls := 0
	structuredCalls := 0
	_, err := pdfPrimaryText(
		context.Background(),
		[]byte("pdf"),
		false,
		func(context.Context, []byte) (string, error) {
			textCalls++
			return "", want
		},
		func(context.Context, []byte) (string, error) {
			structuredCalls++
			return "unexpected", nil
		},
	)
	if !errors.Is(err, want) || textCalls != 1 || structuredCalls != 0 {
		t.Fatalf("err=%v textCalls=%d structuredCalls=%d", err, textCalls, structuredCalls)
	}
}

func TestExtractFileTextWithoutOCRReportsDeferredPayloads(t *testing.T) {
	ctx := context.Background()
	if got, complete := ExtractFileTextWithoutOCR(ctx, "note.txt", []byte("search me")); got != "search me" || !complete {
		t.Fatalf("text got=%q complete=%v", got, complete)
	}
	if got, complete := ExtractFileTextWithoutOCR(ctx, "scan.png", []byte("not-real-image")); got != "" || complete {
		t.Fatalf("image got=%q complete=%v, want deferred OCR", got, complete)
	}
	if got, complete := ExtractFileTextWithoutOCR(ctx, "scan.pdf", []byte("not-real-pdf")); got != "" || complete {
		t.Fatalf("scanned pdf got=%q complete=%v, want deferred OCR", got, complete)
	}
	if got, complete := ExtractFileTextWithoutOCR(ctx, "legacy.hwp", []byte("converter payload")); got != "" || complete {
		t.Fatalf("legacy got=%q complete=%v, want deferred converter", got, complete)
	}
	if got, complete := ExtractFileTextWithoutOCR(ctx, "broken.docx", []byte("not-a-zip")); got != "" || complete {
		t.Fatalf("malformed docx got=%q complete=%v, want incomplete coverage", got, complete)
	}
}
