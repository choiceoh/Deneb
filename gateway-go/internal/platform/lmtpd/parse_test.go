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
	if msg.Detail.ID != "id1" {
		t.Errorf("ID = %q, want id1", msg.Detail.ID)
	}
	if !strings.Contains(msg.Detail.From, "kim@topsolar.kr") {
		t.Errorf("From = %q", msg.Detail.From)
	}
	if msg.Detail.Subject != "회의 자료" {
		t.Errorf("Subject = %q, want 회의 자료", msg.Detail.Subject)
	}
	if !strings.Contains(msg.Detail.Body, "검토 부탁드립니다") {
		t.Errorf("Body = %q", msg.Detail.Body)
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
	if msg.Detail.Subject != "회의 자료" {
		t.Errorf("decoded subject = %q, want 회의 자료", msg.Detail.Subject)
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
	if !strings.Contains(msg.Detail.Body, "PLAIN BODY") {
		t.Errorf("Body = %q, want PLAIN BODY", msg.Detail.Body)
	}
	if strings.Contains(msg.Detail.Body, "<p>") {
		t.Errorf("Body should prefer plain, got HTML markup: %q", msg.Detail.Body)
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
	if !strings.Contains(msg.Detail.Body, "안녕하세요") || !strings.Contains(msg.Detail.Body, "본문") {
		t.Errorf("HTML not flattened: %q", msg.Detail.Body)
	}
	if strings.Contains(msg.Detail.Body, "<p>") {
		t.Errorf("HTML tags leaked: %q", msg.Detail.Body)
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
	if !strings.Contains(msg.Detail.Body, "견적서 첨부합니다") {
		t.Errorf("Body = %q", msg.Detail.Body)
	}
	if len(msg.Detail.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(msg.Detail.Attachments))
	}
	att := msg.Detail.Attachments[0]
	if att.Filename != "quote.pdf" || att.MimeType != "application/pdf" {
		t.Errorf("attachment = %+v", att)
	}
	if att.Size == 0 {
		t.Errorf("attachment size should be > 0 (decoded base64)")
	}
	if len(msg.AttachmentBytes[att.AttachmentID]) == 0 {
		t.Errorf("attachment bytes not retained for archiving (%s)", att.AttachmentID)
	}
}

func TestReadCappedDetectsTruncation(t *testing.T) {
	got, truncated := readCapped(strings.NewReader("abcdef"), 4)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if string(got) != "abcd" {
		t.Fatalf("got %q want abcd", got)
	}

	got, truncated = readCapped(strings.NewReader("abcd"), 4)
	if truncated {
		t.Fatal("did not expect exact-size input to be marked truncated")
	}
	if string(got) != "abcd" {
		t.Fatalf("got %q want abcd", got)
	}
}

func TestParseMessage_MessageIDDedupKey(t *testing.T) {
	raw := crlf(
		"From: a@b.com",
		"Subject: s",
		"Message-ID: <abc123/x y@mail.example.com>",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"body",
		"",
	)
	msg, err := parseMessage([]byte(raw), "fallback-unique")
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	// Sanitized Message-ID becomes both the stable cache/wiki key and the dedup
	// key. Unsafe chars are percent-encoded (not collapsed to "_") so distinct
	// ids stay distinct: '/' → %2F, ' ' → %20.
	want := "abc123%2Fx%20y@mail.example.com"
	if msg.DedupKey != want {
		t.Errorf("DedupKey = %q, want %q", msg.DedupKey, want)
	}
	if msg.Detail.ID != want {
		t.Errorf("Detail.ID = %q, want %q (stable key)", msg.Detail.ID, want)
	}

	// No Message-ID → falls back to the unique per-delivery id.
	msg2, _ := parseMessage([]byte(crlf("From: a@b.com", "Subject: s", "", "body", "")), "fallback-unique")
	if msg2.DedupKey != "fallback-unique" {
		t.Errorf("no Message-ID DedupKey = %q, want fallback", msg2.DedupKey)
	}
}

func TestSanitizeID_NoCollision(t *testing.T) {
	// Distinct Message-IDs that differ only in path/space chars must NOT collapse
	// to the same dedup key (the old "_"-collapse let one crafted id suppress
	// another). Percent-encoding keeps them distinct.
	a := sanitizeID("<a/b>")
	b := sanitizeID("<a b>")
	c := sanitizeID("<a\\b>")
	if a == b || a == c || b == c {
		t.Fatalf("collision: a/b=%q a b=%q a\\b=%q must all differ", a, b, c)
	}
	if a != "a%2Fb" || b != "a%20b" || c != "a%5Cb" {
		t.Errorf("encodings = %q/%q/%q, want a%%2Fb / a%%20b / a%%5Cb", a, b, c)
	}
	// All-unsafe ids stay non-empty and distinct (previously both → "___").
	if sanitizeID("<///>") == sanitizeID("<   >") {
		t.Error("/// and spaces must not collide")
	}
	// Control chars are dropped; leading/trailing dots stripped (filename safety).
	if got := sanitizeID("<.\x01a.>"); got != "a" {
		t.Errorf("control/dot strip = %q, want %q", got, "a")
	}
}

func TestParseMessage_MalformedMultipartPreservesBody(t *testing.T) {
	// A multipart Content-Type with an EMPTY boundary is unparseable; without the
	// fallback the body neither splits nor reads as text and vanishes. It must be
	// preserved (degraded to plain text), not silently dropped.
	raw := crlf(
		"From: a@b.com",
		"Subject: 견적",
		"Content-Type: multipart/mixed; boundary=",
		"",
		"본문 내용은 보존되어야 한다",
		"",
	)
	msg, err := parseMessage([]byte(raw), "id")
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	if !strings.Contains(msg.Detail.Body, "보존되어야 한다") {
		t.Errorf("malformed multipart body lost: %q", msg.Detail.Body)
	}
}

func TestSeenStore(t *testing.T) {
	path := t.TempDir() + "/seen.json"
	s := NewSeenStore(path, 2)
	if s.Seen("k1") {
		t.Error("k1 should not be seen yet")
	}
	s.Mark("k1")
	if !s.Seen("k1") {
		t.Error("k1 should be seen after Mark")
	}
	// Persistence: a fresh store loads the saved key.
	if !NewSeenStore(path, 2).Seen("k1") {
		t.Error("k1 should persist across reload")
	}
	// Bounded eviction (max=2): adding k2,k3 evicts k1.
	s.Mark("k2")
	s.Mark("k3")
	if s.Seen("k1") {
		t.Error("k1 should have been evicted past max")
	}
	if !s.Seen("k3") {
		t.Error("k3 should be seen")
	}
	// MarkIfNew: atomic check-and-set — first true, second false.
	if !s.MarkIfNew("k4") {
		t.Error("first MarkIfNew(k4) should be true (new)")
	}
	if s.MarkIfNew("k4") {
		t.Error("second MarkIfNew(k4) should be false (already seen)")
	}
	// Empty key is unkeyed: never recorded/matched, MarkIfNew always new.
	s.Mark("")
	if s.Seen("") {
		t.Error("empty key must not match")
	}
	if !s.MarkIfNew("") {
		t.Error("empty key MarkIfNew should be true (unkeyed)")
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
	if !strings.Contains(msg.Detail.Body, "café") {
		t.Errorf("quoted-printable not decoded: %q", msg.Detail.Body)
	}
	if !strings.Contains(msg.Detail.Body, "continues here") {
		t.Errorf("soft line break not joined: %q", msg.Detail.Body)
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
