package document

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// renderAttachments is the serial half of the concurrent attachment
// extractor: it must preserve attachment order and the per-doc/total rune caps
// regardless of extraction timing.

func TestExtractAttachments_PlainTextOrder(t *testing.T) {
	attachments := []Attachment{
		{Filename: "first.txt", MimeType: "text/plain", Bytes: []byte("alpha")},
		{Filename: "second.txt", MimeType: "text/plain", Bytes: []byte("beta")},
	}
	out := ExtractAttachments(context.Background(), attachments)
	first, second := strings.Index(out, "alpha"), strings.Index(out, "beta")
	if first < 0 || second < 0 || first >= second {
		t.Fatalf("attachment extraction lost input order: %q", out)
	}
}

func TestRenderArchiveAttachments_OrderAndEmpty(t *testing.T) {
	atts := []Attachment{
		{Filename: "a.pdf", MimeType: "application/pdf"},
		{Filename: "b.jpg", MimeType: "image/jpeg"},
		{Filename: "c.png", MimeType: "image/png"},
	}
	out := renderAttachments(atts, []string{"alpha", "", "gamma"})

	ia, ib, ic := strings.Index(out, "a.pdf"), strings.Index(out, "b.jpg"), strings.Index(out, "c.png")
	if !(ia >= 0 && ia < ib && ib < ic) {
		t.Fatalf("attachment order/headers wrong: %q", out)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "gamma") {
		t.Fatalf("extracted text missing: %q", out)
	}
	if !strings.Contains(out, "추출하지 못했습니다") {
		t.Fatalf("empty attachment should note failure: %q", out)
	}
}

func TestRenderArchiveAttachments_PerDocCap(t *testing.T) {
	long := strings.Repeat("가", attachmentPerDocRunes+500)
	out := renderAttachments(
		[]Attachment{{Filename: "big.pdf", MimeType: "application/pdf"}},
		[]string{long},
	)
	if n := utf8.RuneCountInString(out); n > attachmentPerDocRunes+200 {
		t.Fatalf("per-doc cap not applied: %d runes", n)
	}
	if !strings.Contains(out, "생략") {
		t.Fatalf("clipped doc should mark the cut")
	}
}

func TestRenderArchiveAttachments_TotalCap(t *testing.T) {
	big := strings.Repeat("x", attachmentPerDocRunes) // 20000 runes each
	atts := make([]Attachment, 4)
	texts := make([]string, 4)
	for i := range atts {
		atts[i] = Attachment{Filename: fmt.Sprintf("d%d.txt", i), MimeType: "text/plain"}
		texts[i] = big
	}
	texts[3] = "fourth doc unique content" // must be omitted: docs 0-2 fill the 48000 budget
	out := renderAttachments(atts, texts)

	if !strings.Contains(out, "전체 분량 한도로 생략") {
		t.Fatalf("total cap branch not hit")
	}
	if strings.Contains(out, "fourth doc unique content") {
		t.Fatalf("doc after total cap should be omitted")
	}
}
