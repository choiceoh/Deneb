package chat

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

// TestPrepareDocumentAttachments verifies that a raw document attachment (the
// shape the native client sends: base64 Data + document MimeType, no Type) is
// extracted into a document_text attachment, while images pass through.
func TestPrepareDocumentAttachments(t *testing.T) {
	csv := base64.StdEncoding.EncodeToString([]byte("이름,수량\n모듈,100\n"))
	in := []ChatAttachment{
		{MimeType: "text/csv", Name: "list.csv", Data: csv},
		{Type: "image", MimeType: "image/png", Data: "abc"}, // must pass through untouched
	}

	out := prepareDocumentAttachments(context.Background(), in)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	if out[0].Type != "document_text" {
		t.Errorf("csv not converted to document_text: %+v", out[0])
	}
	if !strings.Contains(out[0].Data, "| 이름 | 수량 |") {
		t.Errorf("csv not rendered as markdown table: %q", out[0].Data)
	}
	if out[1].Type != "image" || out[1].Data != "abc" {
		t.Errorf("image attachment was mutated: %+v", out[1])
	}
}

// TestPrepareDocumentAttachments_PassThrough leaves non-document and
// already-extracted attachments unchanged.
func TestPrepareDocumentAttachments_PassThrough(t *testing.T) {
	in := []ChatAttachment{
		{Type: "document_text", Name: "n", Data: "already text"},
		{MimeType: "application/zip", Name: "a.zip", Data: base64.StdEncoding.EncodeToString([]byte("PK..."))},
		{MimeType: "text/csv", Name: "bad.csv", Data: "!!!not base64!!!"},
	}
	out := prepareDocumentAttachments(context.Background(), in)
	if len(out) != 3 {
		t.Fatalf("len = %d, want 3", len(out))
	}
	if out[0].Data != "already text" {
		t.Errorf("document_text mutated: %+v", out[0])
	}
	if out[1].MimeType != "application/zip" {
		t.Errorf("unsupported type mutated: %+v", out[1])
	}
	if !strings.Contains(out[2].MimeType, "csv") {
		t.Errorf("non-base64 csv mutated: %+v", out[2])
	}
}
