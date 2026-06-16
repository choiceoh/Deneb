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
	picks := map[int]bool{0: false, 2: true} // 0 relevant, 2 relevant + deep review
	sel := buildAttachmentSelection(ext, picks)

	if !strings.Contains(sel.Injected, "견적서.pdf") || !strings.Contains(sel.Injected, "5,000,000원") {
		t.Fatalf("expected 견적서 content injected, got: %q", sel.Injected)
	}
	if strings.Contains(sel.Injected, "logo.png") {
		t.Fatal("unpicked logo must not be injected")
	}
	if len(sel.DeepReview) != 1 || sel.DeepReview[0] != "계약서.pdf" {
		t.Fatalf("deep review = %v, want [계약서.pdf]", sel.DeepReview)
	}
}

func TestBuildAttachmentSelection_EmptyWhenNoPicks(t *testing.T) {
	ext := []extractedAttachment{{att: gmail.AttachmentInfo{Filename: "x.pdf"}, text: "x"}}
	sel := buildAttachmentSelection(ext, map[int]bool{})
	if sel.Injected != "" || len(sel.DeepReview) != 0 {
		t.Fatalf("expected empty selection, got %+v", sel)
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
