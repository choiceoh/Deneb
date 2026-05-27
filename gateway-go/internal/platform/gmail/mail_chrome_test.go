package gmail

import (
	"strings"
	"testing"
)

// body is shared filler that clears the 200-byte shortBodyFloor so chrome
// stripping is allowed to fire. Each repeat ends with a sentence terminator
// so footer/preamble line anchors land on distinct lines.
var chromeBody = strings.Repeat("이번 주 출시 노트의 주요 변경 사항입니다. ", 8)

// TestStripMailChrome_NewPreamblePatterns covers the patterns added in the
// header-filter enhancement: trouble-viewing banners, click-here, online
// view, mobile click-to-view, "[광고]" markers, Korean variations.
func TestStripMailChrome_NewPreamblePatterns(t *testing.T) {
	cases := []struct {
		name     string
		preamble string
		absent   string
	}{
		{"trouble viewing en", "Having trouble viewing this email?", "trouble viewing"},
		{"cannot see en", "Can't see this email? Click here to view in browser.", "Can't see"},
		{"unable to view en", "Unable to view this message? View it online.", "Unable to view"},
		{"not rendering en", "Email not rendering correctly?", "not rendering"},
		{"click here en", "Click here to view this email in your browser.", "Click here"},
		{"tap here en", "Tap here to view in browser.", "Tap here"},
		{"online viewer ko", "온라인에서 보기", "온라인에서"},
		{"browser viewer ko", "브라우저에서 보기", "브라우저에서"},
		{"broken email ko", "이메일이 깨져 보이시나요?", "깨져 보이"},
		{"ad bracket ko", "[광고]", "[광고]"},
		{"ad paren ko", "(광고)", "(광고)"},
		{"ad bracket en", "[AD]", "[AD]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.preamble + "\n\n" + chromeBody + "\n"
			got := stripMailChrome(in)
			if strings.Contains(got, tc.absent) {
				t.Errorf("expected %q to be stripped; got:\n%s", tc.absent, got)
			}
			if !strings.Contains(got, "이번 주 출시") {
				t.Errorf("real body lost; got:\n%s", got)
			}
		})
	}
}

// TestStripMailChrome_NewFooterPatterns covers do-not-reply, mobile
// signatures, business-registration footers, privacy/terms footers,
// "you're receiving this because" footers.
func TestStripMailChrome_NewFooterPatterns(t *testing.T) {
	cases := []struct {
		name   string
		footer string
		absent string
	}{
		{"do not reply en", "Please do not reply to this email.", "do not reply"},
		{"no-reply en", "Sent by no-reply@example.com — please do not respond.", "no-reply"},
		{"automated en", "This is an automatically generated message.", "automatically generated"},
		{"automated ko", "본 메일은 자동으로 발송된 메일입니다.", "자동으로 발송"},
		{"send only ko", "발신 전용 메일입니다.", "발신 전용"},
		{"do not reply ko", "회신하지 마세요.", "회신하지 마"},
		{"business reg ko", "사업자등록번호: 123-45-67890", "사업자등록번호"},
		{"telesales ko", "통신판매업 신고번호: 2026-서울강남-1234", "통신판매업"},
		{"privacy en", "Privacy Policy | Contact us", "Privacy Policy"},
		{"terms en", "Terms of Service apply.", "Terms of Service"},
		{"privacy ko", "개인정보 처리방침", "개인정보 처리방침"},
		{"terms ko", "이용약관 보기", "이용약관"},
		{"receiving because en", "You are receiving this email because you subscribed.", "receiving this email because"},
		{"no longer wish en", "You no longer wish to receive these? Click here.", "no longer wish"},
		{"why you got ko", "이 이메일을 받으신 이유는 회원 가입 시 동의하셨기 때문입니다.", "받으신 이유"},
		{"mobile signature iphone", "Sent from my iPhone", "Sent from my iPhone"},
		{"mobile signature android", "Sent from my Android phone", "Sent from my Android"},
		{"mobile signature galaxy", "Sent from my Galaxy", "Sent from my Galaxy"},
		{"outlook for ios", "Get Outlook for iOS", "Get Outlook"},
		{"rfc 3676 sig with space", "-- \n홍길동\n팀장 / 마케팅", "홍길동"},
		{"rfc 3676 sig no space", "--\n홍길동\n팀장 / 마케팅", "홍길동"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := chromeBody + "\n\n" + tc.footer + "\n"
			got := stripMailChrome(in)
			if strings.Contains(got, tc.absent) {
				t.Errorf("expected %q to be stripped; got:\n%s", tc.absent, got)
			}
			if !strings.Contains(got, "이번 주 출시") {
				t.Errorf("real body lost; got:\n%s", got)
			}
		})
	}
}

// TestStripMailReplyQuote_CutsBelowMarker verifies that reply / forward
// markers truncate the body to the user's actual reply text.
func TestStripMailReplyQuote_CutsBelowMarker(t *testing.T) {
	reply := strings.Repeat("답장입니다. 검토 후 회신드리겠습니다. ", 4)

	cases := []struct {
		name   string
		marker string
		quoted string
	}{
		{
			"gmail english wrote",
			"On Mon, Jan 1, 2026 at 1:23 PM, Alice <alice@example.com> wrote:",
			"> 안녕하세요\n> 이전 메일 내용입니다.",
		},
		{
			"gmail korean wrote",
			"2026년 5월 27일 (화) 오후 1:23, Alice <alice@example.com>님이 작성:",
			"> 안녕하세요\n> 이전 메일 내용입니다.",
		},
		{
			"original message divider en",
			"----- Original Message -----",
			"From: alice@example.com\nSent: ...\nSubject: ...\n\nOriginal body text",
		},
		{
			"forwarded message divider en",
			"---------- Forwarded message ----------",
			"From: alice@example.com\n\nOriginal body text",
		},
		{
			"original message divider ko",
			"----- 원본 메시지 -----",
			"보낸 사람: alice@example.com\n제목: ...\n\n원본 내용입니다.",
		},
		{
			"forwarded message divider ko",
			"----- 전달된 메시지 -----",
			"보낸 사람: alice@example.com\n\n원본 내용입니다.",
		},
		{
			"outlook header ko",
			"보낸 사람: Alice <alice@example.com>",
			"받는 사람: Bob <bob@example.com>\n제목: 회신\n\n원본 내용",
		},
		{
			"bracket original message ko",
			"[원문 메시지]",
			"보낸 사람: alice\n\n내용",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := reply + "\n\n" + tc.marker + "\n" + tc.quoted
			got := stripMailChrome(in)
			if !strings.Contains(got, "답장입니다") {
				t.Errorf("reply lost; got:\n%s", got)
			}
			if strings.Contains(got, tc.marker) {
				t.Errorf("marker %q should be stripped; got:\n%s", tc.marker, got)
			}
			if strings.Contains(got, "원본 내용") || strings.Contains(got, "이전 메일") || strings.Contains(got, "Original body") {
				t.Errorf("quoted content leaked through; got:\n%s", got)
			}
		})
	}
}

// TestStripMailReplyQuote_KeepsForwardWithoutCommentary ensures we don't
// cut when the user forwards without writing anything on top — there's no
// reply to preserve, and the "quoted" content IS what they want shown.
func TestStripMailReplyQuote_KeepsForwardWithoutCommentary(t *testing.T) {
	in := "----- 전달된 메시지 -----\n보낸 사람: alice@example.com\n\n" +
		strings.Repeat("전달받은 본문 내용입니다. 중요한 정보가 들어 있습니다. ", 6)
	got := stripMailChrome(in)
	if !strings.Contains(got, "전달받은 본문 내용") {
		t.Errorf("forwarded body lost when there was no commentary above; got:\n%s", got)
	}
}

// TestStripMailReplyQuote_ShortPrefixSkipped — when the surviving prefix
// would be tiny (< minReplyVisible visible chars), keep the input as-is
// so the operator can see the quoted body.
func TestStripMailReplyQuote_ShortPrefixSkipped(t *testing.T) {
	in := "ㅇㅋ\n\n" +
		"----- Original Message -----\n" +
		strings.Repeat("긴 원본 본문이 여기에 길게 들어 있다고 가정합니다. ", 8)
	got := stripMailChrome(in)
	if !strings.Contains(got, "긴 원본 본문") {
		t.Errorf("when prefix is too short to be a 'reply', the quoted body must survive; got:\n%s", got)
	}
}

// TestStripMailReplyQuote_OutlookHeaderOnlyOnHeaderLikeLine — "보낸 사람:"
// in mid-body prose (no colon-prefixed header form) must not trigger a
// cut.
func TestStripMailReplyQuote_OutlookHeaderOnlyOnHeaderLikeLine(t *testing.T) {
	// The phrase "보낸 사람을" appears mid-sentence here — this is not a
	// header line and must not cut the body.
	body := strings.Repeat("이번 분기 보낸 사람을 추적하는 시스템 개선안에 대해 검토했습니다. ", 5)
	got := stripMailChrome(body)
	if got != body && !strings.Contains(got, "추적하는 시스템") {
		t.Errorf("mid-body 'sender' mention was wrongly treated as Outlook header; got:\n%s", got)
	}
}

// TestVisibleRuneCount sanity-checks the helper used to gate reply-quote
// cuts.
func TestVisibleRuneCount(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"   \n\t\r", 0},
		{"abc", 3},
		{"  abc  ", 3},
		{"한글", 2},
		{"a b\nc", 3},
	}
	for _, tc := range cases {
		if got := visibleRuneCount(tc.in); got != tc.want {
			t.Errorf("visibleRuneCount(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestStripMailChrome_BackwardCompat — verify the original safety
// invariants (short body untouched, top-half copyright kept, over-
// aggressive cut aborted) still hold after the expansion. These overlap
// with the operations_test.go tests but are kept here so the chrome file
// is self-testable.
func TestStripMailChrome_BackwardCompat(t *testing.T) {
	t.Run("short body untouched", func(t *testing.T) {
		in := "OTP: 123456 — Sent from my iPhone"
		if got := stripMailChrome(in); got != in {
			t.Errorf("short body modified: got %q", got)
		}
	})

	t.Run("over-aggressive cut aborts", func(t *testing.T) {
		// 510 bytes of preamble noise followed by one tiny visible line.
		// Phase 1 would cut to nothing, so the safety gate must fall
		// back to the original.
		chrome := strings.Repeat("View in browser. ", 30)
		in := chrome + "\nshort"
		got := stripMailChrome(in)
		if got != in {
			t.Errorf("expected abort (return input), got %q", got)
		}
	})

	t.Run("preamble in deep header (within window)", func(t *testing.T) {
		// Some newsletters wrap a logo + tagline before "view online";
		// the head window was bumped to 800 bytes specifically to cover
		// this layout. Build a preamble that lands past byte 500.
		filler := strings.Repeat("Acme Corporation Newsletter Header Line. ", 14) // ~580 bytes
		in := filler + "\nView this email online\n\n" + chromeBody
		got := stripMailChrome(in)
		if strings.Contains(got, "View this email online") {
			t.Errorf("preamble inside 800-byte window was not stripped; got:\n%s", got)
		}
	})
}
