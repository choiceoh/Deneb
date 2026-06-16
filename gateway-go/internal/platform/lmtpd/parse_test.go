package lmtpd

import (
	"strings"
	"testing"
)

// crlf joins lines with CRLF (mail is CRLF-terminated on the wire).
func crlf(lines ...string) string { return strings.Join(lines, "\r\n") }

func TestParseMessage_PlainText(t *testing.T) {
	raw := crlf(
		"From: 김철수 <kim@topsolar.kr>",
		"To: me@deneb.local",
		"Subject: 회의 자료",
		"Date: Mon, 16 Jun 2026 09:00:00 +0900",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"안녕하세요, 첨부 자료 검토 부탁드립니다.",
		"",
	)
	msg, err := parseMessage([]byte(raw), "id1")
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	if msg.ID != "id1" {
		t.Errorf("ID = %q, want id1", msg.ID)
	}
	if !strings.Contains(msg.From, "kim@topsolar.kr") {
		t.Errorf("From = %q", msg.From)
	}
	if msg.Subject != "회의 자료" {
		t.Errorf("Subject = %q, want 회의 자료", msg.Subject)
	}
	if !strings.Contains(msg.Body, "검토 부탁드립니다") {
		t.Errorf("Body = %q", msg.Body)
	}
}

func TestParseMessage_RFC2047Subject(t *testing.T) {
	// "회의 자료" as a UTF-8 base64 encoded-word.
	raw := crlf(
		"From: a@b.com",
		"Subject: =?UTF-8?B?7ZqM7J2YIOyekOujjA==?=",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"body",
		"",
	)
	msg, err := parseMessage([]byte(raw), "id")
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	if msg.Subject != "회의 자료" {
		t.Errorf("decoded subject = %q, want 회의 자료", msg.Subject)
	}
}

func TestParseMessage_MultipartPrefersPlain(t *testing.T) {
	raw := crlf(
		"From: a@b.com",
		"Subject: s",
		"Content-Type: multipart/alternative; boundary=BNDRY",
		"",
		"--BNDRY",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"PLAIN BODY",
		"--BNDRY",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<p>HTML BODY</p>",
		"--BNDRY--",
		"",
	)
	msg, err := parseMessage([]byte(raw), "id")
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	if !strings.Contains(msg.Body, "PLAIN BODY") {
		t.Errorf("Body = %q, want PLAIN BODY", msg.Body)
	}
	if strings.Contains(msg.Body, "<p>") {
		t.Errorf("Body should prefer plain, got HTML markup: %q", msg.Body)
	}
}

func TestParseMessage_HTMLOnlyFlattened(t *testing.T) {
	raw := crlf(
		"From: a@b.com",
		"Subject: s",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<p>안녕하세요</p><br><div>본문</div>",
		"",
	)
	msg, err := parseMessage([]byte(raw), "id")
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	if !strings.Contains(msg.Body, "안녕하세요") || !strings.Contains(msg.Body, "본문") {
		t.Errorf("HTML not flattened: %q", msg.Body)
	}
	if strings.Contains(msg.Body, "<p>") {
		t.Errorf("HTML tags leaked: %q", msg.Body)
	}
}

func TestParseMessage_AttachmentMetadata(t *testing.T) {
	raw := crlf(
		"From: a@b.com",
		"Subject: 견적서 송부",
		"Content-Type: multipart/mixed; boundary=MIX",
		"",
		"--MIX",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"견적서 첨부합니다.",
		"--MIX",
		"Content-Type: application/pdf; name=\"quote.pdf\"",
		"Content-Disposition: attachment; filename=\"quote.pdf\"",
		"Content-Transfer-Encoding: base64",
		"",
		"JVBERi0xLjQK",
		"--MIX--",
		"",
	)
	msg, err := parseMessage([]byte(raw), "id")
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	if !strings.Contains(msg.Body, "견적서 첨부합니다") {
		t.Errorf("Body = %q", msg.Body)
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(msg.Attachments))
	}
	att := msg.Attachments[0]
	if att.Filename != "quote.pdf" || att.MimeType != "application/pdf" {
		t.Errorf("attachment = %+v", att)
	}
	if att.Size == 0 {
		t.Errorf("attachment size should be > 0 (decoded base64)")
	}
}

func TestParseMessage_QuotedPrintable(t *testing.T) {
	// "café" with quoted-printable é (=C3=A9 in UTF-8).
	raw := crlf(
		"From: a@b.com",
		"Subject: s",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: quoted-printable",
		"",
		"caf=C3=A9 long line that wraps=",
		"continues here",
		"",
	)
	msg, err := parseMessage([]byte(raw), "id")
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	if !strings.Contains(msg.Body, "café") {
		t.Errorf("quoted-printable not decoded: %q", msg.Body)
	}
	if !strings.Contains(msg.Body, "continues here") {
		t.Errorf("soft line break not joined: %q", msg.Body)
	}
}

func TestCharsetDecode_EUCKR(t *testing.T) {
	// "한" in EUC-KR is 0xC7 0xD1.
	got := charsetDecode([]byte{0xC7, 0xD1}, "euc-kr")
	if got != "한" {
		t.Errorf("EUC-KR decode = %q, want 한", got)
	}
	// UTF-8 passes through unchanged.
	if charsetDecode([]byte("한"), "utf-8") != "한" {
		t.Errorf("utf-8 passthrough failed")
	}
}

func TestClampRunes(t *testing.T) {
	if got := clampRunes("한국어", 10); got != "한국어" {
		t.Errorf("clampRunes short = %q", got)
	}
	long := strings.Repeat("가", 50)
	got := clampRunes(long, 10)
	if !strings.HasPrefix(got, strings.Repeat("가", 10)) {
		t.Errorf("clampRunes did not cap to 10 runes: %q", got)
	}
	if !strings.Contains(got, "일부 생략") {
		t.Errorf("clampRunes missing truncation marker: %q", got)
	}
}
