package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// TestExtractDocument_ConsistentAcrossCallers is the regression guard for the
// dispatcher unification: the Gmail attachment path, the exported
// ExtractDocumentText facade, and the Dropbox path now all funnel through the
// single extractDocument switch, so for a real document they must extract the
// *same* bytes — only the surrounding presentation differs.
func TestExtractDocument_ConsistentAcrossCallers(t *testing.T) {
	ctx := context.Background()
	xlsx := makeTestXLSX(t)

	// Canonical dispatcher.
	r := extractDocument(ctx, xlsx, "report.xlsx", "")
	if r.kind != docXLSX || r.err != nil || !strings.Contains(r.text, "품목") {
		t.Fatalf("extractDocument xlsx: kind=%d err=%v text=%q", r.kind, r.err, r.text)
	}

	// Web-fetch / attachment-classifier facade returns the identical text.
	got, ok := ExtractDocumentText(ctx, xlsx, "report.xlsx", "")
	if !ok || got != r.text {
		t.Errorf("ExtractDocumentText diverged: ok=%v\n got=%q\nwant=%q", ok, got, r.text)
	}

	// Dropbox path returns the identical text (no header).
	if dbx := extractDropboxFileText(ctx, "report.xlsx", xlsx); dbx != r.text {
		t.Errorf("dropbox diverged:\n got=%q\nwant=%q", dbx, r.text)
	}

	// Gmail path embeds the identical text under its own Korean header.
	att := &gmail.AttachmentInfo{Filename: "report.xlsx", Size: len(xlsx)}
	gm := extractAttachmentText(ctx, att, xlsx)
	if !strings.HasPrefix(gm, "## 📎 report.xlsx (Excel)\n\n") {
		t.Errorf("gmail xlsx header missing:\n%s", gm)
	}
	if !strings.Contains(gm, r.text) {
		t.Errorf("gmail xlsx body diverged from canonical text:\n%s", gm)
	}
}

// TestExtractDocument_CallerDivergences locks in the *intended* differences
// between the callers so a future "simplification" can't quietly erase them:
//   - plain text is a readable document for Gmail/Dropbox but ExtractDocumentText
//     declines it (web fetch handles text/HTML on its own path),
//   - an unsupported binary yields nothing on every path.
func TestExtractDocument_CallerDivergences(t *testing.T) {
	ctx := context.Background()

	// Plain text.
	txt := []byte("hello world")
	if r := extractDocument(ctx, txt, "note.txt", ""); r.kind != docText || r.text != "hello world" {
		t.Fatalf("text classify: kind=%d text=%q", r.kind, r.text)
	}
	if _, ok := ExtractDocumentText(ctx, txt, "note.txt", "text/plain"); ok {
		t.Error("ExtractDocumentText must decline plain text")
	}
	if got := extractDropboxFileText(ctx, "note.txt", txt); got != "hello world" {
		t.Errorf("dropbox text = %q, want raw passthrough", got)
	}
	att := &gmail.AttachmentInfo{Filename: "note.txt", Size: len(txt)}
	if gm := extractAttachmentText(ctx, att, txt); gm != "## 📎 note.txt\n\nhello world" {
		t.Errorf("gmail text = %q", gm)
	}

	// Unsupported binary.
	bin := []byte{0x00, 0x01, 0x02}
	if r := extractDocument(ctx, bin, "blob.bin", ""); r.kind != docUnsupported {
		t.Fatalf("unsupported classify: kind=%d", r.kind)
	}
	if _, ok := ExtractDocumentText(ctx, bin, "blob.bin", "application/octet-stream"); ok {
		t.Error("ExtractDocumentText must decline unknown binary")
	}
	if got := extractDropboxFileText(ctx, "blob.bin", bin); got != "" {
		t.Errorf("dropbox unsupported = %q, want empty", got)
	}
}

// TestExtractAttachmentText_ErrorFormatting pins the Gmail-facing error string for
// a corrupt document: a parser failure must surface as the metadata + Korean
// "읽기 실패" line, not a header with empty body. Deterministic — non-zip bytes
// always fail xlsxToText.
func TestExtractAttachmentText_ErrorFormatting(t *testing.T) {
	ctx := context.Background()
	bad := []byte("this is not a zip archive")
	att := &gmail.AttachmentInfo{Filename: "broken.xlsx", MimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Size: len(bad)}

	got := extractAttachmentText(ctx, att, bad)
	if !strings.HasPrefix(got, "📎 broken.xlsx (Excel, ") {
		t.Errorf("missing Excel error preamble:\n%s", got)
	}
	if !strings.Contains(got, "⚠️ 엑셀 읽기 실패:") {
		t.Errorf("missing Korean failure line:\n%s", got)
	}
	// The exported facade must decline the same corrupt bytes.
	if _, ok := ExtractDocumentText(ctx, bad, "broken.xlsx", ""); ok {
		t.Error("ExtractDocumentText should decline a corrupt xlsx")
	}
}
