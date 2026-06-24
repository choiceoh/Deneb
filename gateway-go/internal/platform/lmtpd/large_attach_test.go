package lmtpd

import "testing"

// Mirrors the real tsgw.topsolar.kr 대용량첨부 widget: a largeUpDownLoader div with
// one <a> per file (download endpoint mail002A31), an <img> thumbnail
// (mail002A30), plus ordinary body links that must NOT be treated as downloads.
func TestExtractLargeAttachmentLinks(t *testing.T) {
	body := `<html><body>
<div class="largeUpDownLoader">
  <div class="filebox"><a href="https://tsgw.topsolar.kr/mail/mail002A31?&amp;key=AAA" class="icon icon_pdf">견적서_탑솔라.pdf</a></div>
  <div class="filebox"><a href="https://tsgw.topsolar.kr/mail/mail002A31?&amp;key=BBB" class="icon icon_zip">자료일체.zip</a></div>
  <img src="https://tsgw.topsolar.kr/mail/mail002A30?key=THUMB&amp;index=1&amp;type=png">
</div>
<p>본문 <a href="https://www.topsolar.kr">홈페이지</a> <a href="mailto:x@topsolar.kr">메일</a></p>
</body></html>`

	refs := extractLargeAttachmentLinks(body)
	// Absolute http(s) anchors only: 2 download links + the homepage link. The
	// <img> thumbnail and the mailto: are excluded. The homepage is harmless
	// noise here — the download host allowlist (gmailpoll) is the real filter.
	if len(refs) != 3 {
		t.Fatalf("want 3 refs, got %d: %+v", len(refs), refs)
	}
	if refs[0].URL != "https://tsgw.topsolar.kr/mail/mail002A31?&key=AAA" {
		t.Errorf("href HTML entity not decoded: %q", refs[0].URL)
	}
	if refs[0].Filename != "견적서_탑솔라.pdf" {
		t.Errorf("filename hint = %q, want 견적서_탑솔라.pdf", refs[0].Filename)
	}
}

// Real groupware wraps the filename and size on separate lines inside the
// anchor; the hint must keep only the filename (no newline, no "(22.2 MB)").
func TestExtractLargeAttachmentLinks_FilenameHintFirstLine(t *testing.T) {
	body := `<div class="largeUpDownLoader">
	<a href="https://tsgw.topsolar.kr/mail/mail002A31?key=AAA" class="icon icon_zip">
	  자료일체 (2).zip
	  (22.2 MB)
	</a></div>`
	refs := extractLargeAttachmentLinks(body)
	if len(refs) != 1 {
		t.Fatalf("want 1 ref, got %d", len(refs))
	}
	if refs[0].Filename != "자료일체 (2).zip" {
		t.Errorf("filename hint = %q, want %q", refs[0].Filename, "자료일체 (2).zip")
	}
}

func TestExtractLargeAttachmentLinks_NoWidget(t *testing.T) {
	body := `<html><body><a href="https://x.com/file.pdf">file</a></body></html>`
	if refs := extractLargeAttachmentLinks(body); refs != nil {
		t.Fatalf("want nil without the widget marker, got %+v", refs)
	}
}

func TestExtractLargeAttachmentLinks_DedupAndFilter(t *testing.T) {
	body := `<div class="largeUpDownLoader">
	<a href="https://tsgw.topsolar.kr/mail/mail002A31?key=AAA">f1</a>
	<a href="https://tsgw.topsolar.kr/mail/mail002A31?key=AAA">dup</a>
	<a href="#frag">frag</a>
	<a href="/relative/path">rel</a>
	</div>`
	refs := extractLargeAttachmentLinks(body)
	if len(refs) != 1 {
		t.Fatalf("want 1 (dup + #frag + relative dropped), got %d: %+v", len(refs), refs)
	}
}

// A large-attachment mail still carries a text/plain alternative (filenames +
// sizes), so the full parser keeps preferring plain for the body while
// surfacing the links separately. Guards the parse.go wiring.
func TestParseMessage_LargeAttachmentLinks(t *testing.T) {
	raw := crlf(
		"From: a@b.com",
		"Subject: large",
		"MIME-Version: 1.0",
		`Content-Type: multipart/alternative; boundary="B"`,
		"",
		"--B",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		"large file attach 1",
		"--B",
		"Content-Type: text/html; charset=UTF-8",
		"",
		`<div class="largeUpDownLoader"><a href="https://tsgw.topsolar.kr/mail/mail002A31?key=AAA">q.pdf</a></div>`,
		"--B--",
		"",
	)
	msg, err := parseMessage([]byte(raw), "id")
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	if len(msg.Detail.LargeAttachments) != 1 {
		t.Fatalf("want 1 large attachment link, got %d", len(msg.Detail.LargeAttachments))
	}
	if got := msg.Detail.Body; got != "large file attach 1" {
		t.Errorf("body should still be the plain part, got %q", got)
	}
}
