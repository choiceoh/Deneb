package gmailpoll

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

func TestAttachmentCandidates_Filters(t *testing.T) {
	atts := []gmail.AttachmentInfo{
		{Filename: "견적서.pdf", MimeType: "application/pdf", AttachmentID: "a1", Size: 50000},
		{Filename: "logo.png", MimeType: "image/png", AttachmentID: "a2", Size: 400},                     // too small → skip
		{Filename: "signature.png", MimeType: "image/png", AttachmentID: "a3", Size: 3000},               // image, kept
		{Filename: "note.txt", MimeType: "text/plain", AttachmentID: "a4", Size: 9000},                   // not extractable type → skip
		{Filename: "명세서.xlsx", MimeType: "application/vnd.openxmlformats", AttachmentID: "", Size: 8000}, // no id → skip
		{Filename: "계약서.docx", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", AttachmentID: "a6", Size: 20000},
		{Filename: "잘린견적서.pdf", MimeType: "application/pdf", AttachmentID: "a7", Size: 20000, Truncated: true},
	}
	got := attachmentCandidates(atts)
	var names []string
	for _, c := range got {
		names = append(names, c.Filename)
	}
	want := []string{"견적서.pdf", "signature.png", "계약서.docx"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("candidates = %v, want %v", names, want)
	}
}

func TestAttachmentCandidates_SizeCeiling(t *testing.T) {
	atts := []gmail.AttachmentInfo{
		{Filename: "small.pdf", MimeType: "application/pdf", AttachmentID: "a1", Size: 50000},
		{Filename: "huge.pdf", MimeType: "application/pdf", AttachmentID: "a2", Size: maxAttachmentSize + 1}, // over ceiling → skip
	}
	got := attachmentCandidates(atts)
	if len(got) != 1 || got[0].Filename != "small.pdf" {
		t.Fatalf("size ceiling: got %v, want [small.pdf]", got)
	}
}

func TestAttachmentCandidates_CountCap(t *testing.T) {
	var atts []gmail.AttachmentInfo
	for i := 0; i < maxAttachmentCandidates+3; i++ {
		atts = append(atts, gmail.AttachmentInfo{Filename: "doc.pdf", MimeType: "application/pdf", AttachmentID: "x", Size: 5000})
	}
	if got := len(attachmentCandidates(atts)); got != maxAttachmentCandidates {
		t.Fatalf("count cap: got %d, want %d", got, maxAttachmentCandidates)
	}
}

func TestIsExtractableAttachment_MimeFallback(t *testing.T) {
	// No useful extension, but the MIME says PDF → extractable.
	att := gmail.AttachmentInfo{Filename: "attachment", MimeType: "application/pdf"}
	if !isExtractableAttachment(att) {
		t.Fatal("expected MIME pdf to be extractable")
	}
	if isExtractableAttachment(gmail.AttachmentInfo{Filename: "x.zip", MimeType: "application/zip"}) {
		t.Fatal("zip should not be extractable for analysis injection")
	}
}

func TestBuildAttachmentSelection_OnlyPicked(t *testing.T) {
	ext := []extractedAttachment{
		{att: gmail.AttachmentInfo{Filename: "견적서.pdf"}, text: "총액 5,000,000원"},
		{att: gmail.AttachmentInfo{Filename: "logo.png"}, text: "회사로고"},
		{att: gmail.AttachmentInfo{Filename: "계약서.pdf"}, text: "계약 조건 다수"},
	}
	picks := map[int]bool{0: true, 2: true} // 0 and 2 selected, 1 (logo) not
	sel := buildAttachmentSelection(ext, picks)

	if !strings.Contains(sel.Injected, "견적서.pdf") || !strings.Contains(sel.Injected, "5,000,000원") {
		t.Fatalf("expected 견적서 content injected, got: %q", sel.Injected)
	}
	if !strings.Contains(sel.Injected, "계약서.pdf") {
		t.Fatalf("expected 계약서 content injected, got: %q", sel.Injected)
	}
	if strings.Contains(sel.Injected, "logo.png") {
		t.Fatal("unpicked logo must not be injected")
	}
	if len(sel.Truncated) != 0 {
		t.Fatalf("short docs must not be truncated, got %v", sel.Truncated)
	}
}

func TestBuildAttachmentSelection_EmptyWhenNoPicks(t *testing.T) {
	ext := []extractedAttachment{{att: gmail.AttachmentInfo{Filename: "x.pdf"}, text: "x"}}
	sel := buildAttachmentSelection(ext, map[int]bool{})
	if sel.Injected != "" || len(sel.Truncated) != 0 {
		t.Fatalf("expected empty selection, got %+v", sel)
	}
}

func TestBuildAttachmentSelection_TruncatesOversized(t *testing.T) {
	big := strings.Repeat("가", attachmentInjectChars+500) // exceeds the per-doc cap
	ext := []extractedAttachment{
		{att: gmail.AttachmentInfo{Filename: "긴계약서.pdf"}, text: big},
		{att: gmail.AttachmentInfo{Filename: "짧은견적.pdf"}, text: "총액 100원"},
	}
	sel := buildAttachmentSelection(ext, map[int]bool{0: true, 1: true})
	if len(sel.Truncated) != 1 || sel.Truncated[0] != "긴계약서.pdf" {
		t.Fatalf("truncated = %v, want [긴계약서.pdf]", sel.Truncated)
	}
	if !strings.Contains(sel.Injected, "짧은견적.pdf") || !strings.Contains(sel.Injected, "100원") {
		t.Fatal("short doc after a big one must still be injected in full")
	}
}

func TestIsClearBusinessDoc(t *testing.T) {
	yes := []string{"견적서_효성중공업_260602.pdf", "계약서.docx", "거래명세서.xlsx", "발주서.xls", "Quotation-final.PDF"}
	for _, f := range yes {
		if !isClearBusinessDoc(f) {
			t.Errorf("%q should be a clear business doc", f)
		}
	}
	no := []string{"계약.png", "logo.pdf", "newsletter.pdf", "signature.jpg", "계약조건.txt"}
	for _, f := range no {
		if isClearBusinessDoc(f) {
			t.Errorf("%q should NOT be force-included (image, no signal, or non-doc ext)", f)
		}
	}
}

func TestClipChars(t *testing.T) {
	if got := clipChars("짧은글", 10); got != "짧은글" {
		t.Fatalf("short unchanged: %q", got)
	}
	got := clipChars("한국어한국어한국어", 3)
	if !strings.HasPrefix(got, "한국어") || !strings.Contains(got, "생략") {
		t.Fatalf("clip by runes: %q", got)
	}
}
