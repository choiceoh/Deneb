package document

import (
	"context"
	"testing"
)

// The conversion success paths shell out to hwp5txt / LibreOffice, which live on
// the deploy host (like the OCR/ASR sidecars), not in CI — so these lock the
// tool-independent dispatch: unknown formats stay unsupported, and each convertible
// format targets the OOXML that best preserves it.

func TestConvertUnsupportedUnknownExtensionStaysUnsupported(t *testing.T) {
	r := convertUnsupported(context.Background(), []byte("whatever"), "archive.zip")
	if r.kind != docUnsupported {
		t.Fatalf("unknown extension = %v, want docUnsupported", r.kind)
	}
}

func TestExtractDocumentUnknownBinaryStaysUnsupported(t *testing.T) {
	// A format with no native parser and no converter must degrade to unsupported,
	// not error or misclassify — the pre-conversion behavior is preserved.
	r := extractDocument(context.Background(), []byte{0x00, 0x01, 0x02}, "mystery.bin", "application/octet-stream")
	if r.kind != docUnsupported {
		t.Fatalf("unknown binary = %v, want docUnsupported", r.kind)
	}
}

func TestSofficeTargetMapping(t *testing.T) {
	for _, tc := range []struct{ ext, want string }{
		{".doc", "docx"},
		{".rtf", "docx"},
		{".odt", "docx"},
		{".hwp", "docx"},
		{".hwpx", "docx"},
		{".xls", "xlsx"},
		{".ods", "xlsx"},
		{".ppt", "pptx"},
		{".odp", "pptx"},
	} {
		if got := sofficeTarget(tc.ext); got != tc.want {
			t.Errorf("sofficeTarget(%q) = %q, want %q", tc.ext, got, tc.want)
		}
	}
}
